package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

var ErrUserNotFound = errors.New("user not found")

type UserRecord struct {
	ID           string
	Username     string
	PasswordHash string
	PlatformRole PlatformRole
	Active       bool
}

type UserRepository interface {
	FindByUsername(ctx context.Context, username string) (UserRecord, error)
}

type queryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type PGUserRepository struct {
	database queryRower
}

func NewPGUserRepository(database queryRower) *PGUserRepository {
	return &PGUserRepository{database: database}
}

func (r *PGUserRepository) FindByUsername(ctx context.Context, username string) (UserRecord, error) {
	const query = `
		SELECT id::text, username, password_hash, platform_role, is_active
		FROM users
		WHERE lower(username) = lower($1)
		LIMIT 1`

	var record UserRecord
	var role string
	if err := r.database.QueryRow(ctx, query, username).Scan(
		&record.ID,
		&record.Username,
		&record.PasswordHash,
		&role,
		&record.Active,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return UserRecord{}, ErrUserNotFound
		}
		return UserRecord{}, fmt.Errorf("find user for authentication: %w", err)
	}

	record.PlatformRole = PlatformRole(role)
	return record, nil
}
