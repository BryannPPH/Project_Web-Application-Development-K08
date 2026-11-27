package migration

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb"
)

type lockedTargetKey struct{}

type lockedTarget struct {
	databaseID string
	connection targetdb.Connection
}

func hasLockedTarget(ctx context.Context, databaseID string) bool {
	target, ok := ctx.Value(lockedTargetKey{}).(lockedTarget)
	return ok && target.databaseID == databaseID
}

func hasLockedPostgreSQLTarget(ctx context.Context) bool {
	target, ok := ctx.Value(lockedTargetKey{}).(lockedTarget)
	return ok && target.connection.Engine() == targetdb.EnginePostgreSQL
}

func (e *PGExecutor) executionConnection(ctx context.Context, databaseID string) (targetdb.Connection, func(), error) {
	if target, ok := ctx.Value(lockedTargetKey{}).(lockedTarget); ok && target.databaseID == databaseID {
		return target.connection, func() {}, nil
	}
	connection, err := e.provider.Open(ctx, databaseID)
	if err != nil {
		return nil, nil, err
	}
	return connection, func() {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = connection.Close(closeCtx)
	}, nil
}

func (e *PGExecutor) lockExecutionTarget(ctx context.Context, databaseID string) (context.Context, func(), error) {
	connection, closeConnection, err := e.executionConnection(ctx, databaseID)
	if err != nil {
		return ctx, nil, err
	}
	if connection.Engine() == targetdb.EnginePostgreSQL {
		if err := acquireTargetMigrationLock(ctx, connection); err != nil {
			closeConnection()
			return ctx, nil, fmt.Errorf("acquire PostgreSQL migration lock: %w", err)
		}
	}
	return context.WithValue(ctx, lockedTargetKey{}, lockedTarget{databaseID, connection}), closeConnection, nil
}

type unresolvedTargetExecutions struct {
	batchRoots            []string
	singleIDs             []string
	legacySingleDatabases []string
}

const targetJournal = "public.dbmp_migration_target_journal"

// The intent is committed before user SQL. The outcome flag is written in the
// same target transaction as user SQL, providing evidence after a crash.
func recordTargetIntent(ctx context.Context, executionID string) error {
	target := ctx.Value(lockedTargetKey{}).(lockedTarget)
	present, err := targetJournalExists(ctx, target.connection)
	if err != nil {
		return err
	}
	if !present {
		if _, err := target.connection.Exec(ctx, `CREATE TABLE public.dbmp_migration_target_journal (
			execution_id UUID PRIMARY KEY,
			committed BOOLEAN NOT NULL DEFAULT FALSE
		);
		GRANT SELECT (execution_id) ON public.dbmp_migration_target_journal TO PUBLIC`); err != nil {
			return fmt.Errorf("initialize target migration journal: %w", err)
		}
	}
	if _, err := target.connection.Exec(ctx, `INSERT INTO public.dbmp_migration_target_journal (execution_id) VALUES ($1::uuid)`, executionID); err != nil {
		return fmt.Errorf("record target execution intent: %w", err)
	}
	return nil
}

func targetJournalExists(ctx context.Context, connection targetdb.Connection) (bool, error) {
	var present bool
	err := connection.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", targetJournal).Scan(&present)
	return present, err
}

func markTargetCommitted(ctx context.Context, tx targetdb.Transaction, executionID string) error {
	result, err := tx.Exec(ctx, `UPDATE public.dbmp_migration_target_journal SET committed=TRUE WHERE execution_id=$1::uuid`, executionID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return errors.New("target migration journal entry is missing")
	}
	return nil
}

func (e *PGExecutor) inspectSingleTargetCommit(ctx context.Context, databaseID, executionID string) (bool, bool, error) {
	targetCtx, release, err := e.lockExecutionTarget(ctx, databaseID)
	if err != nil {
		return false, false, err
	}
	defer release()
	target := targetCtx.Value(lockedTargetKey{}).(lockedTarget)
	if target.connection.Engine() != targetdb.EnginePostgreSQL {
		return false, false, nil
	}
	present, err := targetJournalExists(targetCtx, target.connection)
	if err != nil || !present {
		return false, false, err
	}
	var committed bool
	err = target.connection.QueryRow(targetCtx, `SELECT committed FROM public.dbmp_migration_target_journal WHERE execution_id=$1::uuid`, executionID).Scan(&committed)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil
	}
	return err == nil, committed, err
}

func checkUnresolvedTarget(ctx context.Context, holds unresolvedTargetExecutions) error {
	target := ctx.Value(lockedTargetKey{}).(lockedTarget)
	if len(holds.singleIDs) > 0 {
		present, err := targetJournalExists(ctx, target.connection)
		if err != nil {
			return err
		}
		if present {
			blocked, err := targetJournalMatches(ctx, target.connection, targetJournal, "execution_id", holds.singleIDs)
			if err != nil {
				return err
			}
			if blocked {
				return ErrTargetOutcomeUncertain
			}
		}
	}
	// Old executions lack target-side evidence. Hold matching database names
	// conservatively until a reviewer reconciles those legacy attempts.
	if len(holds.legacySingleDatabases) > 0 {
		var blocked bool
		err := target.connection.QueryRow(ctx, `SELECT current_database() = ANY($1::text[])`, holds.legacySingleDatabases).Scan(&blocked)
		if err != nil {
			return err
		}
		if blocked {
			return ErrTargetOutcomeUncertain
		}
	}
	if len(holds.batchRoots) == 0 {
		return nil
	}
	tables, err := targetBatchTables(ctx, target.connection)
	if err != nil {
		return err
	}
	for _, table := range tables {
		blocked, err := targetJournalMatches(ctx, target.connection, table, "root_execution_id", holds.batchRoots)
		if err != nil {
			return err
		}
		if blocked {
			return ErrTargetBatchBlocked
		}
	}
	return nil
}

func targetJournalMatches(ctx context.Context, connection targetdb.Connection, table, column string, ids []string) (bool, error) {
	var found bool
	// Table and column names come from constants or quoted catalog identifiers.
	err := connection.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM `+table+` WHERE `+pgx.Identifier{column}.Sanitize()+` IN (
			SELECT id::uuid FROM unnest($1::text[]) AS execution_ids(id)
		))`, ids).Scan(&found)
	return found, err
}

func targetBatchTables(ctx context.Context, connection targetdb.Connection) ([]string, error) {
	rows, err := connection.Query(ctx, `SELECT n.nspname,c.relname
		FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
		WHERE c.relkind IN ('r','p')
		  AND c.relname IN ('dbmp_migration_batch_scope','dbmp_migration_batch_checkpoints')
		  AND c.relpersistence <> 't'
		ORDER BY (c.relname='dbmp_migration_batch_scope') DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var schema, table string
		if err := rows.Scan(&schema, &table); err != nil {
			return nil, err
		}
		tables = append(tables, pgx.Identifier{schema, table}.Sanitize())
	}
	return tables, rows.Err()
}

func (r *PGRepository) unresolvedTargetExecutions(ctx context.Context, databaseID string) (unresolvedTargetExecutions, error) {
	var holds unresolvedTargetExecutions
	rows, err := r.pool.Query(ctx, `SELECT e.execution_mode,a.id::text,a.checkpoint_root_id::text,a.target_journaled,d.database_name
		FROM migration_entries e
		JOIN migration_execution_attempts a ON a.id=e.last_execution_id
		JOIN managed_databases d ON d.id=e.database_id
		WHERE e.database_id<>$1::uuid AND d.engine='postgresql'
		  AND e.status IN ('running','partial','partially_reverted','interrupted','needs_reconciliation')`, databaseID)
	if err != nil {
		return holds, mapRepositoryError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var mode ExecutionMode
		var executionID, rootID, name string
		var journaled bool
		if err := rows.Scan(&mode, &executionID, &rootID, &journaled, &name); err != nil {
			return holds, mapRepositoryError(err)
		}
		switch {
		case mode == ExecutionModeBatch:
			holds.batchRoots = append(holds.batchRoots, rootID)
		case journaled:
			holds.singleIDs = append(holds.singleIDs, executionID)
		default:
			holds.legacySingleDatabases = append(holds.legacySingleDatabases, name)
		}
	}
	return holds, mapRepositoryError(rows.Err())
}
