package migration

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

type fakeTargetProvider struct {
	connection *fakeTargetConnection
	err        error
}

func (f *fakeTargetProvider) Open(context.Context, string) (TargetConnection, error) {
	return f.connection, f.err
}

type fakeTargetConnection struct {
	tx       *fakeTargetTransaction
	beginErr error
	closed   bool
}

func (f *fakeTargetConnection) Begin(context.Context) (TargetTransaction, error) {
	return f.tx, f.beginErr
}
func (f *fakeTargetConnection) Close(context.Context) error { f.closed = true; return nil }

type fakeTargetTransaction struct {
	execErr, commitErr, rollbackErr error
	executed, committed, rolledBack bool
}

func (f *fakeTargetTransaction) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	f.executed = true
	return pgconn.CommandTag{}, f.execErr
}
func (f *fakeTargetTransaction) Commit(context.Context) error { f.committed = true; return f.commitErr }
func (f *fakeTargetTransaction) Rollback(context.Context) error {
	f.rolledBack = true
	return f.rollbackErr
}

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
