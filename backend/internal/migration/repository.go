package migration

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PGRepository struct {
	pool *pgxpool.Pool
}

func NewPGRepository(pool *pgxpool.Pool) *PGRepository { return &PGRepository{pool: pool} }

const entryColumns = `id::text, database_id::text, version, name, sql_text, status,
created_by::text, executed_by::text, created_at, started_at, finished_at, error_message,
reconciled_from_status, reconciled_by::text, reconciled_at, reconciliation_note, revision,
sql_down, irreversible_reason, is_active, last_execution_id::text, execution_mode, batch_select_sql, batch_size, batch_cursor_start`

const readableEntryColumns = `m.id::text, m.database_id::text, m.version, m.name, m.sql_text, m.status,
m.created_by::text, m.executed_by::text, m.created_at, m.started_at, m.finished_at, m.error_message,
 m.reconciled_from_status, m.reconciled_by::text, m.reconciled_at, m.reconciliation_note,
creator.username, executor.username, reconciler.username, m.revision,
m.sql_down, m.irreversible_reason, m.is_active, m.last_execution_id::text, m.execution_mode, m.batch_select_sql, m.batch_size, m.batch_cursor_start`

func (r *PGRepository) CreateDraft(ctx context.Context, databaseID, name, sqlText, creatorID string, now time.Time) (Entry, error) {
	reason := "No down SQL was supplied by this legacy Milestone 1 client."
	return r.CreateDraftDefinition(ctx, databaseID, Definition{
		Name: name, SQL: sqlText, IrreversibleReason: &reason, ExecutionMode: ExecutionModeSingle,
	}, creatorID, now)
}

func (r *PGRepository) CreateDraftDefinition(ctx context.Context, databaseID string, definition Definition, creatorID string, now time.Time) (Entry, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Entry{}, fmt.Errorf("begin create draft: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	key1, key2 := advisoryKeys(databaseID)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, $2)`, key1, key2); err != nil {
		return Entry{}, fmt.Errorf("lock draft version: %w", err)
	}

	row := tx.QueryRow(ctx, `
		INSERT INTO migration_entries (database_id, version, name, sql_text, sql_down, irreversible_reason,
			execution_mode, batch_select_sql, batch_size, batch_cursor_start,
			status, created_by, created_at, updated_at)
		SELECT $1::uuid, COALESCE(MAX(version), 0) + 1, $2, $3, $4, $5,
			$6, $7, $8, $9, 'draft', $10::uuid, $11, $11
		FROM migration_entries WHERE database_id = $1::uuid
		RETURNING `+entryColumns, databaseID, definition.Name, definition.SQL, definition.DownSQL,
		definition.IrreversibleReason, normalizeExecutionMode(definition.ExecutionMode), definition.BatchSelectSQL,
		definition.BatchSize, definition.BatchCursorStart, creatorID, now)
	entry, err := scanEntry(row)
	if err != nil {
		return Entry{}, mapRepositoryError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Entry{}, fmt.Errorf("commit create draft: %w", err)
	}
	return entry, nil
}

func (r *PGRepository) UpdateDraftDefinition(ctx context.Context, databaseID, migrationID string, definition Definition, now time.Time) (Entry, error) {
	row := r.pool.QueryRow(ctx, `
		WITH updated AS (
			UPDATE migration_entries SET name=$3, sql_text=$4, sql_down=$5, irreversible_reason=$6,
				execution_mode=$7, batch_select_sql=$8, batch_size=$9, batch_cursor_start=$10, updated_at=$11
			WHERE database_id=$1::uuid AND id=$2::uuid AND status='draft'
			RETURNING *
		)
		SELECT `+readableEntryColumns+`
		FROM updated m
		JOIN users creator ON creator.id=m.created_by
		LEFT JOIN users executor ON executor.id=m.executed_by
		LEFT JOIN users reconciler ON reconciler.id=m.reconciled_by`,
		databaseID, migrationID, definition.Name, definition.SQL, definition.DownSQL,
		definition.IrreversibleReason, normalizeExecutionMode(definition.ExecutionMode), definition.BatchSelectSQL,
		definition.BatchSize, definition.BatchCursorStart, now)
	entry, err := scanReadableEntry(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Entry{}, r.classifyMissingOrConflict(ctx, databaseID, migrationID)
	}
	return entry, mapRepositoryError(err)
}

func (r *PGRepository) UpdateDraft(ctx context.Context, databaseID, migrationID, name, sqlText string, now time.Time) (Entry, error) {
	row := r.pool.QueryRow(ctx, `
		WITH updated AS (
			UPDATE migration_entries SET name=$3, sql_text=$4, updated_at=$5
			WHERE database_id=$1::uuid AND id=$2::uuid AND status='draft'
			RETURNING *
		)
		SELECT `+readableEntryColumns+`
		FROM updated m
		JOIN users creator ON creator.id=m.created_by
		LEFT JOIN users executor ON executor.id=m.executed_by
		LEFT JOIN users reconciler ON reconciler.id=m.reconciled_by`, databaseID, migrationID, name, sqlText, now)
	entry, err := scanReadableEntry(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Entry{}, r.classifyMissingOrConflict(ctx, databaseID, migrationID)
	}
	return entry, mapRepositoryError(err)
}

func (r *PGRepository) List(ctx context.Context, databaseID string, options ListOptions) (Page, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+readableEntryColumns+`, count(*) OVER()
		FROM migration_entries m
		JOIN users creator ON creator.id=m.created_by
		LEFT JOIN users executor ON executor.id=m.executed_by
		LEFT JOIN users reconciler ON reconciler.id=m.reconciled_by
		WHERE m.database_id=$1::uuid
		ORDER BY m.version DESC LIMIT $2 OFFSET $3`, databaseID, options.Limit, options.Offset)
	if err != nil {
		return Page{}, mapRepositoryError(err)
	}
	defer rows.Close()

	page := Page{Items: make([]Entry, 0), Limit: options.Limit, Offset: options.Offset}
	for rows.Next() {
		entry, total, err := scanReadableEntryWithTotal(rows)
		if err != nil {
			return Page{}, err
		}
		page.Items = append(page.Items, entry)
		page.Total = total
	}
	return page, mapRepositoryError(rows.Err())
}

func (r *PGRepository) Get(ctx context.Context, databaseID, migrationID string) (Entry, error) {
	entry, err := scanReadableEntry(r.pool.QueryRow(ctx, `SELECT `+readableEntryColumns+`
		FROM migration_entries m
		JOIN users creator ON creator.id=m.created_by
		LEFT JOIN users executor ON executor.id=m.executed_by
		LEFT JOIN users reconciler ON reconciler.id=m.reconciled_by
		WHERE m.database_id=$1::uuid AND m.id=$2::uuid`, databaseID, migrationID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Entry{}, ErrNotFound
	}
	return entry, mapRepositoryError(err)
}

func (r *PGRepository) MarkRunning(ctx context.Context, databaseID, migrationID, executorID string, expectedRevision int64, now time.Time) (Entry, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Entry{}, fmt.Errorf("begin running transition: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	var blocked bool
	err = tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM migration_entries current
			JOIN migration_entries earlier ON earlier.database_id=current.database_id AND earlier.version < current.version
			WHERE current.database_id=$1::uuid AND current.id=$2::uuid
			AND earlier.status NOT IN ('succeeded','failed','reconciled')
		)`, databaseID, migrationID).Scan(&blocked)
	if err != nil {
		return Entry{}, mapRepositoryError(err)
	}
	if blocked {
		return Entry{}, ErrWrongOrder
	}

	entry, err := scanEntry(tx.QueryRow(ctx, `
		UPDATE migration_entries SET status='running', executed_by=$3::uuid,
		started_at=$4, finished_at=NULL, error_message=NULL, updated_at=$4
		WHERE database_id=$1::uuid AND id=$2::uuid AND status='draft' AND revision=$5
		RETURNING `+entryColumns, databaseID, migrationID, executorID, now, expectedRevision))
	if errors.Is(err, pgx.ErrNoRows) {
		var current int64
		var status string
		if lookupErr := tx.QueryRow(ctx, `SELECT status, revision FROM migration_entries
			WHERE database_id=$1::uuid AND id=$2::uuid`, databaseID, migrationID).Scan(&status, &current); lookupErr == nil &&
			status == string(StatusDraft) && current != expectedRevision {
			return Entry{}, ErrRevisionChanged
		}
		return Entry{}, r.classifyMissingOrConflict(ctx, databaseID, migrationID)
	}
	if err != nil {
		return Entry{}, mapRepositoryError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Entry{}, fmt.Errorf("commit running transition: %w", err)
	}
	return entry, nil
}

func (r *PGRepository) MarkTerminal(ctx context.Context, databaseID, migrationID string, status Status, publicError *string, now time.Time) (Entry, error) {
	if status != StatusSucceeded && status != StatusFailed && status != StatusInterrupted && status != StatusNeedsReconciliation {
		return Entry{}, ErrInvalidInput
	}
	entry, err := scanEntry(r.pool.QueryRow(ctx, `
		UPDATE migration_entries SET status=$3, error_message=$4, finished_at=$5, updated_at=$5
		WHERE database_id=$1::uuid AND id=$2::uuid AND status='running'
		RETURNING `+entryColumns, databaseID, migrationID, status, publicError, now))
	if errors.Is(err, pgx.ErrNoRows) {
		return Entry{}, r.classifyMissingOrConflict(ctx, databaseID, migrationID)
	}
	return entry, mapRepositoryError(err)
}

func (r *PGRepository) Reconcile(ctx context.Context, databaseID, migrationID, reconciledBy, note string, now time.Time) (Entry, error) {
	entry, err := scanEntry(r.pool.QueryRow(ctx, `
		UPDATE migration_entries
		SET status='reconciled', reconciled_from_status=status, reconciled_by=$3::uuid,
			reconciled_at=$4, reconciliation_note=$5, updated_at=$4
		WHERE database_id=$1::uuid AND id=$2::uuid
		AND status IN ('interrupted','needs_reconciliation')
		RETURNING `+entryColumns, databaseID, migrationID, reconciledBy, now, note))
	if errors.Is(err, pgx.ErrNoRows) {
		return Entry{}, r.classifyMissingOrConflict(ctx, databaseID, migrationID)
	}
	return entry, mapRepositoryError(err)
}

func (r *PGRepository) RecoverRunning(ctx context.Context, before time.Time, reason string) (int64, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, mapRepositoryError(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var recovered int64
	err = tx.QueryRow(ctx, `WITH recovered AS (
		UPDATE migration_entries SET status='interrupted', error_message=$2, finished_at=$1, updated_at=$1
		WHERE status='running' AND started_at < $1
		RETURNING last_execution_id
	), logged AS (
		INSERT INTO migration_execution_events (execution_id,status,event,occurred_at,error_message)
		SELECT last_execution_id,'interrupted','interrupted',$1,$2 FROM recovered WHERE last_execution_id IS NOT NULL
		RETURNING execution_id
	)
	SELECT count(*) FROM recovered`, before, reason).Scan(&recovered)
	if err != nil {
		return 0, mapRepositoryError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit interrupted execution recovery: %w", err)
	}
	return recovered, nil
}

func (r *PGRepository) classifyMissingOrConflict(ctx context.Context, databaseID, migrationID string) error {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM migration_entries WHERE database_id=$1::uuid AND id=$2::uuid)`, databaseID, migrationID).Scan(&exists)
	if err != nil {
		return mapRepositoryError(err)
	}
	if !exists {
		return ErrNotFound
	}
	return ErrConflict
}

type rowScanner interface{ Scan(...any) error }

func scanEntry(row rowScanner) (Entry, error) {
	var entry Entry
	var executedBy *string
	err := row.Scan(&entry.ID, &entry.DatabaseID, &entry.Version, &entry.Name, &entry.SQL,
		&entry.Status, &entry.CreatedBy, &executedBy, &entry.CreatedAt, &entry.StartedAt,
		&entry.FinishedAt, &entry.ErrorMessage, &entry.ReconciledFrom, &entry.ReconciledBy,
		&entry.ReconciledAt, &entry.ReconciliationNote, &entry.Revision,
		&entry.DownSQL, &entry.IrreversibleReason, &entry.IsActive, &entry.LatestExecutionID, &entry.ExecutionMode,
		&entry.BatchSelectSQL, &entry.BatchSize, &entry.BatchCursorStart)
	entry.ExecutedBy = executedBy
	return entry, err
}

func scanReadableEntry(row rowScanner) (Entry, error) {
	var entry Entry
	var reconciledByUsername *string
	err := row.Scan(&entry.ID, &entry.DatabaseID, &entry.Version, &entry.Name, &entry.SQL,
		&entry.Status, &entry.CreatedBy, &entry.ExecutedBy, &entry.CreatedAt, &entry.StartedAt,
		&entry.FinishedAt, &entry.ErrorMessage, &entry.ReconciledFrom, &entry.ReconciledBy,
		&entry.ReconciledAt, &entry.ReconciliationNote, &entry.CreatedByUsername, &entry.ExecutedByUsername,
		&reconciledByUsername, &entry.Revision, &entry.DownSQL, &entry.IrreversibleReason,
		&entry.IsActive, &entry.LatestExecutionID, &entry.ExecutionMode, &entry.BatchSelectSQL, &entry.BatchSize, &entry.BatchCursorStart)
	entry.ReconciledByUsername = reconciledByUsername
	return entry, err
}

func scanReadableEntryWithTotal(row rowScanner) (Entry, int64, error) {
	var entry Entry
	var total int64
	var reconciledByUsername *string
	err := row.Scan(&entry.ID, &entry.DatabaseID, &entry.Version, &entry.Name, &entry.SQL,
		&entry.Status, &entry.CreatedBy, &entry.ExecutedBy, &entry.CreatedAt, &entry.StartedAt,
		&entry.FinishedAt, &entry.ErrorMessage, &entry.ReconciledFrom, &entry.ReconciledBy,
		&entry.ReconciledAt, &entry.ReconciliationNote, &entry.CreatedByUsername, &entry.ExecutedByUsername,
		&reconciledByUsername, &entry.Revision, &entry.DownSQL, &entry.IrreversibleReason,
		&entry.IsActive, &entry.LatestExecutionID, &entry.ExecutionMode, &entry.BatchSelectSQL, &entry.BatchSize, &entry.BatchCursorStart, &total)
	entry.ReconciledByUsername = reconciledByUsername
	return entry, total, err
}

func normalizeExecutionMode(mode ExecutionMode) ExecutionMode {
	if mode == "" {
		return ExecutionModeSingle
	}
	return mode
}

func mapRepositoryError(err error) error {
	if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) || errors.Is(err, ErrWrongOrder) || errors.Is(err, ErrRevisionChanged) || errors.Is(err, ErrNoAttempt) || errors.Is(err, ErrIrreversible) {
		return err
	}
	return fmt.Errorf("migration repository: %w", err)
}
