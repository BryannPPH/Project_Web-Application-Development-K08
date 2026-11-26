package migration

import (
	"context"
	"time"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
)

type Authorizer interface {
	RequireDatabaseView(context.Context, auth.Principal, string) error
	RequireDatabaseEdit(context.Context, auth.Principal, string) error
}

type Repository interface {
	CreateDraft(context.Context, string, string, string, string, time.Time) (Entry, error)
	UpdateDraft(context.Context, string, string, string, string, time.Time) (Entry, error)
	List(context.Context, string, ListOptions) (Page, error)
	Get(context.Context, string, string) (Entry, error)
	// MarkRunning gagal (ErrRevisionChanged) kalau revisi sudah berbeda dari expectedRevision.
	MarkRunning(ctx context.Context, databaseID, migrationID, executorID string, expectedRevision int64, now time.Time) (Entry, error)
	MarkTerminal(context.Context, string, string, Status, *string, time.Time) (Entry, error)
	Reconcile(context.Context, string, string, string, string, time.Time) (Entry, error)
	RecoverRunning(context.Context, time.Time, string) (int64, error)
}

// DefinitionRepository persists the complete immutable migration definition.
// The older Repository methods remain for Milestone 1 clients and tests.
type DefinitionRepository interface {
	CreateDraftDefinition(context.Context, string, Definition, string, time.Time) (Entry, error)
	UpdateDraftDefinition(context.Context, string, string, Definition, time.Time) (Entry, error)
}

type ExecutionPlan struct {
	Entry              Entry
	Action             ExecutionAction
	ExecutionID        string
	JobID              string
	JobLeaseToken      string
	JobWorkerID        string
	LockWait           time.Duration
	ActorUsername      string
	SQLSnapshot        string
	RelatedExecutionID *string
	CheckpointRootID   string
}

// AttemptRepository is the append-only execution ledger. State transitions are
// added as events; attempt identity, actor, revision and SQL snapshot never change.
type AttemptRepository interface {
	PlanExecution(context.Context, string, string, ExecutionAction, string) (ExecutionPlan, error)
	StartExecution(context.Context, ExecutionPlan, string, string, time.Time) (ExecutionAttempt, error)
	AppendExecutionEvent(context.Context, string, string, ExecutionEventType, *string, *int64, *int64, *int64, *int64, time.Time) error
	RecordBatchEvent(context.Context, string, BatchCommitEvent, time.Time) error
	FinishExecution(context.Context, ExecutionAttempt, Status, *string, time.Time) (Entry, error)
	LatestAttempt(context.Context, string) (ExecutionAttempt, error)
	ListHistory(context.Context, string, HistoryOptions) (HistoryPage, error)
	GetHistory(context.Context, string, string) (ExecutionAttempt, error)
}

type BatchExecutor interface {
	ValidateBatch(context.Context, string, Entry) error
	ExecuteBatch(context.Context, string, string, ExecutionPlan, func(BatchCommitEvent) error) ExecutionResult
	BatchProgress(context.Context, string, ExecutionAttempt) (BatchProgress, error)
}

// BatchCheckpointInspector is optional so executors without durable batch checkpoints
// keep the conservative needs-reconciliation recovery path.
type BatchCheckpointInspector interface {
	InspectBatchCheckpoints(context.Context, string, ExecutionAttempt) (BatchCheckpointState, error)
}

type BatchFailureRecorder interface {
	RecordBatchFailure(context.Context, string, string, BatchFailureEvent, *string, time.Time) error
}

// ApprovalGate memastikan semua leader group ber-akses sudah menyetujui revisi
// yang akan dijalankan. Implementasi: approval.Service.RequireApproved.
type ApprovalGate interface {
	RequireApproved(ctx context.Context, databaseID, migrationID, action string, revision int64) error
}

type ExecutionLock interface {
	Release(context.Context) error
}

type Locker interface {
	Acquire(context.Context, string) (ExecutionLock, error)
}

type ExecutionResult struct {
	Status        Status
	PublicError   *string
	InternalError error
	BatchFailure  *BatchFailureEvent
}

type Executor interface {
	Validate(context.Context, string, string) error
	Execute(context.Context, string, string) ExecutionResult
}

type Clock interface {
	Now() time.Time
}

type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now().UTC() }
