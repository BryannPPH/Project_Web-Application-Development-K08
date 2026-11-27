package migration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/platformdb"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	latestOK := latestErr == nil
	if entry.Status == StatusPartial {
		if !latestOK {
			return ExecutionPlan{}, ErrConflict
		}
		latest, latestErr = partialSourceAttempt(ctx, r, entry, latest)
		if latestErr != nil {
			return ExecutionPlan{}, latestErr
		}
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
		partialBatch := entry.ExecutionMode == ExecutionModeBatch && entry.Status == StatusPartial && !entry.IsActive
		resumeRollback := entry.ExecutionMode == ExecutionModeBatch && entry.Status == StatusPartiallyReverted
		if !partialBatch && !resumeRollback && !(entry.IsActive && entry.Status == StatusSucceeded) {
			return ExecutionPlan{}, ErrConflict
		}
		laterBlocking, err := r.laterBlockingVersion(ctx, databaseID, entry.Version)
		if err != nil {
			return ExecutionPlan{}, err
		}
		if laterBlocking != nil {
			return ExecutionPlan{}, ErrWrongOrder
		}
		if entry.DownSQL == nil || entry.IrreversibleReason != nil {
			return ExecutionPlan{}, ErrIrreversible
		}
		var applied ExecutionAttempt
		if partialBatch {
			applied = latest
		} else if entry.Status == StatusPartiallyReverted && latestOK &&
			latest.Action == ActionRollback && latest.RelatedExecutionID != nil {
			applied, err = r.GetHistory(ctx, databaseID, *latest.RelatedExecutionID)
			if err != nil || (applied.Status != string(StatusSucceeded) && applied.Status != string(StatusPartial)) ||
				(applied.Action != ActionApply && applied.Action != ActionRetry && applied.Action != ActionForward) {
				return ExecutionPlan{}, ErrConflict
			}
		} else {
			applied, err = r.latestSuccessfulApply(ctx, migrationID)
			if err != nil {
				return ExecutionPlan{}, err
			}
		}
		plan.SQLSnapshot = *entry.DownSQL
		plan.RelatedExecutionID = stringPtr(applied.ID)
		plan.CheckpointRootID = applied.CheckpointRootID
	case ActionForward:
		if entry.IsActive || (entry.Status != StatusRolledBack && entry.Status != StatusPartial) {
			return ExecutionPlan{}, ErrConflict
		}
		if err := r.ensureEarlierMigrationsAllow(ctx, databaseID, entry.Version); err != nil {
			return ExecutionPlan{}, err
		}
		if entry.Status == StatusPartial {
			if latest.Action != ActionForward {
				return ExecutionPlan{}, ErrConflict
			}
			plan.CheckpointRootID = latest.CheckpointRootID
		} else {
			// A failed forward leaves the migration rolled back. Reuse the
			// successful rollback as the anchor for the next forward attempt.
			if latestOK && latest.Action == ActionForward && latest.Status == string(StatusFailed) && latest.RelatedExecutionID != nil {
				latest, err = r.getAttempt(ctx, databaseID, *latest.RelatedExecutionID)
				if err != nil {
					return ExecutionPlan{}, err
				}
			}
			if !latestOK || latest.Action != ActionRollback || latest.Status != string(StatusSucceeded) {
				return ExecutionPlan{}, ErrConflict
			}
		}
		laterBlocking, err := r.laterBlockingVersion(ctx, databaseID, entry.Version)
		if err != nil {
			return ExecutionPlan{}, err
		}
		if laterBlocking != nil {
			return ExecutionPlan{}, ErrWrongOrder
		}
		plan.SQLSnapshot = entry.SQL
		plan.RelatedExecutionID = stringPtr(latest.ID)
	default:
		return ExecutionPlan{}, ErrInvalidInput
	}
	return plan, nil
}

func (r *PGRepository) laterBlockingVersion(ctx context.Context, databaseID string, version int64) (*int64, error) {
	var blocker *int64
	err := r.pool.QueryRow(ctx, `SELECT max(version) FROM migration_entries
		WHERE database_id=$1::uuid AND version>$2
		AND (is_active OR status IN ('running','partial','partially_reverted','needs_reconciliation','interrupted'))`,
		databaseID, version).Scan(&blocker)
	return blocker, mapRepositoryError(err)
}

func partialSourceAttempt(ctx context.Context, repository AttemptRepository, entry Entry, attempt ExecutionAttempt) (ExecutionAttempt, error) {
	if attempt.Status == string(StatusFailed) && attempt.RelatedExecutionID != nil {
		// A failed resume or rollback with no new commits leaves the source
		// partial execution intact.
		source, err := repository.GetHistory(ctx, entry.DatabaseID, *attempt.RelatedExecutionID)
		if err != nil {
			return ExecutionAttempt{}, err
		}
		attempt = source
	}
	if attempt.Status != string(StatusPartial) ||
		(attempt.Action != ActionApply && attempt.Action != ActionRetry && attempt.Action != ActionForward) {
		return ExecutionAttempt{}, ErrConflict
	}
	return attempt, nil
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

	if plan.revalidate != nil {
		// The short start transaction fixes the policy state until the attempt
		// is durably marked running. Concurrent execution starts share these locks.
		if _, err := tx.Exec(ctx, `LOCK TABLE groups, group_database_access, group_leaders,
			group_members, managed_databases, migration_approvals, users IN SHARE MODE`); err != nil {
			return ExecutionAttempt{}, fmt.Errorf("lock execution policy: %w", err)
		}
		if err := plan.revalidate(platformdb.WithTransactionReads(ctx, tx)); err != nil {
			return ExecutionAttempt{}, err
		}
	}

	var attempt ExecutionAttempt
	err = tx.QueryRow(ctx, `INSERT INTO migration_execution_attempts
		(id,database_id, migration_id, version, revision, action, actor_id, requested_at, sql_snapshot, related_execution_id, checkpoint_root_id, lock_wait_micros, previous_migration_status, target_journaled)
		VALUES ($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7::uuid,$8,$9,$10::uuid,$11::uuid,$12,$13,$14)
		RETURNING id::text, database_id::text, migration_id::text, version, revision, action,
		actor_id::text, requested_at, sql_snapshot, related_execution_id::text, checkpoint_root_id::text`,
		plan.ExecutionID, plan.Entry.DatabaseID, plan.Entry.ID, plan.Entry.Version, plan.Entry.Revision, plan.Action,
		actorID, now, plan.SQLSnapshot, plan.RelatedExecutionID, plan.CheckpointRootID, plan.LockWait.Microseconds(), plan.Entry.Status, plan.targetJournaled).Scan(
		&attempt.ID, &attempt.DatabaseID, &attempt.MigrationID, &attempt.Version, &attempt.Revision,
		&attempt.Action, &attempt.ActorID, &attempt.RequestedAt, &attempt.SQLSnapshot, &attempt.RelatedExecutionID,
		&attempt.CheckpointRootID)
	if err != nil {
		return ExecutionAttempt{}, mapRepositoryError(err)
	}
	attempt.ActorUsername = actorUsername
	attempt.previousMigrationStatus = plan.Entry.Status

	var allowed bool
	err = tx.QueryRow(ctx, `UPDATE migration_entries SET status='running', executed_by=$3::uuid,
		started_at=$4, finished_at=NULL, error_message=NULL, last_execution_id=$6::uuid, updated_at=$4,
		reconciled_from_status=NULL,reconciled_by=NULL,reconciled_at=NULL,reconciliation_note=NULL
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
		command, err := tx.Exec(ctx, `UPDATE migration_jobs SET execution_id=$1::uuid,retry_execution_id=NULL,updated_at=$4
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

func (r *PGRepository) RecordBatchFailure(ctx context.Context, executionID, status string, failure BatchFailureEvent, message *string, now time.Time) error {
	return insertBatchFailure(ctx, r.pool.Exec, executionID, status, failure, message, now)
}

func insertBatchFailure(ctx context.Context, exec func(context.Context, string, ...any) (pgconn.CommandTag, error),
	executionID, status string, failure BatchFailureEvent, message *string, now time.Time) error {
	parameters, err := json.Marshal(failure.QueryParameters)
	if err != nil {
		return fmt.Errorf("encode failed batch SQL parameters: %w", err)
	}
	_, err = exec(ctx, `INSERT INTO migration_execution_events
		(execution_id,status,event,occurred_at,error_message,query_sql,query_parameters,batch_number,cursor_start,cursor_end)
		VALUES ($1::uuid,$2,'batch_failed',$3,$4,$5,$6::jsonb,$7,$8,$9)`,
		executionID, status, now, message, failure.QuerySQL, parameters,
		failure.BatchNumber, failure.CursorStart, failure.CursorEnd)
	return mapRepositoryError(err)
}

func (r *PGRepository) FinishExecution(ctx context.Context, attempt ExecutionAttempt, status Status, message *string, now time.Time) (Entry, error) {
	return r.finishExecution(ctx, attempt, status, message, now, nil, nil, nil)
}

type reconciliationRecord struct {
	actorID string
	note    string
}

func (r *PGRepository) finishExecution(ctx context.Context, attempt ExecutionAttempt, status Status, message *string,
	now time.Time, reconciliation *reconciliationRecord, batchFailure *BatchFailureEvent, retryJob *Job) (Entry, error) {
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
	var previousStatus Status
	if err := tx.QueryRow(ctx, `SELECT is_active,status FROM migration_entries WHERE id=$1::uuid FOR UPDATE`, attempt.MigrationID).
		Scan(&active, &previousStatus); err != nil {
		return Entry{}, mapRepositoryError(err)
	}
	if reconciliation != nil && !needsReconciliation(previousStatus) {
		return Entry{}, ErrConflict
	}
	switch {
	case status == StatusSucceeded && attempt.Action == ActionRollback:
		migrationStatus, active = StatusRolledBack, false
	case status == StatusSucceeded:
		migrationStatus, active = StatusSucceeded, true
	case status == StatusPartial:
		migrationStatus, active = StatusPartial, false
	case status == StatusPartiallyReverted:
		migrationStatus = StatusPartiallyReverted
	case status == StatusInterrupted || status == StatusNeedsReconciliation:
		migrationStatus = StatusNeedsReconciliation
		// A commit may have completed even when its acknowledgement was lost.
		// Preserve the last known active position and block later work for review.
	case attempt.Action == ActionRollback:
		switch attempt.previousMigrationStatus {
		case StatusPartial, StatusPartiallyReverted, StatusSucceeded:
			migrationStatus = attempt.previousMigrationStatus
		default:
			// Older attempts do not have a saved previous state.
			migrationStatus = StatusSucceeded
			if !active {
				migrationStatus = StatusPartial
			}
		}
		message = nil
	case attempt.Action == ActionForward:
		migrationStatus, active, message = StatusRolledBack, false, nil
		if attempt.previousMigrationStatus == StatusPartial {
			migrationStatus = StatusPartial
		}
	case attempt.Action == ActionRetry && attempt.previousMigrationStatus == StatusPartial:
		migrationStatus, active, message = StatusPartial, false, nil
	default:
		migrationStatus, active = StatusFailed, false
	}
	// Commit failure evidence with the final status so recovery cannot see
	// a terminal event while the migration is still marked running.
	if batchFailure != nil {
		if err := insertBatchFailure(ctx, tx.Exec, attempt.ID, eventStatus, *batchFailure, eventMessage, now); err != nil {
			return Entry{}, err
		}
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
	if reconciliation != nil {
		if _, err := tx.Exec(ctx, `UPDATE migration_entries SET reconciled_from_status=$2,reconciled_by=$3::uuid,
			reconciled_at=$4,reconciliation_note=$5 WHERE id=$1::uuid`,
			attempt.MigrationID, previousStatus, reconciliation.actorID, now, reconciliation.note); err != nil {
			return Entry{}, mapRepositoryError(err)
		}
		var code *string
		if status == StatusFailed {
			code = stringPtr("TARGET_TRANSACTION_NOT_COMMITTED")
		}
		if _, err := tx.Exec(ctx, `UPDATE migration_jobs SET status=$2,error_code=$3,error_message=$4,updated_at=$5
			WHERE execution_id=$1::uuid AND status='needs_reconciliation'`,
			attempt.ID, jobStatusForExecutionStatus(string(status)), code, eventMessage, now); err != nil {
			return Entry{}, mapRepositoryError(err)
		}
	}
	if retryJob != nil {
		// Save the failed attempt and schedule the retry in one platform transaction.
		command, err := tx.Exec(ctx, `UPDATE migration_jobs
			SET status='retrying', retry_count=retry_count+1, next_attempt_at=$5, updated_at=$6,
			    error_code='TARGET_TRANSACTION_RETRY', error_message=$7, retry_execution_id=execution_id,
			    execution_id=NULL, worker_id=NULL, lease_token=NULL, lease_expires_at=NULL
			WHERE id=$1::uuid AND status='running' AND lease_token=$2::uuid AND worker_id=$3
			  AND execution_id=$4::uuid AND lease_expires_at > $6`,
			retryJob.ID, valueOrEmpty(retryJob.leaseToken), valueOrEmpty(retryJob.workerID), attempt.ID,
			now.Add(retryJob.transactionRetryDelay), now, message)
		if err != nil {
			return Entry{}, mapRepositoryError(err)
		}
		if command.RowsAffected() != 1 {
			return Entry{}, ErrLeaseLost
		}
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

const attemptSelect = `SELECT a.id::text AS id,a.database_id::text,a.migration_id::text,a.version,a.revision,
	a.action,a.actor_id::text,u.username,a.requested_at,a.sql_snapshot,a.related_execution_id::text,
	a.checkpoint_root_id::text,COALESCE(latest.status,'queued'),latest.error_message,COALESCE(a.previous_migration_status,''),m.name,
	NULL::text AS rejected_job_id,NULL::text AS rejection_code,a.target_journaled
	FROM migration_execution_attempts a
	JOIN users u ON u.id=a.actor_id
	JOIN migration_entries m ON m.id=a.migration_id
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

// Failed jobs without an execution belong in history without changing an
// execution's lifecycle or locking a draft definition.
const rejectedJobSelect = `SELECT j.id::text AS id,j.database_id::text,j.migration_id::text,m.version,j.requested_revision,
	j.action,j.actor_id::text,j.actor_username,j.created_at AS requested_at,''::text,NULL::text,
	''::text,j.status,j.error_message,''::text,m.name,j.id::text,j.error_code,FALSE
	FROM migration_jobs j JOIN migration_entries m ON m.id=j.migration_id
	WHERE j.database_id=$1::uuid AND j.status='failed' AND j.execution_id IS NULL`

func (r *PGRepository) ListHistory(ctx context.Context, databaseID string, options HistoryOptions) (HistoryPage, error) {
	where := `a.database_id=$1::uuid`
	args := []any{databaseID}
	jobs := rejectedJobSelect
	if options.ExecutionID != "" {
		where += ` AND a.id=$2::uuid`
		jobs += ` AND j.id=$2::uuid`
		args = append(args, options.ExecutionID)
	}
	if options.HideRollback {
		where += ` AND NOT (
			(latest.status='succeeded' AND a.action IN ('apply','retry','forward') AND EXISTS (
				SELECT 1 FROM migration_execution_attempts rollback
				JOIN LATERAL (SELECT e.status FROM migration_execution_events e
					WHERE e.execution_id=rollback.id ORDER BY e.id DESC LIMIT 1) rollback_latest ON TRUE
				WHERE rollback.database_id=a.database_id AND rollback.action='rollback'
					AND rollback.related_execution_id=a.id AND rollback_latest.status='succeeded'
			))
			OR (latest.status='succeeded' AND a.action='rollback' AND EXISTS (
				SELECT 1 FROM migration_execution_attempts cancelled
				JOIN LATERAL (SELECT e.status FROM migration_execution_events e
					WHERE e.execution_id=cancelled.id ORDER BY e.id DESC LIMIT 1) cancelled_latest ON TRUE
				WHERE cancelled.database_id=a.database_id AND cancelled.action IN ('apply','retry','forward')
					AND cancelled.id=a.related_execution_id AND cancelled_latest.status='succeeded'
			))
		)`
	}
	limitArg, offsetArg := len(args)+1, len(args)+2
	args = append(args, options.Limit, options.Offset)
	history := `(` + attemptSelect + `WHERE ` + where + ` UNION ALL ` + jobs + `)`
	rows, err := r.pool.Query(ctx, `SELECT * FROM `+history+
		fmt.Sprintf(` history ORDER BY requested_at DESC,id DESC LIMIT $%d OFFSET $%d`, limitArg, offsetArg), args...)
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
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM `+history+` history`, countArgs...).Scan(&page.Total); err != nil {
		return HistoryPage{}, mapRepositoryError(err)
	}
	// History lists summaries. Detail and explicit execution filters still include the event ledger.
	if options.ExecutionID == "" {
		return page, nil
	}
	for index := range page.Items {
		if page.Items[index].JobID != nil {
			continue
		}
		events, err := r.executionEvents(ctx, page.Items[index].ID)
		if err != nil {
			return HistoryPage{}, err
		}
		page.Items[index].Events = events
	}
	return page, nil
}

func (r *PGRepository) GetHistory(ctx context.Context, databaseID, executionID string) (ExecutionAttempt, error) {
	attempt, err := r.getAttempt(ctx, databaseID, executionID)
	if errors.Is(err, ErrNoAttempt) {
		job, jobErr := scanAttempt(r.pool.QueryRow(ctx, rejectedJobSelect+` AND j.id=$2::uuid`, databaseID, executionID))
		if errors.Is(jobErr, pgx.ErrNoRows) {
			return ExecutionAttempt{}, ErrNoAttempt
		}
		return job, mapRepositoryError(jobErr)
	}
	if err != nil {
		return ExecutionAttempt{}, err
	}
	attempt.Events, err = r.executionEvents(ctx, executionID)
	return attempt, err
}

func (r *PGRepository) getAttempt(ctx context.Context, databaseID, executionID string) (ExecutionAttempt, error) {
	attempt, err := scanAttempt(r.pool.QueryRow(ctx, attemptSelect+`WHERE a.database_id=$1::uuid AND a.id=$2::uuid`, databaseID, executionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ExecutionAttempt{}, ErrNoAttempt
	}
	if err != nil {
		return ExecutionAttempt{}, mapRepositoryError(err)
	}
	return attempt, nil
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
		&attempt.RelatedExecutionID, &attempt.CheckpointRootID, &attempt.Status, &attempt.ErrorMessage,
		&attempt.previousMigrationStatus, &attempt.MigrationName, &attempt.JobID, &attempt.ErrorCode, &attempt.targetJournaled)
	if attempt.JobID != nil {
		attempt.ID = ""
	}
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
