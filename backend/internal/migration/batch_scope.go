package migration

import (
	"context"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb"
)

const batchScopeTable = `CREATE TABLE IF NOT EXISTS dbmp_migration_batch_scope (
	root_execution_id UUID NOT NULL,
	batch_number BIGINT NOT NULL CHECK (batch_number >= 0),
	cursor_column TEXT NOT NULL,
	cursor_values BIGINT[] NOT NULL,
	CHECK ((batch_number = 0) = (cardinality(cursor_values) = 0)),
	PRIMARY KEY (root_execution_id,batch_number)
)`

func loadBatchScope(ctx context.Context, connection targetdb.Connection, rootID string, batchNumber int64) ([]int64, string, error) {
	var keys []int64
	var column string
	err := connection.QueryRow(ctx, `SELECT cursor_values,cursor_column FROM dbmp_migration_batch_scope
		WHERE root_execution_id=$1::uuid AND batch_number=$2`, rootID, batchNumber).Scan(&keys, &column)
	return keys, column, err
}

func prepareBatchScope(ctx context.Context, connection targetdb.Connection, rootID string, entry Entry, committedBatches int64) error {
	_, _, err := loadBatchScope(ctx, connection, rootID, 0)
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if committedBatches > 0 {
		return errors.New("the original row scope was not recorded; create a repair migration")
	}
	tx, err := connection.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ"); err != nil {
		return err
	}

	cursor := entry.BatchCursorStart
	for batchNumber := int64(1); ; batchNumber++ {
		keys, column, err := selectBatchKeys(ctx, tx, *entry.BatchSelectSQL, cursor, *entry.BatchSize)
		if err != nil {
			return err
		}
		if len(keys) == 0 {
			// Batch zero marks even an empty scope as fully captured.
			if err := insertBatchScope(ctx, tx, rootID, 0, column, keys); err != nil {
				return err
			}
			return tx.Commit(ctx)
		}
		if err := insertBatchScope(ctx, tx, rootID, batchNumber, column, keys); err != nil {
			return err
		}
		cursor = keys[len(keys)-1]
	}
}

func insertBatchScope(ctx context.Context, tx targetdb.Transaction, rootID string, batchNumber int64, column string, keys []int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO dbmp_migration_batch_scope
		(root_execution_id,batch_number,cursor_column,cursor_values) VALUES ($1::uuid,$2,$3,$4)`,
		rootID, batchNumber, column, keys)
	return err
}

func selectBatchKeys(ctx context.Context, tx targetdb.Transaction, selector string, cursor int64, size int) ([]int64, string, error) {
	ctx = targetdb.WithSafeParameterIndexes(ctx, 0, 1)
	rows, err := tx.Query(ctx, selector, cursor, size)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	column, ok := targetdb.PostgreSQLSingleColumn(rows)
	if !ok {
		return nil, "", errors.New("batch selector must return one named integer cursor column")
	}
	keys := make([]int64, 0, size)
	for rows.Next() {
		key, err := scanBatchKey(rows)
		if err != nil {
			return nil, "", err
		}
		if len(keys) == size || key <= cursor {
			return nil, "", errors.New("batch selector must return unique, increasing cursors within the configured size")
		}
		keys = append(keys, key)
		cursor = key
	}
	return keys, column, rows.Err()
}

func scanBatchKey(rows targetdb.Rows) (int64, error) {
	var raw any
	if err := rows.Scan(&raw); err != nil {
		return 0, err
	}
	key, ok := integerCursor(raw)
	if !ok {
		return 0, errors.New("batch row cursor must be an integer")
	}
	return key, nil
}

func executeScopedBatch(ctx context.Context, tx targetdb.Transaction, sql, cursorColumn string, start, end int64, expected []int64) (int64, error) {
	statements, err := splitStatements(sql, targetdb.EnginePostgreSQL)
	if err != nil || len(statements) != 1 {
		return 0, ErrSQLPolicy
	}
	words, err := topLevelStatementWords(statements[0])
	if err != nil || len(words) != 1 || !batchStatement(words[0][0]) {
		return 0, ErrSQLPolicy
	}
	column := pgx.Identifier{cursorColumn}.Sanitize()
	query := statements[0] + "\nRETURNING " + column
	if containsWord(words[0], "RETURNING") {
		query = "WITH dbmp_changed AS (\n" + statements[0] +
			"\n, " + column + " AS __dbmp_cursor) SELECT __dbmp_cursor FROM dbmp_changed"
	}
	ctx = targetdb.WithSafeParameterIndexes(ctx, 0, 1)
	rows, err := tx.Query(ctx, query, start, end)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	actual := make([]int64, 0, len(expected))
	scopeError := errors.New("affected rows differ from the original batch scope; no changes from this batch were committed")
	for rows.Next() {
		if len(actual) == len(expected) {
			return 0, scopeError
		}
		key, err := scanBatchKey(rows)
		if err != nil {
			return 0, err
		}
		actual = append(actual, key)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	slices.Sort(actual)
	if !slices.Equal(actual, expected) {
		return 0, scopeError
	}
	return int64(len(actual)), nil
}
