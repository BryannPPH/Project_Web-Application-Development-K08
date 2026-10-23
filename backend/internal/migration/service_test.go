package migration

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz"
)

type fakeAuthorizer struct {
	viewErr, editErr error
	editCalls        int
}

func (f *fakeAuthorizer) RequireDatabaseView(context.Context, auth.Principal, string) error {
	return f.viewErr
}
func (f *fakeAuthorizer) RequireDatabaseEdit(context.Context, auth.Principal, string) error {
	f.editCalls++
	return f.editErr
}

type fakeRepository struct {
	entry                                 Entry
	page                                  Page
	getCalls, runningCalls, terminalCalls int
	terminalErr                           error
	recovered                             int64
	recoverErr                            error
	recoveryReason                        string
}

func (f *fakeRepository) CreateDraft(_ context.Context, databaseID, name, sqlText, creatorID string, now time.Time) (Entry, error) {
	return Entry{ID: "migration", DatabaseID: databaseID, Version: 1, Name: name, SQL: sqlText, Status: StatusDraft, CreatedBy: creatorID, CreatedAt: now}, nil
}
func (f *fakeRepository) UpdateDraft(_ context.Context, databaseID, migrationID, name, sqlText string, _ time.Time) (Entry, error) {
	return Entry{ID: migrationID, DatabaseID: databaseID, Name: name, SQL: sqlText, Status: StatusDraft}, nil
}
func (f *fakeRepository) List(context.Context, string, ListOptions) (Page, error) { return f.page, nil }
func (f *fakeRepository) Get(context.Context, string, string) (Entry, error) {
	f.getCalls++
	return f.entry, nil
}
func (f *fakeRepository) MarkRunning(_ context.Context, _, _, executorID string, now time.Time) (Entry, error) {
	f.runningCalls++
	entry := f.entry
	entry.Status, entry.ExecutedBy, entry.StartedAt = StatusRunning, &executorID, &now
	return entry, nil
}
func (f *fakeRepository) MarkTerminal(_ context.Context, _, _ string, status Status, message *string, now time.Time) (Entry, error) {
	f.terminalCalls++
	if f.terminalErr != nil {
		return Entry{}, f.terminalErr
	}
	entry := f.entry
	entry.Status, entry.ErrorMessage, entry.FinishedAt = status, message, &now
	return entry, nil
}
func (f *fakeRepository) RecoverRunning(_ context.Context, _ time.Time, reason string) (int64, error) {
	f.recoveryReason = reason
	return f.recovered, f.recoverErr
}

type fakeLock struct{ released bool }

func (f *fakeLock) Release(context.Context) error { f.released = true; return nil }

type fakeLocker struct {
	lock       *fakeLock
	acquireErr error
}

func (f *fakeLocker) Acquire(context.Context, string) (ExecutionLock, error) {
	return f.lock, f.acquireErr
}

type fakeExecutor struct {
	result ExecutionResult
	calls  int
}

func (f *fakeExecutor) Execute(context.Context, string, string) ExecutionResult {
	f.calls++
	return f.result
}

type fixedClock struct{ current time.Time }

func (f fixedClock) Now() time.Time { return f.current }

func newTestService(t *testing.T, repository *fakeRepository, authorizer *fakeAuthorizer, locker *fakeLocker, executor *fakeExecutor) *Service {
	t.Helper()
	service, err := NewService(repository, authorizer, locker, executor,
		fixedClock{current: time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)},
		slog.New(slog.NewTextHandler(io.Discard, nil)), ServiceConfig{})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestExecuteRechecksAuthorizationAfterLockAndFinalizes(t *testing.T) {
	t.Parallel()
	repository := &fakeRepository{entry: Entry{ID: "migration", DatabaseID: "database", Status: StatusDraft, SQL: "SELECT 1"}}
	authorizer := &fakeAuthorizer{}
	lock := &fakeLock{}
	executor := &fakeExecutor{result: ExecutionResult{Status: StatusSucceeded}}
	service := newTestService(t, repository, authorizer, &fakeLocker{lock: lock}, executor)

	entry, err := service.Execute(context.Background(), auth.Principal{UserID: "user"}, "database", "migration")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if entry.Status != StatusSucceeded || authorizer.editCalls != 2 || repository.runningCalls != 1 || repository.terminalCalls != 1 || executor.calls != 1 || !lock.released {
		t.Fatalf("unexpected orchestration: entry=%#v auth=%d running=%d terminal=%d execute=%d release=%v", entry, authorizer.editCalls, repository.runningCalls, repository.terminalCalls, executor.calls, lock.released)
	}
}

func TestExecuteDoesNotRunNonDraft(t *testing.T) {
	t.Parallel()
	repository := &fakeRepository{entry: Entry{Status: StatusSucceeded, SQL: "SELECT 1"}}
	executor := &fakeExecutor{}
	service := newTestService(t, repository, &fakeAuthorizer{}, &fakeLocker{lock: &fakeLock{}}, executor)
	_, err := service.Execute(context.Background(), auth.Principal{UserID: "user"}, "database", "migration")
	if !errors.Is(err, ErrConflict) || executor.calls != 0 {
		t.Fatalf("Execute() error=%v calls=%d", err, executor.calls)
	}
}

func TestExecutePersistsInterruptedOutcome(t *testing.T) {
	t.Parallel()
	repository := &fakeRepository{entry: Entry{Status: StatusDraft, SQL: "SELECT 1"}}
	message := "manual reconciliation required"
	executor := &fakeExecutor{result: ExecutionResult{Status: StatusInterrupted, PublicError: &message}}
	service := newTestService(t, repository, &fakeAuthorizer{}, &fakeLocker{lock: &fakeLock{}}, executor)
	entry, err := service.Execute(context.Background(), auth.Principal{UserID: "user"}, "database", "migration")
	if err != nil || entry.Status != StatusInterrupted {
		t.Fatalf("Execute() = %#v, %v", entry, err)
	}
}

func TestViewerHistorySanitizesErrorDetails(t *testing.T) {
	t.Parallel()
	raw := "syntax error near secret_table"
	repository := &fakeRepository{page: Page{Items: []Entry{{ErrorMessage: &raw}}}}
	authorizer := &fakeAuthorizer{editErr: authz.ErrDatabaseEditForbidden}
	service := newTestService(t, repository, authorizer, &fakeLocker{}, &fakeExecutor{})
	page, err := service.List(context.Background(), auth.Principal{UserID: "user"}, "database", ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Items[0].ErrorMessage == nil || *page.Items[0].ErrorMessage == raw {
		t.Fatalf("error was not sanitized: %#v", page.Items[0].ErrorMessage)
	}
}

func TestDraftValidationRejectsEmptyAndTransactionControl(t *testing.T) {
	t.Parallel()
	service := newTestService(t, &fakeRepository{}, &fakeAuthorizer{}, &fakeLocker{}, &fakeExecutor{})
	principal := auth.Principal{UserID: "user"}
	if _, err := service.CreateDraft(context.Background(), principal, "database", "name", "   "); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty SQL error = %v", err)
	}
	if _, err := service.CreateDraft(context.Background(), principal, "database", "name", "BEGIN; SELECT 1"); !errors.Is(err, ErrSQLPolicy) {
		t.Fatalf("transaction SQL error = %v", err)
	}
}

func TestRecoverRunningUsesSafeReasonAndPropagatesFailure(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	repository := &fakeRepository{recovered: 2}
	if err := recoverRunning(context.Background(), repository, logger, time.Now()); err != nil {
		t.Fatalf("recoverRunning() error = %v", err)
	}
	if repository.recoveryReason != recoveryReason {
		t.Fatalf("recovery reason = %q", repository.recoveryReason)
	}

	repository.recoverErr = errors.New("platform unavailable")
	if err := recoverRunning(context.Background(), repository, logger, time.Now()); err == nil {
		t.Fatal("recoverRunning() must fail when repository recovery fails")
	}
}
