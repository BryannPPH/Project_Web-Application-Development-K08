package migration

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb"
)

type fakeTargetProvider struct {
	connection *fakeTargetConnection
	err        error
}

func (f *fakeTargetProvider) Open(context.Context, string) (targetdb.Connection, error) {
	return f.connection, f.err
}

type fakeTargetConnection struct {
	tx       *fakeTargetTransaction
	beginErr error
	closed   bool
	engine   targetdb.Engine
}

func (f *fakeTargetConnection) Engine() targetdb.Engine {
	return targetdb.NormalizeEngine(f.engine)
}
func (f *fakeTargetConnection) Query(context.Context, string, ...any) (targetdb.Rows, error) {
	return nil, errors.New("not configured")
}
func (f *fakeTargetConnection) QueryRow(context.Context, string, ...any) targetdb.Row {
	return fakeRow{err: errors.New("not configured")}
}
func (f *fakeTargetConnection) Exec(context.Context, string, ...any) (targetdb.CommandResult, error) {
	return nil, errors.New("not configured")
}
func (f *fakeTargetConnection) Begin(context.Context, ...targetdb.TxOptions) (targetdb.Transaction, error) {
	return f.tx, f.beginErr
}
func (f *fakeTargetConnection) Close(context.Context) error { f.closed = true; return nil }

type fakeTargetTransaction struct {
	execErr, commitErr, rollbackErr error
	executed, committed, rolledBack bool
}

func (f *fakeTargetTransaction) Exec(context.Context, string, ...any) (targetdb.CommandResult, error) {
	return nil, f.execErr
}
func (f *fakeTargetTransaction) ExecScript(context.Context, string) (targetdb.CommandResult, error) {
	f.executed = true
	return nil, f.execErr
}
func (f *fakeTargetTransaction) Query(context.Context, string, ...any) (targetdb.Rows, error) {
	return nil, errors.New("not configured")
}
func (f *fakeTargetTransaction) QueryRow(context.Context, string, ...any) targetdb.Row {
	return fakeRow{err: errors.New("not configured")}
}
func (f *fakeTargetTransaction) Commit(context.Context) error { f.committed = true; return f.commitErr }
func (f *fakeTargetTransaction) Rollback(context.Context) error {
	f.rolledBack = true
	return f.rollbackErr
}

type fakeRow struct{ err error }

func (r fakeRow) Scan(...any) error { return r.err }

func TestPGExecutorCommitsSuccessfulMigration(t *testing.T) {
	t.Parallel()
	tx := &fakeTargetTransaction{}
	connection := &fakeTargetConnection{tx: tx}
	result := NewPGExecutor(&fakeTargetProvider{connection: connection}).Execute(context.Background(), "database", "SELECT 1")
	if result.Status != StatusSucceeded || !tx.executed || !tx.committed || tx.rolledBack || !connection.closed {
		t.Fatalf("unexpected successful result: %#v tx=%#v closed=%v", result, tx, connection.closed)
	}
}

func TestPGExecutorRollsBackSQLFailure(t *testing.T) {
	t.Parallel()
	tx := &fakeTargetTransaction{execErr: errors.New("syntax error password=secret")}
	result := NewPGExecutor(&fakeTargetProvider{connection: &fakeTargetConnection{tx: tx}}).Execute(context.Background(), "database", "bad sql")
	if result.Status != StatusFailed || !tx.rolledBack || result.PublicError == nil || *result.PublicError == "" {
		t.Fatalf("unexpected failure result: %#v tx=%#v", result, tx)
	}
	if *result.PublicError == "syntax error password=secret" {
		t.Fatal("credential-like value was not redacted")
	}
}

func TestPGExecutorMarksUncertainRollbackInterrupted(t *testing.T) {
	t.Parallel()
	tx := &fakeTargetTransaction{execErr: errors.New("execution failed"), rollbackErr: errors.New("connection lost")}
	result := NewPGExecutor(&fakeTargetProvider{connection: &fakeTargetConnection{tx: tx}}).Execute(context.Background(), "database", "bad sql")
	if result.Status != StatusInterrupted {
		t.Fatalf("status = %s, want interrupted", result.Status)
	}
}

func TestPGExecutorMarksCommitErrorInterrupted(t *testing.T) {
	t.Parallel()
	tx := &fakeTargetTransaction{commitErr: errors.New("connection lost")}
	result := NewPGExecutor(&fakeTargetProvider{connection: &fakeTargetConnection{tx: tx}}).Execute(context.Background(), "database", "SELECT 1")
	if result.Status != StatusInterrupted {
		t.Fatalf("status = %s, want interrupted", result.Status)
	}
}

func TestSanitizeTargetErrorRemovesDSNCredentials(t *testing.T) {
	t.Parallel()
	message := sanitizeTargetError(errors.New("dial postgres://owner:super-secret@db.internal:5432/app failed"))
	if strings.Contains(message, "super-secret") {
		t.Fatalf("sanitized error leaked password: %s", message)
	}
}

func TestValidateRejectsMySQLDDL(t *testing.T) {
	t.Parallel()
	connection := &fakeTargetConnection{engine: targetdb.EngineMySQL}
	executor := NewPGExecutor(&fakeTargetProvider{connection: connection})
	if err := executor.Validate(context.Background(), "database", "CREATE TABLE items(id bigint)"); !errors.Is(err, ErrSQLPolicy) {
		t.Fatalf("Validate() error = %v, want ErrSQLPolicy", err)
	}
	if err := executor.Validate(context.Background(), "database", "INSERT INTO items VALUES (1); UPDATE items SET id=2"); err != nil {
		t.Fatalf("Validate(transactional DML) error = %v", err)
	}
}
