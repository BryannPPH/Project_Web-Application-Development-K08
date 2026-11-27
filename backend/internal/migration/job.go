package migration

import (
	"context"
	"time"
)

type JobStatus string

const (
	JobQueued              JobStatus = "queued"
	JobRunning             JobStatus = "running"
	JobRetrying            JobStatus = "retrying"
	JobSucceeded           JobStatus = "succeeded"
	JobFailed              JobStatus = "failed"
	JobPartial             JobStatus = "partial"
	JobPartiallyReverted   JobStatus = "partially_reverted"
	JobNeedsReconciliation JobStatus = "needs_reconciliation"
)

func (s JobStatus) Terminal() bool {
	return s == JobSucceeded || s == JobFailed || s == JobPartial ||
		s == JobPartiallyReverted || s == JobNeedsReconciliation
}

type Job struct {
	ID                  string          `json:"jobId"`
	DatabaseID          string          `json:"databaseId"`
	MigrationID         string          `json:"migrationId"`
	RequestedRevision   int64           `json:"revision"`
	Action              ExecutionAction `json:"action"`
	ExecutionMode       ExecutionMode   `json:"executionMode"`
	ActorID             string          `json:"actorId"`
	ActorUsername       string          `json:"actor"`
	Status              JobStatus       `json:"status"`
	RetryCount          int             `json:"retryCount"`
	ExecutionID         *string         `json:"executionId,omitempty"`
	PreviousExecutionID *string         `json:"previousExecutionId,omitempty"`
	CorrelationID       *string         `json:"correlationId,omitempty"`
	CreatedAt           time.Time       `json:"createdAt"`
	StartedAt           *time.Time      `json:"startedAt,omitempty"`
	FinishedAt          *time.Time      `json:"finishedAt,omitempty"`
	NextAttemptAt       time.Time       `json:"nextAttemptAt"`
	ErrorCode           *string         `json:"errorCode,omitempty"`
	ErrorMessage        *string         `json:"errorMessage,omitempty"`
	BatchProgress       *BatchProgress  `json:"batchProgress,omitempty"`

	leaseToken            *string
	leaseExpiresAt        *time.Time
	workerID              *string
	traceparent           *string
	tracestate            *string
	transactionRetryDelay time.Duration
}

type EnqueueMetadata struct {
	IdempotencyKey string
	CorrelationID  string
	Traceparent    string
	Tracestate     string
}

type queuedExecutionKey struct{}

func withQueuedExecution(ctx context.Context, job Job) context.Context {
	return context.WithValue(ctx, queuedExecutionKey{}, job)
}

func queuedExecutionFromContext(ctx context.Context) (Job, bool) {
	job, ok := ctx.Value(queuedExecutionKey{}).(Job)
	return job, ok
}

type enqueueJobInput struct {
	DatabaseID        string
	MigrationID       string
	RequestedRevision int64
	Action            ExecutionAction
	ExecutionMode     ExecutionMode
	ActorID           string
	ActorUsername     string
	Metadata          EnqueueMetadata
	Now               time.Time
}

type JobRepository interface {
	Enqueue(context.Context, enqueueJobInput) (Job, bool, error)
	Get(context.Context, string, string) (Job, error)
	LatestJobID(context.Context, string, string) (*string, error)
	ClaimNext(context.Context, string, string, time.Duration, time.Time) (Job, bool, error)
	Heartbeat(context.Context, Job, time.Time, time.Duration) error
	Finish(context.Context, Job, JobStatus, *string, *string, time.Time) error
	ScheduleRetry(context.Context, Job, time.Time, string, string, time.Time) error
	ListExpired(context.Context, time.Time, int) ([]Job, error)
	RequeueExpiredWithoutExecution(context.Context, Job, time.Time) (bool, error)
}
