package migration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb"
)

type PGExecutor struct {
	provider targetdb.Provider
}

func NewPGExecutor(provider targetdb.Provider) *PGExecutor { return &PGExecutor{provider: provider} }

func (e *PGExecutor) Validate(ctx context.Context, databaseID, sqlText string) error {
	connection, err := e.provider.Open(ctx, databaseID)
	if err != nil {
		return err
	}
	defer func() { _ = connection.Close(context.WithoutCancel(ctx)) }()
	if connection.Engine() == targetdb.EngineMySQL {
		return validateMySQLMigrationPolicy(sqlText)
	}
	return nil
}

func (e *PGExecutor) Execute(ctx context.Context, databaseID, sqlText string) ExecutionResult {
	connection, err := e.provider.Open(ctx, databaseID)
	if err != nil {
		message := "target database is unavailable"
		return ExecutionResult{Status: StatusFailed, PublicError: &message, InternalError: fmt.Errorf("open target: %w", err)}
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = connection.Close(closeCtx)
	}()

	if connection.Engine() == targetdb.EngineMySQL {
		mode, err := classifyMySQLMigration(sqlText)
		if err != nil {
			message := err.Error()
			return ExecutionResult{Status: StatusFailed, PublicError: &message, InternalError: err}
		}
		if mode == mysqlMigrationDDL {
			statements, err := targetdb.SplitMySQLScript(sqlText)
			if err != nil {
				message := "MySQL DDL could not be safely split into statements"
				return ExecutionResult{Status: StatusFailed, PublicError: &message, InternalError: err}
			}
			for index, statement := range statements {
				if _, err := connection.Exec(ctx, statement); err != nil {
					message := fmt.Sprintf("DDL statement %d of %d failed; earlier changes or this statement may already be applied. Reconcile the target before running another migration. %s",
						index+1, len(statements), sanitizeTargetError(err))
					return ExecutionResult{
						Status: StatusNeedsReconciliation, PublicError: &message,
						InternalError: fmt.Errorf("execute MySQL DDL statement %d of %d: %w", index+1, len(statements), err),
					}
				}
			}
			return ExecutionResult{Status: StatusSucceeded}
		}
	}

	tx, err := connection.Begin(ctx)
	if err != nil {
		message := "could not start target transaction"
		return ExecutionResult{Status: StatusFailed, PublicError: &message, InternalError: fmt.Errorf("begin target transaction: %w", err)}
	}

	if _, err := tx.ExecScript(ctx, sqlText); err != nil {
		public := sanitizeTargetError(err)
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if rollbackErr := tx.Rollback(rollbackCtx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) && !errors.Is(rollbackErr, sql.ErrTxDone) {
			message := "migration outcome is uncertain; manual reconciliation is required"
			return ExecutionResult{
				Status: StatusInterrupted, PublicError: &message,
				InternalError: errors.Join(fmt.Errorf("execute target migration: %w", err), fmt.Errorf("rollback target migration: %w", rollbackErr)),
			}
		}
		return ExecutionResult{Status: StatusFailed, PublicError: &public, InternalError: fmt.Errorf("execute target migration: %w", err)}
	}

	if err := tx.Commit(ctx); err != nil {
		message := "migration outcome is uncertain; manual reconciliation is required"
		return ExecutionResult{Status: StatusInterrupted, PublicError: &message, InternalError: fmt.Errorf("commit target migration: %w", err)}
	}
	return ExecutionResult{Status: StatusSucceeded}
}

var credentialPattern = regexp.MustCompile(`(?i)(password|passwd|pwd|user|username|host|dbname|database|sslkey|sslcert)\s*=\s*([^\s;]+)`)
var postgresURLPattern = regexp.MustCompile(`(?i)(postgres(?:ql)?://[^:/@\s]+:)[^@\s]+@`)

func sanitizeTargetError(err error) string {
	message := strings.TrimSpace(err.Error())
	message = postgresURLPattern.ReplaceAllString(message, "$1[redacted]@")
	message = credentialPattern.ReplaceAllString(message, "$1=[redacted]")
	if len(message) > 1000 {
		message = message[:1000] + "…"
	}
	if message == "" {
		return "target SQL statement failed"
	}
	return message
}
