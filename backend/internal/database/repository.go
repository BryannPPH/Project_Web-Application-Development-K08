package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository menyimpan metadata managed database di platform database.
type Repository interface {
	List(ctx context.Context) ([]ManagedDatabase, error)
	ListByIDs(ctx context.Context, ids []string) ([]ManagedDatabase, error)
	Get(ctx context.Context, id string) (ManagedDatabase, error)
	GetCredential(ctx context.Context, id string) (ManagedDatabase, StoredCredential, error)
	Create(ctx context.Context, record ManagedDatabase, credential StoredCredential) (ManagedDatabase, error)
	Update(ctx context.Context, id string, changes patch) (ManagedDatabase, error)
	Delete(ctx context.Context, id string) error
}

// PGRepository adalah implementasi Repository berbasis pgxpool.
type PGRepository struct {
	database *pgxpool.Pool
}

func NewPGRepository(database *pgxpool.Pool) *PGRepository {
	return &PGRepository{database: database}
}

// selectColumns sengaja tidak memuat credential_ciphertext.
const selectColumns = `
	id::text, engine, name, host, port, database_name, username, ssl_mode, is_active,
	octet_length(credential_ciphertext) > 0, created_at, updated_at`

func scanManagedDatabase(row pgx.Row) (ManagedDatabase, error) {
	var record ManagedDatabase
	err := row.Scan(
		&record.ID,
		&record.Engine,
		&record.Name,
		&record.Host,
		&record.Port,
		&record.DatabaseName,
		&record.Username,
		&record.SSLMode,
		&record.IsActive,
		&record.HasCredential,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	return record, err
}

func collect(rows pgx.Rows) ([]ManagedDatabase, error) {
	defer rows.Close()
	records := make([]ManagedDatabase, 0)
	for rows.Next() {
		record, err := scanManagedDatabase(rows)
		if err != nil {
			return nil, fmt.Errorf("scan managed database: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate managed databases: %w", err)
	}
	return records, nil
}

func (r *PGRepository) List(ctx context.Context) ([]ManagedDatabase, error) {
	rows, err := r.database.Query(ctx, `SELECT `+selectColumns+` FROM managed_databases ORDER BY lower(name)`)
	if err != nil {
		return nil, fmt.Errorf("list managed databases: %w", err)
	}
	return collect(rows)
}

func (r *PGRepository) ListByIDs(ctx context.Context, ids []string) ([]ManagedDatabase, error) {
	if len(ids) == 0 {
		return []ManagedDatabase{}, nil
	}
	rows, err := r.database.Query(ctx, `
		SELECT `+selectColumns+`
		FROM managed_databases
		WHERE id = ANY($1::uuid[])
		ORDER BY lower(name)`, ids)
	if err != nil {
		return nil, fmt.Errorf("list managed databases by id: %w", err)
	}
	return collect(rows)
}

func (r *PGRepository) Get(ctx context.Context, id string) (ManagedDatabase, error) {
	record, err := scanManagedDatabase(r.database.QueryRow(ctx,
		`SELECT `+selectColumns+` FROM managed_databases WHERE id = $1::uuid`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return ManagedDatabase{}, ErrNotFound
	}
	if err != nil {
		return ManagedDatabase{}, fmt.Errorf("get managed database: %w", err)
	}
	return record, nil
}

func (r *PGRepository) GetCredential(ctx context.Context, id string) (ManagedDatabase, StoredCredential, error) {
	var record ManagedDatabase
	var credential StoredCredential
	err := r.database.QueryRow(ctx, `
		SELECT id::text, engine, name, host, port, database_name, username, ssl_mode, is_active,
		       credential_ciphertext, credential_key_version
		FROM managed_databases
		WHERE id = $1::uuid`, id).Scan(
		&record.ID,
		&record.Engine,
		&record.Name,
		&record.Host,
		&record.Port,
		&record.DatabaseName,
		&record.Username,
		&record.SSLMode,
		&record.IsActive,
		&credential.Ciphertext,
		&credential.KeyVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ManagedDatabase{}, StoredCredential{}, ErrNotFound
	}
	if err != nil {
		return ManagedDatabase{}, StoredCredential{}, fmt.Errorf("get managed database credential: %w", err)
	}
	record.HasCredential = len(credential.Ciphertext) > 0
	return record, credential, nil
}

func (r *PGRepository) Create(ctx context.Context, record ManagedDatabase, credential StoredCredential) (ManagedDatabase, error) {
	created, err := scanManagedDatabase(r.database.QueryRow(ctx, `
		INSERT INTO managed_databases (
			id, engine, name, host, port, database_name, username,
			credential_ciphertext, credential_key_version, ssl_mode, is_active
		)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING `+selectColumns,
		record.ID,
		record.Engine,
		record.Name,
		record.Host,
		record.Port,
		record.DatabaseName,
		record.Username,
		credential.Ciphertext,
		credential.KeyVersion,
		record.SSLMode,
		record.IsActive,
	))
	if err != nil {
		return ManagedDatabase{}, mapWriteError("create managed database", err)
	}
	return created, nil
}

func (r *PGRepository) Update(ctx context.Context, id string, changes patch) (ManagedDatabase, error) {
	var ciphertext []byte
	var keyVersion *int16
	if changes.Credential != nil {
		ciphertext = changes.Credential.Ciphertext
		keyVersion = &changes.Credential.KeyVersion
	}
	// SQL statis: nilai NULL berarti field tidak diubah.
	updated, err := scanManagedDatabase(r.database.QueryRow(ctx, `
		UPDATE managed_databases
		SET engine = COALESCE($2, engine),
			name = COALESCE($3, name),
			host = COALESCE($4, host),
			port = COALESCE($5, port),
			database_name = COALESCE($6, database_name),
			username = COALESCE($7, username),
			ssl_mode = COALESCE($8, ssl_mode),
			is_active = COALESCE($9, is_active),
			credential_ciphertext = COALESCE($10, credential_ciphertext),
			credential_key_version = COALESCE($11, credential_key_version),
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1::uuid
		RETURNING `+selectColumns,
		id,
		changes.Engine,
		changes.Name,
		changes.Host,
		changes.Port,
		changes.DatabaseName,
		changes.Username,
		changes.SSLMode,
		changes.IsActive,
		ciphertext,
		keyVersion,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return ManagedDatabase{}, ErrNotFound
	}
	if err != nil {
		return ManagedDatabase{}, mapWriteError("update managed database", err)
	}
	return updated, nil
}

func (r *PGRepository) Delete(ctx context.Context, id string) error {
	tag, err := r.database.Exec(ctx, `DELETE FROM managed_databases WHERE id = $1::uuid`, id)
	if err != nil {
		return mapWriteError("delete managed database", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func mapWriteError(operation string, err error) error {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch pgError.Code {
		case "23505":
			return ErrDuplicateName
		// 23001 = restrict_violation (ON DELETE RESTRICT), 23503 = foreign_key_violation.
		case "23001", "23503":
			return ErrInUse
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}
