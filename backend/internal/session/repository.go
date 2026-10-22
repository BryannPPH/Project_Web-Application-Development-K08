package session

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
)

var ErrNotFound = errors.New("session not found")

type Record struct {
	ID                      string
	Principal               auth.Principal
	UserActive              bool
	CurrentTokenHash        []byte
	PreviousTokenHash       []byte
	PreviousTokenValidUntil *time.Time
	CSRFTokenHash           []byte
	LastActivityAt          time.Time
	RotatedAt               time.Time
	AbsoluteExpiresAt       time.Time
	RevokedAt               *time.Time
	TokenIsCurrent          bool
}

type CreateParams struct {
	UserID            string
	CurrentTokenHash  []byte
	CSRFTokenHash     []byte
	Now               time.Time
	AbsoluteExpiresAt time.Time
}

type RotateParams struct {
	SessionID               string
	ExpectedCurrentHash     []byte
	NewCurrentHash          []byte
	PreviousTokenValidUntil time.Time
	Now                     time.Time
}

type Repository interface {
	Create(ctx context.Context, params CreateParams) (string, error)
	FindByTokenHash(ctx context.Context, tokenHash []byte, now time.Time) (Record, error)
	Rotate(ctx context.Context, params RotateParams) (bool, error)
	Touch(ctx context.Context, sessionID string, now time.Time) (bool, error)
	RotateCSRF(ctx context.Context, sessionID string, csrfHash []byte, now time.Time) (bool, error)
	Revoke(ctx context.Context, sessionID string, now time.Time) error
	RevokeAllForUser(ctx context.Context, userID string, now time.Time) error
}

type database interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

type PGRepository struct {
	database database
}

func NewPGRepository(database database) *PGRepository {
	return &PGRepository{database: database}
}

func (r *PGRepository) Create(ctx context.Context, params CreateParams) (string, error) {
	const query = `
		INSERT INTO sessions (
			user_id,
			current_token_hash,
			csrf_token_hash,
			last_activity_at,
			rotated_at,
			absolute_expires_at,
			created_at,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $4, $5, $4, $4)
		RETURNING id::text`

	var id string
	if err := r.database.QueryRow(
		ctx,
		query,
		params.UserID,
		params.CurrentTokenHash,
		params.CSRFTokenHash,
		params.Now,
		params.AbsoluteExpiresAt,
	).Scan(&id); err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return id, nil
}

func (r *PGRepository) FindByTokenHash(ctx context.Context, tokenHash []byte, now time.Time) (Record, error) {
	const query = `
		SELECT
			s.id::text,
			u.id::text,
			u.username,
			u.platform_role,
			u.is_active,
			s.current_token_hash,
			s.previous_token_hash,
			s.previous_token_valid_until,
			s.csrf_token_hash,
			s.last_activity_at,
			s.rotated_at,
			s.absolute_expires_at,
			s.revoked_at,
			(s.current_token_hash = $1) AS token_is_current
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.current_token_hash = $1
		   OR (s.previous_token_hash = $1 AND s.previous_token_valid_until > $2)
		ORDER BY (s.current_token_hash = $1) DESC
		LIMIT 1`

	var record Record
	var role string
	if err := r.database.QueryRow(ctx, query, tokenHash, now).Scan(
		&record.ID,
		&record.Principal.UserID,
		&record.Principal.Username,
		&role,
		&record.UserActive,
		&record.CurrentTokenHash,
		&record.PreviousTokenHash,
		&record.PreviousTokenValidUntil,
		&record.CSRFTokenHash,
		&record.LastActivityAt,
		&record.RotatedAt,
		&record.AbsoluteExpiresAt,
		&record.RevokedAt,
		&record.TokenIsCurrent,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Record{}, ErrNotFound
		}
		return Record{}, fmt.Errorf("find session: %w", err)
	}
	record.Principal.PlatformRole = auth.PlatformRole(role)
	return record, nil
}

func (r *PGRepository) Rotate(ctx context.Context, params RotateParams) (bool, error) {
	const query = `
		UPDATE sessions
		SET previous_token_hash = current_token_hash,
			previous_token_valid_until = $3,
			current_token_hash = $4,
			last_activity_at = $5,
			rotated_at = $5,
			updated_at = $5
		WHERE id = $1
		  AND current_token_hash = $2
		  AND revoked_at IS NULL
		  AND absolute_expires_at > $5`
	tag, err := r.database.Exec(
		ctx,
		query,
		params.SessionID,
		params.ExpectedCurrentHash,
		params.PreviousTokenValidUntil,
		params.NewCurrentHash,
		params.Now,
	)
	if err != nil {
		return false, fmt.Errorf("rotate session token: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (r *PGRepository) Touch(ctx context.Context, sessionID string, now time.Time) (bool, error) {
	const query = `
		UPDATE sessions
		SET last_activity_at = GREATEST(last_activity_at, $2),
			updated_at = $2
		WHERE id = $1
		  AND revoked_at IS NULL
		  AND absolute_expires_at > $2`
	tag, err := r.database.Exec(ctx, query, sessionID, now)
	if err != nil {
		return false, fmt.Errorf("update session activity: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (r *PGRepository) RotateCSRF(ctx context.Context, sessionID string, csrfHash []byte, now time.Time) (bool, error) {
	const query = `
		UPDATE sessions
		SET csrf_token_hash = $2, updated_at = $3
		WHERE id = $1
		  AND revoked_at IS NULL
		  AND absolute_expires_at > $3`
	tag, err := r.database.Exec(ctx, query, sessionID, csrfHash, now)
	if err != nil {
		return false, fmt.Errorf("rotate csrf token: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (r *PGRepository) Revoke(ctx context.Context, sessionID string, now time.Time) error {
	const query = `
		UPDATE sessions
		SET revoked_at = COALESCE(revoked_at, $2), updated_at = $2
		WHERE id = $1`
	if _, err := r.database.Exec(ctx, query, sessionID, now); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

func (r *PGRepository) RevokeAllForUser(ctx context.Context, userID string, now time.Time) error {
	const query = `
		UPDATE sessions
		SET revoked_at = $2, updated_at = $2
		WHERE user_id = $1 AND revoked_at IS NULL`
	if _, err := r.database.Exec(ctx, query, userID, now); err != nil {
		return fmt.Errorf("revoke user sessions: %w", err)
	}
	return nil
}
