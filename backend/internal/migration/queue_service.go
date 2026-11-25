package migration

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/observability"
	shareduuid "github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/shared/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type QueueService struct {
	repository JobRepository
	migrations Repository
	attempts   AttemptRepository
	authorizer Authorizer
	approvals  ApprovalGate
	clock      Clock
	logger     *slog.Logger
}

func NewQueueService(repository JobRepository, migrations Repository, authorizer Authorizer,
	approvals ApprovalGate, clock Clock, logger *slog.Logger) (*QueueService, error) {
	if repository == nil || migrations == nil || authorizer == nil || approvals == nil {
		return nil, errors.New("migration queue dependencies are required")
	}
	attempts, ok := migrations.(AttemptRepository)
	if !ok {
		return nil, errors.New("migration queue requires execution history")
	}
	if clock == nil {
		clock = RealClock{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &QueueService{repository: repository, migrations: migrations, attempts: attempts,
		authorizer: authorizer, approvals: approvals, clock: clock, logger: logger}, nil
}

func (s *QueueService) EnqueueAction(ctx context.Context, principal auth.Principal, databaseID, migrationID string,
	action ExecutionAction, metadata EnqueueMetadata) (result Job, resultErr error) {
	ctx, span := otel.Tracer("dbmp/migration-queue").Start(ctx, "migration.job.enqueue",
		trace.WithAttributes(attribute.String("db.namespace", databaseID), attribute.String("db.migration.id", migrationID),
			attribute.String("db.migration.action", string(action))))
	defer func() {
		if resultErr != nil {
			span.RecordError(errors.New("migration job enqueue failed"))
			span.SetStatus(codes.Error, "migration job enqueue failed")
		} else {
			span.SetAttributes(attribute.String("db.job.id", result.ID))
		}
		span.End()
	}()
	metadata.Traceparent, metadata.Tracestate = observability.StoredTraceContext(ctx)
	if !shareduuid.Valid(databaseID) || !shareduuid.Valid(migrationID) || !validAction(action) {
		return Job{}, ErrInvalidInput
	}
	metadata.IdempotencyKey = strings.TrimSpace(metadata.IdempotencyKey)
	if len(metadata.IdempotencyKey) > 128 || strings.ContainsAny(metadata.IdempotencyKey, "\r\n\x00") {
		return Job{}, ErrInvalidInput
	}
	if err := s.authorizer.RequireDatabaseEdit(ctx, principal, databaseID); err != nil {
		return Job{}, err
	}
	entry, err := s.migrations.Get(ctx, databaseID, migrationID)
	if err != nil {
		return Job{}, err
	}
	approvalAction := approvalActionFor(action)
	if err := s.approvals.RequireApproved(ctx, databaseID, migrationID, approvalAction, entry.Revision); err != nil {
		return Job{}, err
	}
	if entry.ExecutionMode == "" {
		entry.ExecutionMode = ExecutionModeSingle
	}
	job, created, err := s.repository.Enqueue(ctx, enqueueJobInput{
		DatabaseID: databaseID, MigrationID: migrationID, RequestedRevision: entry.Revision,
		Action: action, ExecutionMode: entry.ExecutionMode, ActorID: principal.UserID,
		ActorUsername: principal.Username, Metadata: metadata, Now: s.clock.Now(),
	})
	if err != nil {
		return Job{}, err
	}
	if !created && (job.MigrationID != migrationID || job.Action != action || job.RequestedRevision != entry.Revision) {
		return Job{}, ErrConflict
	}
	if created {
		fields := []any{
			"event", "migration_job_enqueued", "actor", principal.Username,
			"database_id", databaseID, "migration_id", migrationID, "job_id", job.ID,
			"revision", job.RequestedRevision, "action", job.Action, "execution_mode", job.ExecutionMode,
			"correlation_id", metadata.CorrelationID,
		}
		fields = append(fields, observability.TraceFields(ctx)...)
		s.logger.InfoContext(ctx, "migration job enqueued", fields...)
	}
	return job, nil
}

func (s *QueueService) GetJob(ctx context.Context, principal auth.Principal, databaseID, jobID string) (Job, error) {
	if !shareduuid.Valid(databaseID) || !shareduuid.Valid(jobID) {
		return Job{}, ErrInvalidInput
	}
	if err := s.authorizer.RequireDatabaseView(ctx, principal, databaseID); err != nil {
		return Job{}, err
	}
	job, err := s.repository.Get(ctx, databaseID, jobID)
	if err != nil {
		return Job{}, err
	}
	if job.ExecutionID == nil || job.ExecutionMode != ExecutionModeBatch {
		return job, nil
	}
	attempt, err := s.attempts.GetHistory(ctx, databaseID, *job.ExecutionID)
	if err != nil {
		return Job{}, err
	}
	progress := BatchProgress{ExecutionID: attempt.ID, Status: attempt.Status}
	for _, event := range attempt.Events {
		if event.Event != EventBatchCommitted && event.Event != EventBatchReverted {
			continue
		}
		progress.CommittedBatches++
		if event.RowsAffected != nil {
			progress.CommittedRows += *event.RowsAffected
		}
		if event.CursorEnd != nil {
			progress.LastCursor = *event.CursorEnd
		}
	}
	job.BatchProgress = &progress
	return job, nil
}

func validAction(action ExecutionAction) bool {
	return action == ActionApply || action == ActionRetry || action == ActionRollback || action == ActionForward
}

func approvalActionFor(action ExecutionAction) string {
	if action == ActionRetry {
		return string(ActionApply)
	}
	return string(action)
}
