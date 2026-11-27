package migration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/observability"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb"
)

const batchCheckpointTable = `CREATE TABLE IF NOT EXISTS dbmp_migration_batch_checkpoints (
	root_execution_id UUID NOT NULL,
	execution_id UUID NOT NULL,
	operation VARCHAR(8) NOT NULL CHECK (operation IN ('up','down')),
	batch_number BIGINT NOT NULL CHECK (batch_number > 0),
	cursor_start BIGINT NOT NULL,
	cursor_end BIGINT NOT NULL CHECK (cursor_end >= cursor_start),
	rows_affected BIGINT NOT NULL CHECK (rows_affected >= 0),
	committed_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (root_execution_id,operation,batch_number)
)`

func (e *PGExecutor) ValidateBatch(ctx context.Context, databaseID string, entry Entry) error {
	if entry.ExecutionMode != ExecutionModeBatch || entry.BatchSelectSQL == nil || entry.BatchSize == nil {
		return ErrInvalidInput
	}
	connection, closeConnection, err := e.executionConnection(ctx, databaseID)
	if err != nil {
		return err
	}
	defer closeConnection()
	if connection.Engine() != targetdb.EnginePostgreSQL {
		return ErrSQLPolicy
	}
	if err := validateSQLPolicy(*entry.BatchSelectSQL); err != nil {
		return err
	}
	return nil
}

func (e *PGExecutor) ExecuteBatch(ctx context.Context, databaseID, executionID string, plan ExecutionPlan, report func(BatchCommitEvent) error) ExecutionResult {
	if plan.Entry.ExecutionMode != ExecutionModeBatch || plan.Entry.BatchSelectSQL == nil || plan.Entry.BatchSize == nil {
		return failedBatch("invalid batch migration definition", ErrInvalidInput)
	}
	connection, closeConnection, err := e.executionConnection(ctx, databaseID)
	if err != nil {
		return failedBatch("target database is unavailable", err)
	}
	defer closeConnection()
	if connection.Engine() != targetdb.EnginePostgreSQL {
		return failedBatch("batch migrations require PostgreSQL", ErrSQLPolicy)
	}
	if !hasLockedTarget(ctx, databaseID) {
		if err := acquireTargetMigrationLock(ctx, connection); err != nil {
			return failedBatch("could not acquire the PostgreSQL migration lock", err)
		}
	}
	if _, err := connection.Exec(ctx, batchCheckpointTable); err != nil {
		return failedBatch("could not initialize batch checkpoint storage", err)
	}
	if _, err := connection.Exec(ctx, batchScopeTable); err != nil {
		return failedBatch("could not initialize batch scope storage", err)
	}
	if plan.Action == ActionRollback {
		return e.executeBatchRollback(ctx, connection, databaseID, executionID, plan, report)
	}
	return e.executeBatchUp(ctx, connection, databaseID, executionID, plan, report)
}

func (e *PGExecutor) executeBatchUp(ctx context.Context, connection targetdb.Connection, databaseID, executionID string, plan ExecutionPlan, report func(BatchCommitEvent) error) ExecutionResult {
	rootID := plan.CheckpointRootID
	if rootID == "" {
		rootID = executionID
	}
	var cursorStart, batchNumber int64
	if err := connection.QueryRow(ctx, `SELECT COALESCE(max(cursor_end),$2),COALESCE(max(batch_number),0)
		FROM dbmp_migration_batch_checkpoints WHERE root_execution_id=$1::uuid AND operation='up'`,
		rootID, plan.Entry.BatchCursorStart).Scan(&cursorStart, &batchNumber); err != nil {
		return failedBatch("could not read batch checkpoint", err)
	}
	if err := prepareBatchScope(ctx, connection, rootID, plan.Entry, batchNumber); err != nil {
		return e.batchFailure(ctx, connection, databaseID, executionID, rootID, "up", "could not establish the original batch scope", err)
	}
	for {
		activity := targetdb.ActivityFromContext(ctx)
		activity.BatchNumber = batchNumber + 1
		batchCtx := targetdb.WithActivity(ctx, activity)
		keys, column, err := loadBatchScope(batchCtx, connection, rootID, batchNumber+1)
		if errors.Is(err, pgx.ErrNoRows) {
			return ExecutionResult{Status: StatusSucceeded}
		}
		if err != nil {
			return e.batchFailure(ctx, connection, databaseID, executionID, rootID, "up", "could not read the batch scope", err)
		}
		cursorEnd := keys[len(keys)-1]
		selectCtx, cancel := context.WithCancel(batchCtx)
		tx, err := connection.Begin(selectCtx)
		if err != nil {
			cancel()
			return e.batchFailure(ctx, connection, databaseID, executionID, rootID, "up", "could not start batch transaction", err)
		}
		rowsAffected, executedSQL, err := executeScopedBatch(selectCtx, tx, plan.SQLSnapshot, column, cursorStart, cursorEnd, keys)
		if err != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			cancel()
			batchNumber := batchNumber + 1
			return e.batchFailure(ctx, connection, databaseID, executionID, rootID, "up", "batch SQL failed", err,
				BatchFailureEvent{QuerySQL: executedSQL, QueryParameters: []int64{cursorStart, cursorEnd},
					BatchNumber: &batchNumber, CursorStart: &cursorStart, CursorEnd: &cursorEnd})
		}
		batchNumber++
		if _, err := tx.Exec(selectCtx, `INSERT INTO dbmp_migration_batch_checkpoints
			(root_execution_id,execution_id,operation,batch_number,cursor_start,cursor_end,rows_affected,committed_at)
			VALUES ($1::uuid,$2::uuid,'up',$3,$4,$5,$6,$7)`,
			rootID, executionID, batchNumber, cursorStart, cursorEnd, rowsAffected, time.Now().UTC()); err != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			cancel()
			return e.batchFailure(ctx, connection, databaseID, executionID, rootID, "up", "could not save batch checkpoint", err,
				BatchFailureEvent{QuerySQL: executedSQL, QueryParameters: []int64{cursorStart, cursorEnd},
					BatchNumber: &batchNumber, CursorStart: &cursorStart, CursorEnd: &cursorEnd})
		}
		commitErr := tx.Commit(selectCtx)
		cancel()
		if commitErr != nil {
			committed, checkErr := e.batchCheckpointExists(batchCtx, connection, rootID, "up", batchNumber)
			if checkErr != nil {
				return ExecutionResult{Status: StatusInterrupted,
					PublicError:   stringPtr("batch commit outcome is uncertain; inspect the target checkpoint before resuming"),
					InternalError: errors.Join(commitErr, checkErr), BatchFailure: &BatchFailureEvent{
						QuerySQL: executedSQL, QueryParameters: []int64{cursorStart, cursorEnd},
						BatchNumber: &batchNumber, CursorStart: &cursorStart, CursorEnd: &cursorEnd,
					}}
			}
			if !committed {
				return e.batchFailure(ctx, connection, databaseID, executionID, rootID, "up", "batch transaction did not commit", commitErr,
					BatchFailureEvent{QuerySQL: executedSQL, QueryParameters: []int64{cursorStart, cursorEnd},
						BatchNumber: &batchNumber, CursorStart: &cursorStart, CursorEnd: &cursorEnd})
			}
		}
		logFields := []any{"event", "migration_batch_committed",
			"database_id", databaseID, "migration_id", plan.Entry.ID, "execution_id", executionID,
			"actor", plan.ActorUsername, "revision", plan.Entry.Revision,
			"batch", batchNumber, "cursor_start", cursorStart, "cursor_end", cursorEnd,
			"rows_affected", rowsAffected, "query", redactSQLForLog(executedSQL),
			"parameters", []int64{cursorStart, cursorEnd}}
		logFields = append(logFields, batchActivityFields(batchCtx)...)
		e.logger.InfoContext(batchCtx, "migration batch committed", logFields...)
		if report != nil {
			if err := report(BatchCommitEvent{Operation: "up", BatchNumber: batchNumber, CursorStart: cursorStart,
				CursorEnd: cursorEnd, RowsAffected: rowsAffected, QuerySQL: executedSQL}); err != nil {
				e.logger.ErrorContext(ctx, "record committed batch in platform history", "event", "migration_batch_history_record_failed",
					"execution_id", executionID, "batch", batchNumber, "error", safeError(err))
			}
		}
		cursorStart = cursorEnd
	}
}

func (e *PGExecutor) executeBatchRollback(ctx context.Context, connection targetdb.Connection, databaseID, executionID string, plan ExecutionPlan, report func(BatchCommitEvent) error) ExecutionResult {
	rootID := plan.CheckpointRootID
	if rootID == "" {
		return failedBatch("rollback has no source batch checkpoint", ErrInvalidInput)
	}
	var lastBatch int64
	if err := connection.QueryRow(ctx, `SELECT COALESCE(max(batch_number),0) FROM dbmp_migration_batch_checkpoints
		WHERE root_execution_id=$1::uuid AND operation='up'`, rootID).Scan(&lastBatch); err != nil {
		return failedBatch("could not read source batch checkpoints", err)
	}
	if plan.Entry.DownSQL == nil {
		return failedBatch("batch migration is irreversible", ErrIrreversible)
	}
	if _, _, err := loadBatchScope(ctx, connection, rootID, 0); err != nil {
		return failedBatch("rollback cannot verify the original row scope; create a repair migration", err)
	}
	for batchNumber := lastBatch; batchNumber > 0; batchNumber-- {
		activity := targetdb.ActivityFromContext(ctx)
		activity.BatchNumber = batchNumber
		batchCtx := targetdb.WithActivity(ctx, activity)
		var cursorStart, cursorEnd int64
		err := connection.QueryRow(batchCtx, `SELECT cursor_start,cursor_end FROM dbmp_migration_batch_checkpoints
			WHERE root_execution_id=$1::uuid AND operation='up' AND batch_number=$2`, rootID, batchNumber).
			Scan(&cursorStart, &cursorEnd)
		if errors.Is(err, pgx.ErrNoRows) {
			return e.batchFailure(ctx, connection, databaseID, executionID, rootID, "down", "source batch checkpoint is missing", err)
		}
		if err != nil {
			return e.batchFailure(ctx, connection, databaseID, executionID, rootID, "down", "could not read source batch range", err)
		}
		committed, err := e.batchCheckpointExists(batchCtx, connection, rootID, "down", batchNumber)
		if err != nil {
			return ExecutionResult{Status: StatusInterrupted, PublicError: stringPtr("rollback checkpoint outcome is uncertain"), InternalError: err}
		}
		if committed {
			continue
		}
		keys, column, err := loadBatchScope(batchCtx, connection, rootID, batchNumber)
		if err != nil {
			return e.batchFailure(ctx, connection, databaseID, executionID, rootID, "down", "rollback cannot verify the original row scope", err)
		}
		tx, err := connection.Begin(batchCtx)
		if err != nil {
			return e.batchFailure(ctx, connection, databaseID, executionID, rootID, "down", "could not start rollback batch transaction", err)
		}
		rowsAffected, executedSQL, err := executeScopedBatch(batchCtx, tx, plan.SQLSnapshot, column, cursorStart, cursorEnd, keys)
		if err != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			return e.batchFailure(ctx, connection, databaseID, executionID, rootID, "down", "rollback batch SQL failed", err,
				BatchFailureEvent{QuerySQL: executedSQL, QueryParameters: []int64{cursorStart, cursorEnd},
					BatchNumber: &batchNumber, CursorStart: &cursorStart, CursorEnd: &cursorEnd})
		}
		if _, err := tx.Exec(batchCtx, `INSERT INTO dbmp_migration_batch_checkpoints
			(root_execution_id,execution_id,operation,batch_number,cursor_start,cursor_end,rows_affected,committed_at)
			VALUES ($1::uuid,$2::uuid,'down',$3,$4,$5,$6,$7)`,
			rootID, executionID, batchNumber, cursorStart, cursorEnd, rowsAffected, time.Now().UTC()); err != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			return e.batchFailure(ctx, connection, databaseID, executionID, rootID, "down", "could not save rollback checkpoint", err,
				BatchFailureEvent{QuerySQL: executedSQL, QueryParameters: []int64{cursorStart, cursorEnd},
					BatchNumber: &batchNumber, CursorStart: &cursorStart, CursorEnd: &cursorEnd})
		}
		if err := tx.Commit(batchCtx); err != nil {
			committed, checkErr := e.batchCheckpointExists(batchCtx, connection, rootID, "down", batchNumber)
			if checkErr != nil {
				return ExecutionResult{Status: StatusInterrupted,
					PublicError:   stringPtr("rollback batch commit outcome is uncertain"),
					InternalError: errors.Join(err, checkErr), BatchFailure: &BatchFailureEvent{
						QuerySQL: executedSQL, QueryParameters: []int64{cursorStart, cursorEnd},
						BatchNumber: &batchNumber, CursorStart: &cursorStart, CursorEnd: &cursorEnd,
					}}
			}
			if !committed {
				return e.batchFailure(ctx, connection, databaseID, executionID, rootID, "down", "rollback batch transaction did not commit", err,
					BatchFailureEvent{QuerySQL: executedSQL, QueryParameters: []int64{cursorStart, cursorEnd},
						BatchNumber: &batchNumber, CursorStart: &cursorStart, CursorEnd: &cursorEnd})
			}
		}
		logFields := []any{"event", "migration_batch_rollback_committed",
			"database_id", databaseID, "migration_id", plan.Entry.ID, "execution_id", executionID,
			"actor", plan.ActorUsername, "revision", plan.Entry.Revision,
			"batch", batchNumber, "cursor_start", cursorStart, "cursor_end", cursorEnd,
			"rows_affected", rowsAffected, "query", redactSQLForLog(executedSQL),
			"parameters", []int64{cursorStart, cursorEnd}}
		logFields = append(logFields, batchActivityFields(batchCtx)...)
		e.logger.InfoContext(batchCtx, "migration rollback batch committed", logFields...)
		if report != nil {
			if err := report(BatchCommitEvent{Operation: "down", BatchNumber: batchNumber, CursorStart: cursorStart,
				CursorEnd: cursorEnd, RowsAffected: rowsAffected, QuerySQL: executedSQL}); err != nil {
				e.logger.ErrorContext(ctx, "record reverted batch in platform history", "event", "migration_batch_history_record_failed",
					"execution_id", executionID, "batch", batchNumber, "error", safeError(err))
			}
		}
	}
	return ExecutionResult{Status: StatusSucceeded}
}

func batchActivityFields(ctx context.Context) []any {
	activity := targetdb.ActivityFromContext(ctx)
	fields := []any{"job_id", activity.JobID, "attempt", activity.Attempt,
		"correlation_id", activity.CorrelationID, "batch_number", activity.BatchNumber}
	return append(fields, observability.TraceFields(ctx)...)
}

func (e *PGExecutor) batchFailure(ctx context.Context, connection targetdb.Connection, databaseID, executionID, rootID, operation, public string, err error, details ...BatchFailureEvent) ExecutionResult {
	var committed int64
	if queryErr := connection.QueryRow(ctx, `SELECT count(*) FROM dbmp_migration_batch_checkpoints
		WHERE root_execution_id=$1::uuid AND operation=$2`, rootID, operation).Scan(&committed); queryErr != nil {
		fields := []any{"event", "migration_batch_checkpoint_uncertain",
			"database_id", databaseID, "execution_id", executionID, "error", safeError(errors.Join(err, queryErr))}
		fields = append(fields, batchActivityFields(ctx)...)
		e.logger.ErrorContext(ctx, "migration batch checkpoint inspection failed", fields...)
		return ExecutionResult{Status: StatusInterrupted, PublicError: stringPtr("batch state is uncertain; inspect target checkpoint evidence before retrying"), InternalError: errors.Join(err, queryErr)}
	}
	status := StatusFailed
	if operation == "down" && committed > 0 {
		status = StatusPartiallyReverted
	} else if operation == "up" && committed > 0 {
		status = StatusPartial
	}
	logCtx := ctx
	fields := []any{"event", "migration_batch_failed",
		"database_id", databaseID, "execution_id", executionID, "committed_batches", committed,
		"operation", operation, "error", safeError(err)}
	if len(details) > 0 {
		failure := details[0]
		activity := targetdb.ActivityFromContext(ctx)
		if failure.BatchNumber != nil {
			activity.BatchNumber = *failure.BatchNumber
		}
		logCtx = targetdb.WithActivity(ctx, activity)
		fields = append(fields, "query", redactSQLForLog(failure.QuerySQL),
			"parameters", failure.QueryParameters)
		if failure.CursorStart != nil {
			fields = append(fields, "cursor_start", *failure.CursorStart)
		}
		if failure.CursorEnd != nil {
			fields = append(fields, "cursor_end", *failure.CursorEnd)
		}
	}
	fields = append(fields, batchActivityFields(logCtx)...)
	e.logger.ErrorContext(logCtx, "migration batch failed", fields...)
	result := ExecutionResult{Status: status, PublicError: stringPtr(public + ": " + sanitizeTargetError(err)), InternalError: err}
	if len(details) > 0 {
		result.BatchFailure = &details[0]
	}
	return result
}

func (e *PGExecutor) batchCheckpointExists(ctx context.Context, connection targetdb.Connection, rootID, operation string, batchNumber int64) (bool, error) {
	var exists bool
	err := connection.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM dbmp_migration_batch_checkpoints
		WHERE root_execution_id=$1::uuid AND operation=$2 AND batch_number=$3)`, rootID, operation, batchNumber).Scan(&exists)
	return exists, err
}

func (e *PGExecutor) BatchProgress(ctx context.Context, databaseID string, attempt ExecutionAttempt) (BatchProgress, error) {
	connection, err := e.provider.Open(ctx, databaseID)
	if err != nil {
		return BatchProgress{}, err
	}
	defer func() { _ = connection.Close(context.WithoutCancel(ctx)) }()
	if connection.Engine() != targetdb.EnginePostgreSQL {
		return BatchProgress{}, ErrSQLPolicy
	}
	operation := "up"
	if attempt.Action == ActionRollback {
		operation = "down"
	}
	var progress BatchProgress
	progress.ExecutionID, progress.Status = attempt.ID, attempt.Status
	err = connection.QueryRow(ctx, `SELECT count(*),COALESCE(sum(rows_affected),0),
		CASE WHEN $2='down' THEN COALESCE(min(cursor_start),0) ELSE COALESCE(max(cursor_end),0) END
		FROM dbmp_migration_batch_checkpoints WHERE root_execution_id=$1::uuid AND operation=$2`,
		attempt.CheckpointRootID, operation).Scan(&progress.CommittedBatches, &progress.CommittedRows, &progress.LastCursor)
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "does not exist") {
		return BatchProgress{}, err
	}
	return progress, nil
}

func (e *PGExecutor) InspectBatchCheckpoints(ctx context.Context, databaseID string, attempt ExecutionAttempt) (BatchCheckpointState, error) {
	connection, err := e.provider.Open(ctx, databaseID)
	if err != nil {
		return BatchCheckpointState{}, err
	}
	defer func() { _ = connection.Close(context.WithoutCancel(ctx)) }()
	if connection.Engine() != targetdb.EnginePostgreSQL {
		return BatchCheckpointState{}, ErrSQLPolicy
	}

	var state BatchCheckpointState
	var upMin, upMax, downMin, downMax int64
	err = connection.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE operation='up'),
		COALESCE(min(batch_number) FILTER (WHERE operation='up'),0),
		COALESCE(max(batch_number) FILTER (WHERE operation='up'),0),
		count(*) FILTER (WHERE operation='down'),
		COALESCE(min(batch_number) FILTER (WHERE operation='down'),0),
		COALESCE(max(batch_number) FILTER (WHERE operation='down'),0)
		FROM dbmp_migration_batch_checkpoints WHERE root_execution_id=$1::uuid`, attempt.CheckpointRootID).
		Scan(&state.UpBatches, &upMin, &upMax, &state.DownBatches, &downMin, &downMax)
	if err != nil {
		return BatchCheckpointState{}, err
	}
	if state.UpBatches == 0 {
		if state.DownBatches != 0 {
			return BatchCheckpointState{}, fmt.Errorf("rollback checkpoints exist without source batches")
		}
		return state, nil
	}
	if upMin != 1 || upMax != state.UpBatches || state.DownBatches > state.UpBatches {
		return BatchCheckpointState{}, fmt.Errorf("batch checkpoint sequence is inconsistent")
	}
	if state.DownBatches > 0 &&
		(downMin != state.UpBatches-state.DownBatches+1 || downMax != state.UpBatches) {
		return BatchCheckpointState{}, fmt.Errorf("rollback checkpoint sequence is inconsistent")
	}
	var orphanedDown int64
	if err := connection.QueryRow(ctx, `SELECT count(*) FROM dbmp_migration_batch_checkpoints d
		WHERE d.root_execution_id=$1::uuid AND d.operation='down' AND NOT EXISTS (
			SELECT 1 FROM dbmp_migration_batch_checkpoints u
			WHERE u.root_execution_id=d.root_execution_id AND u.operation='up' AND u.batch_number=d.batch_number
		)`, attempt.CheckpointRootID).Scan(&orphanedDown); err != nil {
		return BatchCheckpointState{}, err
	}
	if orphanedDown != 0 {
		return BatchCheckpointState{}, fmt.Errorf("rollback checkpoint has no source batch")
	}
	return state, nil
}

func integerCursor(value any) (int64, bool) {
	switch number := value.(type) {
	case int:
		return int64(number), true
	case int16:
		return int64(number), true
	case int32:
		return int64(number), true
	case int64:
		return number, true
	case uint:
		return int64(number), uint64(number) <= uint64(^uint64(0)>>1)
	case uint32:
		return int64(number), true
	case uint64:
		if number > uint64(^uint64(0)>>1) {
			return 0, false
		}
		return int64(number), true
	default:
		return 0, false
	}
}

func failedBatch(message string, err error) ExecutionResult {
	return ExecutionResult{Status: StatusFailed, PublicError: stringPtr(message), InternalError: fmt.Errorf("%s: %w", message, err)}
}
