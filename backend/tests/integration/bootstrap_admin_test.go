package integration_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
)

func TestBootstrapAdminIsHashedAndCreatedOnlyOnce(t *testing.T) {
	databaseURL := os.Getenv("TEST_PLATFORM_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_PLATFORM_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	controlPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect control database: %v", err)
	}
	t.Cleanup(controlPool.Close)

	schemaName := fmt.Sprintf("bootstrap_test_%d", time.Now().UTC().UnixNano())
	quotedSchema := pgx.Identifier{schemaName}.Sanitize()
	if _, err := controlPool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create bootstrap test schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := controlPool.Exec(cleanupCtx, "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("drop bootstrap test schema: %v", err)
		}
	})
	if _, err := controlPool.Exec(ctx, "CREATE TABLE "+quotedSchema+`.users (
		username TEXT NOT NULL,
		password_hash TEXT NOT NULL,
		platform_role TEXT NOT NULL,
		is_active BOOLEAN NOT NULL
	)`); err != nil {
		t.Fatalf("create bootstrap users table: %v", err)
	}

	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse bootstrap pool config: %v", err)
	}
	poolConfig.ConnConfig.RuntimeParams["search_path"] = schemaName
	bootstrapPool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatalf("create bootstrap pool: %v", err)
	}
	t.Cleanup(bootstrapPool.Close)
	if err := bootstrapPool.Ping(ctx); err != nil {
		t.Fatalf("ping bootstrap pool: %v", err)
	}

	passwords := auth.NewArgon2idHasher()
	bootstrapper := auth.NewBootstrapper(bootstrapPool, passwords)
	results := make(chan error, 2)
	var waitGroup sync.WaitGroup
	for range 2 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			results <- bootstrapper.CreateInitialAdmin(ctx, "initial-admin", "bootstrap-password-123")
		}()
	}
	waitGroup.Wait()
	close(results)

	var successes int
	var duplicates int
	for result := range results {
		switch {
		case result == nil:
			successes++
		case errors.Is(result, auth.ErrPlatformAdminExists):
			duplicates++
		default:
			t.Fatalf("unexpected bootstrap result: %v", result)
		}
	}
	if successes != 1 || duplicates != 1 {
		t.Fatalf("bootstrap results: success=%d duplicate=%d, want 1 and 1", successes, duplicates)
	}

	var storedHash string
	var count int
	if err := bootstrapPool.QueryRow(ctx, `
		SELECT count(*), min(password_hash)
		FROM users
		WHERE platform_role = 'platform_admin' AND is_active = TRUE
	`).Scan(&count, &storedHash); err != nil {
		t.Fatalf("read bootstrap admin: %v", err)
	}
	if count != 1 || storedHash == "bootstrap-password-123" {
		t.Fatalf("stored admin count=%d hash=%q", count, storedHash)
	}
	matches, err := passwords.Verify("bootstrap-password-123", storedHash)
	if err != nil || !matches {
		t.Fatalf("verify stored password: matches=%v error=%v", matches, err)
	}
}
