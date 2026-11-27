package migration

import (
	"context"
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

// Target-side batch roots let aliases identify the same database without
// relying on hostnames, ports, or the credentials used to connect.
func checkUnresolvedTargetBatches(ctx context.Context, roots []string) error {
	target, ok := ctx.Value(lockedTargetKey{}).(lockedTarget)
	if !ok || len(roots) == 0 {
		return nil
	}
	tables, err := targetBatchTables(ctx, target.connection)
	if err != nil {
		return err
	}
	for _, table := range tables {
		var blocked bool
		err := target.connection.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1 FROM `+table+` WHERE root_execution_id IN (
				SELECT id::uuid FROM unnest($1::text[]) AS roots(id)
			))`, roots).Scan(&blocked)
		if err != nil {
			return err
		}
		if blocked {
			return ErrTargetBatchBlocked
		}
	}
	return nil
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

func (r *PGRepository) unresolvedBatchRoots(ctx context.Context, databaseID string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT a.checkpoint_root_id::text
		FROM migration_entries e
		JOIN migration_execution_attempts a ON a.id=e.last_execution_id
		WHERE e.database_id<>$1::uuid AND e.execution_mode='batch'
		  AND e.status IN ('running','partial','partially_reverted','interrupted','needs_reconciliation')`, databaseID)
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	defer rows.Close()
	var roots []string
	for rows.Next() {
		var root string
		if err := rows.Scan(&root); err != nil {
			return nil, mapRepositoryError(err)
		}
		roots = append(roots, root)
	}
	return roots, mapRepositoryError(rows.Err())
}
