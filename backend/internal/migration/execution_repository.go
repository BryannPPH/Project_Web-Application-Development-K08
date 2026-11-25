package migration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

func (r *PGRepository) PlanExecution(ctx context.Context, databaseID, migrationID string, action ExecutionAction, _ string) (ExecutionPlan, error) {
	entry, err := r.Get(ctx, databaseID, migrationID)
	if err != nil {
		return ExecutionPlan{}, err
	}
	plan := ExecutionPlan{Entry: entry, Action: action}

	latest, latestErr := r.LatestAttempt(ctx, migrationID)
	if latestErr != nil && !errors.Is(latestErr, ErrNoAttempt) {
		return ExecutionPlan{}, latestErr
	}
	var latestOK bool
	if latestErr == nil {
		latestOK = true
	}

	switch action {
	case ActionApply:
		if entry.Status != StatusDraft || latestOK {
			return ExecutionPlan{}, ErrConflict
		}
		if err := r.ensureEarlierMigrationsAllow(ctx, databaseID, entry.Version); err != nil {
			return ExecutionPlan{}, err
		}
		plan.SQLSnapshot = entry.SQL
	case ActionRetry:
		if !latestOK || (latest.Status != string(StatusFailed) && latest.Status != "partial") ||
			(latest.Action != ActionApply && latest.Action != ActionRetry) || entry.IsActive {
			return ExecutionPlan{}, ErrConflict
		}
		if err := r.ensureEarlierMigrationsAllow(ctx, databaseID, entry.Version); err != nil {
			return ExecutionPlan{}, err
		}
		plan.SQLSnapshot = entry.SQL
		plan.RelatedExecutionID = stringPtr(latest.ID)
		plan.CheckpointRootID = latest.CheckpointRootID
	case ActionRollback:
		if !entry.IsActive || (entry.Status != StatusSucceeded && entry.Status != StatusPartiallyReverted) {
			return ExecutionPlan{}, ErrConflict
		}
		var laterActive bool
		if err := r.pool.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1 FROM migration_entries WHERE database_id=$1::uuid AND is_active AND version>$2
		)`, databaseID, entry.Version).Scan(&laterActive); err != nil {
			return ExecutionPlan{}, mapRepositoryError(err)
		}
		if laterActive {
			return ExecutionPlan{}, ErrWrongOrder
		}
		if entry.DownSQL == nil || entry.IrreversibleReason != nil {
			return ExecutionPlan{}, ErrIrreversible
		}
		applied, err := r.latestSuccessfulApply(ctx, migrationID)
		if err != nil {
			return ExecutionPlan{}, err
		}
		plan.SQLSnapshot = *entry.DownSQL
		plan.RelatedExecutionID = stringPtr(applied.ID)
		plan.CheckpointRootID = applied.CheckpointRootID
	case ActionForward:
		if entry.IsActive || entry.Status != StatusRolledBack {
			return ExecutionPlan{}, ErrConflict
		}
		if err := r.ensureEarlierMigrationsAllow(ctx, databaseID, entry.Version); err != nil {
			return ExecutionPlan{}, err
		}
		if !latestOK || latest.Action != ActionRollback || latest.Status != string(StatusSucceeded) {
			return ExecutionPlan{}, ErrConflict
		}
		var laterActive bool
		if err := r.pool.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1 FROM migration_entries WHERE database_id=$1::uuid AND is_active AND version>$2
		)`, databaseID, entry.Version).Scan(&laterActive); err != nil {
			return ExecutionPlan{}, mapRepositoryError(err)
		}
		if laterActive {
			return ExecutionPlan{}, ErrWrongOrder
		}
		plan.SQLSnapshot = entry.SQL
		plan.RelatedExecutionID = stringPtr(latest.ID)
	default:
		return ExecutionPlan{}, ErrInvalidInput
	}
	return plan, nil
}

func (r *PGRepository) ensureEarlierMigrationsAllow(ctx context.Context, databaseID string, version int64) error {
	var blocked bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM migration_entries
		WHERE database_id=$1::uuid AND version<$2
		AND status NOT IN ('succeeded','failed','reconciled')
	)`, databaseID, version).Scan(&blocked)
	if err != nil {
		return mapRepositoryError(err)
	}
	if blocked {
		return ErrWrongOrder
	}
	return nil
}

func (r *PGRepository) StartExecution(ctx context.Context, plan ExecutionPlan, actorID, actorUsername string, now time.Time) (ExecutionAttempt, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ExecutionAttempt{}, fmt.Errorf("begin migration execution attempt: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	var attempt ExecutionAttempt
	err = tx.QueryRow(ctx, `INSERT INTO migration_execution_attempts
		(id,database_id, migration_id, version, revision, action, actor_id, requested_at, sql_snapshot, related_execution_id, checkpoint_root_id, lock_wait_micros)
		VALUES ($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7::uuid,$8,$9,$10::uuid,$11::uuid,$12)
		RETURNING id::text, database_id::text, migration_id::text, version, revision, action,
		actor_id::text, requested_at, sql_snapshot, related_execution_id::text, checkpoint_root_id::text`,
		plan.ExecutionID, plan.Entry.DatabaseID, plan.Entry.ID, plan.Entry.Version, plan.Entry.Revision, plan.Action,
		actorID, now, plan.SQLSnapshot, plan.RelatedExecutionID, plan.CheckpointRootID, plan.LockWait.Microseconds()).Scan(
		&attempt.ID, &attempt.DatabaseID, &attempt.MigrationID, &attempt.Version, &attempt.Revision,
		&attempt.Action, &attempt.ActorID, &attempt.RequestedAt, &attempt.SQLSnapshot, &attempt.RelatedExecutionID,
		&attempt.CheckpointRootID)
	if err != nil {
		return ExecutionAttempt{}, mapRepositoryError(err)
	}
	attempt.ActorUsername = actorUsername

	var allowed bool
	err = tx.QueryRow(ctx, `UPDATE migration_entries SET status='running', executed_by=$3::uuid,
		started_at=$4, finished_at=NULL, error_message=NULL, last_execution_id=$6::uuid, updated_at=$4
		WHERE database_id=$1::uuid AND id=$2::uuid AND revision=$5 AND status <> 'running'
		RETURNING TRUE`, plan.Entry.DatabaseID, plan.Entry.ID, actorID, now, plan.Entry.Revision, attempt.ID).Scan(&allowed)
	if errors.Is(err, pgx.ErrNoRows) {
		return ExecutionAttempt{}, ErrRevisionChanged
	}
	if err != nil {
		return ExecutionAttempt{}, mapRepositoryError(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO migration_execution_events
		(execution_id,status,event,occurred_at) VALUES ($1::uuid,'running','started',$2)`, attempt.ID, now); err != nil {
		return ExecutionAttempt{}, mapRepositoryError(err)
	}
	if plan.JobID != "" {
		command, err := tx.Exec(ctx, `UPDATE migration_jobs SET execution_id=$1::uuid,updated_at=$4
			WHERE id=$2::uuid AND database_id=$3::uuid AND status='running' AND lease_token=$5::uuid
			  AND worker_id=$6 AND lease_expires_at > $4`,
			attempt.ID, plan.JobID, attempt.DatabaseID, now, plan.JobLeaseToken, plan.JobWorkerID)
		if err != nil {
			return ExecutionAttempt{}, mapRepositoryError(err)
		}
		if command.RowsAffected() != 1 {
			return ExecutionAttempt{}, ErrLeaseLost
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ExecutionAttempt{}, fmt.Errorf("commit migration execution attempt: %w", err)
	}
	attempt.Status = "running"
	return attempt, nil
}

func (r *PGRepository) AppendExecutionEvent(ctx context.Context, executionID, status string, event ExecutionEventType,
	message *string, rowsAffected, batchNumber, cursorStart, cursorEnd *int64, now time.Time) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO migration_execution_events
		(execution_id,status,event,occurred_at,error_message,rows_affected,batch_number,cursor_start,cursor_end)
		VALUES ($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9)`, executionID, status, event, now,
		message, rowsAffected, batchNumber, cursorStart, cursorEnd)
	return mapRepositoryError(err)
}

func (r *PGRepository) RecordBatchEvent(ctx context.Context, executionID string, batch BatchCommitEvent, now time.Time) error {
	parameters, err := json.Marshal([]int64{batch.CursorStart, batch.CursorEnd})
	if err != nil {
		return fmt.Errorf("encode batch SQL parameters: %w", err)
	}
	event := EventBatchCommitted
	if batch.Operation == "down" {
		event = EventBatchReverted
	}
	_, err = r.pool.Exec(ctx, `INSERT INTO migration_execution_events
		(execution_id,status,event,occurred_at,query_sql,query_parameters,rows_affected,batch_number,cursor_start,cursor_end)
		VALUES ($1::uuid,'running',$2,$3,$4,$5::jsonb,$6,$7,$8,$9)`,
		executionID, event, now, batch.QuerySQL, parameters, batch.RowsAffected, batch.BatchNumber, batch.CursorStart, batch.CursorEnd)
	return mapRepositoryError(err)
}

func (r *PGRepository) FinishExecution(ctx context.Context, attempt ExecutionAttempt, status Status, message *string, now time.Time) (Entry, error) {
	eventStatus, eventType := string(status), eventForStatus(status)
	eventMessage := message
	if status == StatusSucceeded {
		eventStatus, eventType = "succeeded", EventCompleted
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Entry{}, fmt.Errorf("begin migration execution finalization: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	var migrationStatus Status
	var active bool
	if err := tx.QueryRow(ctx, `SELECT is_active FROM migration_entries WHERE id=$1::uuid FOR UPDATE`, attempt.MigrationID).Scan(&active); err != nil {
		return Entry{}, mapRepositoryError(err)
	}
	switch {
	case status == StatusSucceeded && attempt.Action == ActionRollback:
		migrationStatus, active = StatusRolledBack, false
	case status == StatusSucceeded:
		migrationStatus, active = StatusSucceeded, true
	case status == StatusPartial:
		migrationStatus, active = StatusPartial, false
	case status == StatusPartiallyReverted:
		migrationStatus, active = StatusPartiallyReverted, true
	case status == StatusInterrupted || status == StatusNeedsReconciliation:
		migrationStatus = StatusNeedsReconciliation
		// A commit may have completed even when its acknowledgement was lost.
		// Preserve the last known active position and block later work for review.
	case attempt.Action == ActionRollback:
		migrationStatus, active, message = StatusSucceeded, true, nil
	case attempt.Action == ActionForward:
		migrationStatus, active, message = StatusRolledBack, false, nil
	default:
		migrationStatus, active = StatusFailed, false
	}
	if _, err := tx.Exec(ctx, `INSERT INTO migration_execution_events
		(execution_id,status,event,occurred_at,error_message) VALUES ($1::uuid,$2,$3,$4,$5)`,
		attempt.ID, eventStatus, eventType, now, eventMessage); err != nil {
		return Entry{}, mapRepositoryError(err)
	}
	_, err = scanEntry(tx.QueryRow(ctx, `UPDATE migration_entries SET status=$3,is_active=$4,
		executed_by=$7::uuid,error_message=$5,finished_at=$6,updated_at=$6
		WHERE database_id=$1::uuid AND id=$2::uuid AND last_execution_id=$8::uuid
		RETURNING `+entryColumns,
		attempt.DatabaseID, attempt.MigrationID, migrationStatus, active, message, now, attempt.ActorID, attempt.ID))
	if err != nil {
		return Entry{}, mapRepositoryError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Entry{}, fmt.Errorf("commit migration execution finalization: %w", err)
	}
	return r.Get(ctx, attempt.DatabaseID, attempt.MigrationID)
}

func (r *PGRepository) LatestAttempt(ctx context.Context, migrationID string) (ExecutionAttempt, error) {
	attempt, err := scanAttempt(r.pool.QueryRow(ctx, attemptSelect+`
		WHERE a.migration_id=$1::uuid ORDER BY a.requested_at DESC,a.id DESC LIMIT 1`, migrationID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ExecutionAttempt{}, ErrNoAttempt
	}
	return attempt, mapRepositoryError(err)
}

func (r *PGRepository) latestSuccessfulApply(ctx context.Context, migrationID string) (ExecutionAttempt, error) {
	attempt, err := scanAttempt(r.pool.QueryRow(ctx, attemptSelect+`
		WHERE a.migration_id=$1::uuid AND latest.status='succeeded'
		AND a.action IN ('apply','retry','forward')
		ORDER BY a.requested_at DESC,a.id DESC LIMIT 1`, migrationID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ExecutionAttempt{}, ErrConflict
	}
	return attempt, mapRepositoryError(err)
}

const attemptSelect = `SELECT a.id::text,a.database_id::text,a.migration_id::text,a.version,a.revision,
	a.action,a.actor_id::text,u.username,a.requested_at,a.sql_snapshot,a.related_execution_id::text,
	a.checkpoint_root_id::text,COALESCE(latest.status,'queued'),latest.error_message
	FROM migration_execution_attempts a
	JOIN users u ON u.id=a.actor_id
	LEFT JOIN LATERAL (SELECT e.status,e.error_message FROM migration_execution_events e
		WHERE e.execution_id=a.id ORDER BY e.id DESC LIMIT 1) latest ON TRUE `

func (r *PGRepository) executionRoot(ctx context.Context, executionID string) (string, error) {
	var root string
	err := r.pool.QueryRow(ctx, `WITH RECURSIVE chain AS (
		SELECT id,related_execution_id,action,0 AS depth FROM migration_execution_attempts WHERE id=$1::uuid
		UNION ALL
		SELECT parent.id,parent.related_execution_id,parent.action,chain.depth+1
		FROM migration_execution_attempts parent JOIN chain ON parent.id=chain.related_execution_id
	)
	SELECT id::text FROM chain WHERE action IN ('apply','forward') ORDER BY depth DESC LIMIT 1`, executionID).Scan(&root)
	if err != nil {
		return "", mapRepositoryError(err)
	}
	return root, nil
}

func (r *PGRepository) ListHistory(ctx context.Context, databaseID string, options HistoryOptions) (HistoryPage, error) {
	where := `a.database_id=$1::uuid`
	args := []any{databaseID}
	if options.ExecutionID != "" {
		where += ` AND a.id=$2::uuid`
		args = append(args, options.ExecutionID)
	}
	limitArg, offsetArg := len(args)+1, len(args)+2
	args = append(args, options.Limit, options.Offset)
	rows, err := r.pool.Query(ctx, attemptSelect+`WHERE `+where+
		fmt.Sprintf(` ORDER BY a.requested_at DESC,a.id DESC LIMIT $%d OFFSET $%d`, limitArg, offsetArg), args...)
	if err != nil {
		return HistoryPage{}, mapRepositoryError(err)
	}
	defer rows.Close()
	page := HistoryPage{Items: make([]ExecutionAttempt, 0), Limit: options.Limit, Offset: options.Offset}
	for rows.Next() {
		attempt, err := scanAttempt(rows)
		if err != nil {
			return HistoryPage{}, err
		}
		page.Items = append(page.Items, attempt)
	}
	if err := rows.Err(); err != nil {
		return HistoryPage{}, mapRepositoryError(err)
	}
	countArgs := args[:len(args)-2]
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM migration_execution_attempts a WHERE `+where, countArgs...).Scan(&page.Total); err != nil {
		return HistoryPage{}, mapRepositoryError(err)
	}
	for index := range page.Items {
		events, err := r.executionEvents(ctx, page.Items[index].ID)
		if err != nil {
			return HistoryPage{}, err
		}
		page.Items[index].Events = events
	}
	return page, nil
}

func (r *PGRepository) GetHistory(ctx context.Context, databaseID, executionID string) (ExecutionAttempt, error) {
	attempt, err := scanAttempt(r.pool.QueryRow(ctx, attemptSelect+`WHERE a.database_id=$1::uuid AND a.id=$2::uuid`, databaseID, executionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ExecutionAttempt{}, ErrNoAttempt
	}
	if err != nil {
		return ExecutionAttempt{}, mapRepositoryError(err)
	}
	attempt.Events, err = r.executionEvents(ctx, executionID)
	return attempt, err
}

func (r *PGRepository) executionEvents(ctx context.Context, executionID string) ([]ExecutionEvent, error) {
	rows, err := r.pool.Query(ctx, `SELECT id,status,event,occurred_at,error_message,COALESCE(query_sql,''),query_parameters,rows_affected,
		batch_number,cursor_start,cursor_end FROM migration_execution_events
		WHERE execution_id=$1::uuid ORDER BY id`, executionID)
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	defer rows.Close()
	events := make([]ExecutionEvent, 0)
	for rows.Next() {
		var event ExecutionEvent
		var rawParameters []byte
		if err := rows.Scan(&event.ID, &event.Status, &event.Event, &event.OccurredAt, &event.ErrorMessage,
			&event.QuerySQL, &rawParameters,
			&event.RowsAffected, &event.BatchNumber, &event.CursorStart, &event.CursorEnd); err != nil {
			return nil, mapRepositoryError(err)
		}
		if len(rawParameters) > 0 {
			if err := json.Unmarshal(rawParameters, &event.QueryParameters); err != nil {
				return nil, fmt.Errorf("decode batch SQL parameters: %w", err)
			}
		}
		events = append(events, event)
	}
	return events, mapRepositoryError(rows.Err())
}

func scanAttempt(row rowScanner) (ExecutionAttempt, error) {
	var attempt ExecutionAttempt
	err := row.Scan(&attempt.ID, &attempt.DatabaseID, &attempt.MigrationID, &attempt.Version, &attempt.Revision,
		&attempt.Action, &attempt.ActorID, &attempt.ActorUsername, &attempt.RequestedAt, &attempt.SQLSnapshot,
		&attempt.RelatedExecutionID, &attempt.CheckpointRootID, &attempt.Status, &attempt.ErrorMessage)
	return attempt, err
}

func eventForStatus(status Status) ExecutionEventType {
	switch status {
	case StatusSucceeded:
		return EventCompleted
	case StatusPartial:
		return EventPartial
	case StatusPartiallyReverted:
		return EventPartiallyReverted
	case StatusInterrupted:
		return EventInterrupted
	case StatusNeedsReconciliation:
		return EventNeedsReconciliation
	default:
		return EventFailed
	}
}

func stringPtr(value string) *string { return &value }
