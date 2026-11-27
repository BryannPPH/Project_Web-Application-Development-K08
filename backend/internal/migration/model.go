package migration

import "time"

type Status string

const (
	StatusDraft               Status = "draft"
	StatusRunning             Status = "running"
	StatusSucceeded           Status = "succeeded"
	StatusFailed              Status = "failed"
	StatusInterrupted         Status = "interrupted"
	StatusNeedsReconciliation Status = "needs_reconciliation"
	StatusReconciled          Status = "reconciled"
	StatusRolledBack          Status = "rolled_back"
	StatusPartial             Status = "partial"
	StatusPartiallyReverted   Status = "partially_reverted"
)

func (s Status) Terminal() bool {
	return s == StatusSucceeded || s == StatusFailed || s == StatusInterrupted || s == StatusNeedsReconciliation || s == StatusReconciled || s == StatusRolledBack || s == StatusPartial || s == StatusPartiallyReverted
}

type ExecutionMode string

const (
	ExecutionModeSingle ExecutionMode = "single"
	ExecutionModeBatch  ExecutionMode = "batch"
)

type ExecutionAction string

const (
	ActionApply    ExecutionAction = "apply"
	ActionRetry    ExecutionAction = "retry"
	ActionRollback ExecutionAction = "rollback"
	ActionForward  ExecutionAction = "forward"
)

type ExecutionEventType string

const (
	EventQueued              ExecutionEventType = "queued"
	EventStarted             ExecutionEventType = "started"
	EventBatchCommitted      ExecutionEventType = "batch_committed"
	EventBatchReverted       ExecutionEventType = "batch_reverted"
	EventBatchFailed         ExecutionEventType = "batch_failed"
	EventCompleted           ExecutionEventType = "completed"
	EventFailed              ExecutionEventType = "failed"
	EventInterrupted         ExecutionEventType = "interrupted"
	EventNeedsReconciliation ExecutionEventType = "needs_reconciliation"
	EventPartial             ExecutionEventType = "partial"
	EventPartiallyReverted   ExecutionEventType = "partially_reverted"
)

type Entry struct {
	ID                   string          `json:"id"`
	DatabaseID           string          `json:"databaseId"`
	Version              int64           `json:"version"`
	Revision             int64           `json:"revision"`
	Name                 string          `json:"name"`
	SQL                  string          `json:"sql"`
	DownSQL              *string         `json:"downSql"`
	IrreversibleReason   *string         `json:"irreversibleReason"`
	IsActive             bool            `json:"isActive"`
	LatestExecutionID    *string         `json:"latestExecutionId"`
	LatestJobID          *string         `json:"latestJobId,omitempty"`
	PartialAction        ExecutionAction `json:"partialAction,omitempty"`
	ExecutionMode        ExecutionMode   `json:"executionMode"`
	BatchSelectSQL       *string         `json:"batchSelectSql"`
	BatchSize            *int            `json:"batchSize"`
	BatchCursorStart     int64           `json:"batchCursorStart"`
	Status               Status          `json:"status"`
	CreatedBy            string          `json:"createdBy"`
	CreatedByUsername    string          `json:"createdByUsername"`
	ExecutedBy           *string         `json:"executedBy"`
	ExecutedByUsername   *string         `json:"executedByUsername"`
	CreatedAt            time.Time       `json:"createdAt"`
	StartedAt            *time.Time      `json:"startedAt"`
	FinishedAt           *time.Time      `json:"finishedAt"`
	ErrorMessage         *string         `json:"errorMessage"`
	ReconciledFrom       *Status         `json:"reconciledFrom"`
	ReconciledBy         *string         `json:"reconciledBy"`
	ReconciledByUsername *string         `json:"reconciledByUsername"`
	ReconciledAt         *time.Time      `json:"reconciledAt"`
	ReconciliationNote   *string         `json:"reconciliationNote"`
	CanEdit              bool            `json:"canEdit"`
	CanReconcile         bool            `json:"canReconcile"`

	attemptStatus Status
	attemptError  *string
}

// Definition is immutable after the first execution attempt has been recorded.
// In batch mode SQL and DownSQL each accept two PostgreSQL parameters: the
// exclusive lower cursor and inclusive upper cursor for that batch.
type Definition struct {
	Name               string
	SQL                string
	DownSQL            *string
	IrreversibleReason *string
	ExecutionMode      ExecutionMode
	BatchSelectSQL     *string
	BatchSize          *int
	BatchCursorStart   int64
}

type ExecutionAttempt struct {
	ID                 string           `json:"executionId,omitempty"`
	JobID              *string          `json:"jobId,omitempty"`
	ErrorCode          *string          `json:"errorCode,omitempty"`
	DatabaseID         string           `json:"databaseId"`
	MigrationID        string           `json:"migrationId"`
	MigrationName      string           `json:"migrationName"`
	Version            int64            `json:"version"`
	Revision           int64            `json:"revision"`
	Action             ExecutionAction  `json:"action"`
	ActorID            string           `json:"actorId"`
	ActorUsername      string           `json:"actorUsername"`
	RequestedAt        time.Time        `json:"requestedAt"`
	SQLSnapshot        string           `json:"sqlSnapshot"`
	RelatedExecutionID *string          `json:"relatedExecutionId"`
	CheckpointRootID   string           `json:"checkpointRootId"`
	Status             string           `json:"status"`
	ErrorMessage       *string          `json:"errorMessage"`
	Events             []ExecutionEvent `json:"events,omitempty"`

	previousMigrationStatus Status
}

type ExecutionEvent struct {
	ID              int64              `json:"id"`
	Status          string             `json:"status"`
	Event           ExecutionEventType `json:"event"`
	OccurredAt      time.Time          `json:"occurredAt"`
	ErrorMessage    *string            `json:"errorMessage"`
	QuerySQL        string             `json:"querySql,omitempty"`
	QueryParameters []int64            `json:"queryParameters,omitempty"`
	RowsAffected    *int64             `json:"rowsAffected"`
	BatchNumber     *int64             `json:"batchNumber"`
	CursorStart     *int64             `json:"cursorStart"`
	CursorEnd       *int64             `json:"cursorEnd"`
}

type BatchCommitEvent struct {
	Operation    string
	BatchNumber  int64
	CursorStart  int64
	CursorEnd    int64
	RowsAffected int64
	QuerySQL     string
}

type BatchFailureEvent struct {
	QuerySQL        string
	QueryParameters []int64
	BatchNumber     *int64
	CursorStart     *int64
	CursorEnd       *int64
}

type HistoryOptions struct {
	Limit        int
	Offset       int
	ExecutionID  string
	HideRollback bool
}

type HistoryPage struct {
	Items  []ExecutionAttempt `json:"items"`
	Limit  int                `json:"limit"`
	Offset int                `json:"offset"`
	Total  int64              `json:"total"`
}

type BatchProgress struct {
	ExecutionID      string `json:"executionId"`
	CommittedBatches int64  `json:"committedBatches"`
	CommittedRows    int64  `json:"committedRows"`
	LastCursor       int64  `json:"lastCursor"`
	Status           string `json:"status"`
}

// BatchCheckpointState describes target-side commits used to recover an expired worker lease.
type BatchCheckpointState struct {
	UpBatches   int64
	DownBatches int64
}

type ListOptions struct {
	Limit  int
	Offset int
}

type Page struct {
	Items   []Entry `json:"items"`
	Limit   int     `json:"limit"`
	Offset  int     `json:"offset"`
	Total   int64   `json:"total"`
	CanEdit bool    `json:"canEdit"`
}
