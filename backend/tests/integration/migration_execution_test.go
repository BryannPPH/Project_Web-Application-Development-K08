package integration_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/migration"
)

type allowMigrationAccess struct{}

func (allowMigrationAccess) RequireDatabaseView(context.Context, auth.Principal, string) error {
	return nil
}
func (allowMigrationAccess) RequireDatabaseEdit(context.Context, auth.Principal, string) error {
	return nil
}

type integrationTargetProvider struct{ url string }
type integrationTargetConnection struct{ connection *pgx.Conn }

func (p integrationTargetProvider) Open(ctx context.Context, _ string) (migration.TargetConnection, error) {
	connection, err := pgx.Connect(ctx, p.url)
	if err != nil {
		return nil, err
	}
	return integrationTargetConnection{connection: connection}, nil
}
func (c integrationTargetConnection) Begin(ctx context.Context) (migration.TargetTransaction, error) {
	return c.connection.Begin(ctx)
}
func (c integrationTargetConnection) Close(ctx context.Context) error { return c.connection.Close(ctx) }

func TestMigrationExecutionCommitAndRollback(t *testing.T) {
	platformURL := os.Getenv("TEST_PLATFORM_DATABASE_URL")
	targetURL := os.Getenv("TEST_TARGET_DATABASE_URL")
	if platformURL == "" || targetURL == "" {
		t.Skip("platform and target test database URLs are required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	platform, err := pgxpool.New(ctx, platformURL)
	if err != nil {
		t.Fatalf("connect platform database: %v", err)
	}
	defer platform.Close()
	target, err := pgx.Connect(ctx, targetURL)
	if err != nil {
		t.Fatalf("connect target database: %v", err)
	}
	defer func() { _ = target.Close(context.Background()) }()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	table := "migration_atomic_" + suffix
	username := "migration_user_" + suffix
	databaseName := "migration_database_" + suffix
	var userID, databaseID string
	if err := platform.QueryRow(ctx, `INSERT INTO users (username,password_hash) VALUES ($1,'test-only') RETURNING id::text`, username).Scan(&userID); err != nil {
		t.Fatalf("create migration user: %v", err)
	}
	if err := platform.QueryRow(ctx, `INSERT INTO managed_databases
		(name,host,database_name,username,credential_ciphertext,ssl_mode)
		VALUES ($1,'localhost','target_db','target_owner',$2,'disable') RETURNING id::text`, databaseName, []byte("test-only")).Scan(&databaseID); err != nil {
		t.Fatalf("create managed database: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = target.Exec(cleanupCtx, `DROP TABLE IF EXISTS `+table)
		_, _ = platform.Exec(cleanupCtx, `DELETE FROM migration_entries WHERE database_id=$1`, databaseID)
		_, _ = platform.Exec(cleanupCtx, `DELETE FROM managed_databases WHERE id=$1`, databaseID)
		_, _ = platform.Exec(cleanupCtx, `DELETE FROM users WHERE id=$1`, userID)
	})

	repository := migration.NewPGRepository(platform)
	service, err := migration.NewService(
		repository,
		allowMigrationAccess{},
		migration.NewPGAdvisoryLocker(platform),
		migration.NewPGExecutor(integrationTargetProvider{url: targetURL}),
		migration.RealClock{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		migration.ServiceConfig{ExecutionTimeout: 10 * time.Second},
	)
	if err != nil {
		t.Fatal(err)
	}
	principal := auth.Principal{UserID: userID, Username: username}

	successSQL := fmt.Sprintf(`CREATE TABLE %s(id bigint PRIMARY KEY); INSERT INTO %s VALUES (1)`, table, table)
	success, err := service.CreateDraft(ctx, principal, databaseID, "create atomic fixture", successSQL)
	if err != nil {
		t.Fatalf("create successful draft: %v", err)
	}
	success, err = service.Execute(ctx, principal, databaseID, success.ID)
	if err != nil || success.Status != migration.StatusSucceeded {
		t.Fatalf("execute successful migration = status %s, error %v", success.Status, err)
	}

	failureSQL := fmt.Sprintf(`INSERT INTO %s VALUES (2); INSERT INTO table_that_does_not_exist VALUES (1)`, table)
	failure, err := service.CreateDraft(ctx, principal, databaseID, "rollback fixture", failureSQL)
	if err != nil {
		t.Fatalf("create failing draft: %v", err)
	}
	failure, err = service.Execute(ctx, principal, databaseID, failure.ID)
	if err != nil || failure.Status != migration.StatusFailed {
		t.Fatalf("execute failing migration = status %s, error %v", failure.Status, err)
	}

	var rows int
	if err := target.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&rows); err != nil {
		t.Fatalf("inspect target: %v", err)
	}
	if rows != 1 {
		t.Fatalf("target row count = %d, want 1 after rollback", rows)
	}

	page, err := service.List(ctx, principal, databaseID, migration.ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	if page.Total != 2 || page.Items[0].Version != 2 || page.Items[1].Version != 1 {
		t.Fatalf("unexpected linear history: %#v", page)
	}

	const concurrentDrafts = 8
	versions := make(chan int64, concurrentDrafts)
	errorsFound := make(chan error, concurrentDrafts)
	var wait sync.WaitGroup
	for index := 0; index < concurrentDrafts; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			entry, createErr := repository.CreateDraft(ctx, databaseID, fmt.Sprintf("concurrent %d", index), "SELECT 1", userID, time.Now().UTC())
			if createErr != nil {
				errorsFound <- createErr
				return
			}
			versions <- entry.Version
		}(index)
	}
	wait.Wait()
	close(versions)
	close(errorsFound)
	for createErr := range errorsFound {
		t.Fatalf("concurrent create: %v", createErr)
	}
	seen := make(map[int64]bool, concurrentDrafts)
	for version := range versions {
		if seen[version] {
			t.Fatalf("duplicate concurrent version %d", version)
		}
		seen[version] = true
	}
	if len(seen) != concurrentDrafts {
		t.Fatalf("allocated %d concurrent versions, want %d", len(seen), concurrentDrafts)
	}
}
