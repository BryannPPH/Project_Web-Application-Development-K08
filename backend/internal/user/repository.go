package user

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
)

// Repository menyimpan user platform.
type Repository interface {
	List(ctx context.Context) ([]User, error)
	Get(ctx context.Context, id string) (User, error)
	Create(ctx context.Context, record createRecord) (User, error)
	Update(ctx context.Context, id string, record updateRecord, beforeCommit BeforeCommit) (User, error)
}

// userAdminLockKey menyerialkan perubahan user agar guard "admin terakhir"
// tidak dapat dilewati oleh dua request bersamaan.
const userAdminLockKey int64 = 0x44424d5055534552 // "DBMPUSER"

type PGRepository struct {
	database *pgxpool.Pool
}

func NewPGRepository(database *pgxpool.Pool) *PGRepository {
	return &PGRepository{database: database}
}

const selectColumns = `id::text, username, platform_role, is_active, created_at, updated_at`

func scanUser(row pgx.Row) (User, error) {
	var record User
	var role string
	err := row.Scan(&record.ID, &record.Username, &role, &record.IsActive, &record.CreatedAt, &record.UpdatedAt)
	record.PlatformRole = auth.PlatformRole(role)
	return record, err
}

func (r *PGRepository) List(ctx context.Context) ([]User, error) {
	rows, err := r.database.Query(ctx, `SELECT `+selectColumns+` FROM users ORDER BY lower(username)`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	users := make([]User, 0)
	for rows.Next() {
		record, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate users: %w", err)
	}
	return users, nil
}

func (r *PGRepository) Get(ctx context.Context, id string) (User, error) {
	record, err := scanUser(r.database.QueryRow(ctx, `SELECT `+selectColumns+` FROM users WHERE id = $1::uuid`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("get user: %w", err)
	}
	return record, nil
}

func (r *PGRepository) Create(ctx context.Context, record createRecord) (User, error) {
	created, err := scanUser(r.database.QueryRow(ctx, `
		INSERT INTO users (username, password_hash, platform_role, is_active)
		VALUES ($1, $2, $3, $4)
		RETURNING `+selectColumns,
		record.Username, record.PasswordHash, string(record.PlatformRole), record.IsActive))
	if err != nil {
		return User{}, mapWriteError("create user", err)
	}
	return created, nil
}

func (r *PGRepository) Update(ctx context.Context, id string, record updateRecord, beforeCommit BeforeCommit) (User, error) {
	transaction, err := r.database.Begin(ctx)
	if err != nil {
		return User{}, fmt.Errorf("begin user update: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	if _, err := transaction.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, userAdminLockKey); err != nil {
		return User{}, fmt.Errorf("lock user update: %w", err)
	}

	before, err := scanUser(transaction.QueryRow(ctx,
		`SELECT `+selectColumns+` FROM users WHERE id = $1::uuid FOR UPDATE`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("load user for update: %w", err)
	}

	var role *string
	if record.PlatformRole != nil {
		value := string(*record.PlatformRole)
		role = &value
	}
	after, err := scanUser(transaction.QueryRow(ctx, `
		UPDATE users
		SET username = COALESCE($2, username),
			platform_role = COALESCE($3, platform_role),
			is_active = COALESCE($4, is_active),
			password_hash = COALESCE($5, password_hash),
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1::uuid
		RETURNING `+selectColumns,
		id, record.Username, role, record.IsActive, record.PasswordHash))
	if err != nil {
		return User{}, mapWriteError("update user", err)
	}

	if isActiveAdmin(before) && !isActiveAdmin(after) {
		var remaining int
		if err := transaction.QueryRow(ctx, `
			SELECT count(*) FROM users
			WHERE platform_role = 'platform_admin' AND is_active = TRUE`).Scan(&remaining); err != nil {
			return User{}, fmt.Errorf("count active platform admins: %w", err)
		}
		if remaining == 0 {
			return User{}, ErrLastActiveAdmin
		}
	}

	if beforeCommit != nil {
		if err := beforeCommit(before, after); err != nil {
			return User{}, err
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return User{}, fmt.Errorf("commit user update: %w", err)
	}
	return after, nil
}

func isActiveAdmin(record User) bool {
	return record.IsActive && record.PlatformRole == auth.PlatformRoleAdmin
}

func mapWriteError(operation string, err error) error {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) && pgError.Code == "23505" {
		return ErrDuplicateUsername
	}
	return fmt.Errorf("%s: %w", operation, err)
}
