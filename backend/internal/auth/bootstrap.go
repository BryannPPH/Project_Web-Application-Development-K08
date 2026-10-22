package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInvalidUsername     = errors.New("username must contain between 3 and 64 characters")
	ErrPlatformAdminExists = errors.New("an active platform admin already exists")
)

const bootstrapAdvisoryLockKey int64 = 0x44424d5041444d49 // "DBMPADMI"

// Bootstrapper membuat satu administrator platform awal.
type Bootstrapper struct {
	database  *pgxpool.Pool
	passwords PasswordHasher
}

func NewBootstrapper(database *pgxpool.Pool, passwords PasswordHasher) *Bootstrapper {
	return &Bootstrapper{database: database, passwords: passwords}
}

func (b *Bootstrapper) CreateInitialAdmin(ctx context.Context, username, password string) error {
	username = strings.TrimSpace(username)
	if length := utf8.RuneCountInString(username); length < 3 || length > 64 {
		return ErrInvalidUsername
	}

	passwordHash, err := b.passwords.Hash(password)
	if err != nil {
		return err
	}

	transaction, err := b.database.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin bootstrap transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	// Cegah bootstrap serentak dalam lingkup transaksi.
	if _, err := transaction.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", bootstrapAdvisoryLockKey); err != nil {
		return fmt.Errorf("lock platform admin bootstrap: %w", err)
	}

	var exists bool
	if err := transaction.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM users
			WHERE platform_role = 'platform_admin' AND is_active = TRUE
		)`).Scan(&exists); err != nil {
		return fmt.Errorf("check initial platform admin: %w", err)
	}
	if exists {
		return ErrPlatformAdminExists
	}

	if _, err := transaction.Exec(ctx, `
		INSERT INTO users (username, password_hash, platform_role, is_active)
		VALUES ($1, $2, 'platform_admin', TRUE)`, username, passwordHash); err != nil {
		return fmt.Errorf("create initial platform admin: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit platform admin bootstrap: %w", err)
	}
	return nil
}
