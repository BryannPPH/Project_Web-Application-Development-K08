package migration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	rand "math/rand/v2"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/observability"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/requestmeta"
	shareduuid "github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/shared/uuid"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type WorkerConfig struct {
	Concurrency         int
	PollInterval        time.Duration
	LeaseDuration       time.Duration
	HeartbeatInterval   time.Duration
	RecoveryLockTimeout time.Duration
	MaxRetries          int
	RetryBaseDelay      time.Duration
	RetryMaxDelay       time.Duration
}

func DefaultWorkerConfig() WorkerConfig {
	return WorkerConfig{
		Concurrency: 4, PollInterval: time.Second, LeaseDuration: 45 * time.Second,
		HeartbeatInterval: 10 * time.Second, RecoveryLockTimeout: 300 * time.Millisecond,
		MaxRetries: 5, RetryBaseDelay: 2 * time.Second, RetryMaxDelay: time.Minute,
	}
}

func (c WorkerConfig) Validate() error {
	if c.Concurrency < 1 || c.Concurrency > 64 || c.PollInterval < 50*time.Millisecond ||
		c.LeaseDuration < 3*c.HeartbeatInterval || c.HeartbeatInterval <= 0 ||
		c.RecoveryLockTimeout <= 0 || c.MaxRetries < 0 || c.MaxRetries > 20 ||
		c.RetryBaseDelay <= 0 || c.RetryMaxDelay < c.RetryBaseDelay {
		return errors.New("invalid migration worker configuration")
	}
	return nil
}

type Worker struct {
	jobs     JobRepository
	service  *Service
	attempts AttemptRepository
	config   WorkerConfig
	logger   *slog.Logger
	clock    Clock
}

func NewWorker(jobs JobRepository, service *Service, config WorkerConfig, logger *slog.Logger) (*Worker, error) {
	if jobs == nil || service == nil {
		return nil, errors.New("migration worker dependencies are required")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	attempts, ok := service.repository.(AttemptRepository)
	if !ok {
		return nil, errors.New("migration worker requires execution history")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{jobs: jobs, service: service, attempts: attempts, config: config, logger: logger, clock: RealClock{}}, nil
}

func (w *Worker) Run(ctx context.Context) error {
	workerID, err := shareduuid.New()
	if err != nil {
		return err
	}
	w.logger.InfoContext(ctx, "migration worker started", "event", "migration_worker_started",
		"worker_id", workerID, "concurrency", w.config.Concurrency)
	var wait sync.WaitGroup
	for index := 0; index < w.config.Concurrency; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			w.runLoop(ctx, fmt.Sprintf("%s-%02d", workerID, index+1))
		}(index)
	}
	<-ctx.Done()
	wait.Wait()
	w.logger.Info("migration worker stopped", "event", "migration_worker_stopped", "worker_id", workerID)
	return nil
}

func (w *Worker) runLoop(ctx context.Context, workerID string) {
	for ctx.Err() == nil {
		if err := w.recoverExpired(ctx); err != nil && ctx.Err() == nil {
			w.logger.ErrorContext(ctx, "recover expired migration jobs", "event", "migration_job_recovery_failed",
				"worker_id", workerID, "error", err.Error())
		}
		leaseToken, err := shareduuid.New()
		if err != nil {
			w.wait(ctx)
			continue
		}
		job, claimed, err := w.jobs.ClaimNext(ctx, workerID, leaseToken, w.config.LeaseDuration, w.clock.Now())
		if err != nil {
			if ctx.Err() == nil {
				w.logger.ErrorContext(ctx, "claim migration job", "event", "migration_job_claim_failed",
					"worker_id", workerID, "error", err.Error())
			}
			w.wait(ctx)
			continue
		}
		if !claimed {
			w.wait(ctx)
			continue
		}
		w.process(ctx, workerID, job)
	}
}

func (w *Worker) process(ctx context.Context, workerID string, job Job) {
	parent := observability.ContextFromStored(ctx, valueOrEmpty(job.traceparent), valueOrEmpty(job.tracestate))
	jobCtx, span := otel.Tracer("dbmp/migration-worker").Start(parent, "migration.job.process",
		trace.WithAttributes(attribute.String("db.job.id", job.ID), attribute.String("db.namespace", job.DatabaseID),
			attribute.String("db.migration.id", job.MigrationID), attribute.String("db.migration.action", string(job.Action))))
	defer span.End()
	if job.CorrelationID != nil {
		jobCtx = requestmeta.WithRequestID(jobCtx, *job.CorrelationID)
	}
	principal := auth.Principal{UserID: job.ActorID, Username: job.ActorUsername, PlatformRole: auth.PlatformRoleUser}
	jobCtx = auth.WithPrincipal(jobCtx, principal)
	jobCtx = targetdb.WithActivity(jobCtx, targetdb.Activity{
		JobID: job.ID, MigrationID: job.MigrationID, Action: string(job.Action),
		Attempt: job.RetryCount + 1, CorrelationID: valueOrEmpty(job.CorrelationID),
	})
	w.logger.InfoContext(jobCtx, "migration job processing started", append([]any{
		"event", "migration_job_started", "actor", job.ActorUsername, "actor_id", job.ActorID,
		"worker_id", workerID,
		"database_id", job.DatabaseID, "migration_id", job.MigrationID, "job_id", job.ID,
		"revision", job.RequestedRevision, "action", job.Action, "execution_mode", job.ExecutionMode,
		"retry_count", job.RetryCount,
	}, observability.TraceFields(jobCtx)...)...)
	if job.ExecutionID == nil && job.RetryCount > w.config.MaxRetries {
		code, message := "WORKER_RESTART_LIMIT", "worker restarted too many times before execution began"
		finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer finishCancel()
		if err := w.jobs.Finish(finishCtx, job, JobFailed, &code, &message, w.clock.Now()); err != nil {
			w.logger.ErrorContext(finishCtx, "record worker restart limit", "event", "migration_job_failure_record_failed",
				"job_id", job.ID, "error", err.Error())
			return
		}
		w.logger.ErrorContext(jobCtx, "migration job exceeded restart limit", append([]any{
			"event", "migration_job_failed", "job_id", job.ID, "error_code", code,
		}, observability.TraceFields(jobCtx)...)...)
		return
	}

	workCtx, cancel := context.WithCancel(jobCtx)
	heartbeatDone := make(chan error, 1)
	go w.heartbeat(workCtx, job, cancel, heartbeatDone)
	executionCtx := withQueuedExecution(workCtx, job)
	entry, executionErr := w.service.ExecuteAction(executionCtx, principal, job.DatabaseID, job.MigrationID, job.Action)
	cancel()
	heartbeatErr := <-heartbeatDone

	if errors.Is(heartbeatErr, ErrLeaseLost) {
		w.logger.WarnContext(jobCtx, "migration job lease lost", append([]any{
			"event", "migration_job_lease_lost", "job_id", job.ID,
		}, observability.TraceFields(jobCtx)...)...)
		return
	}
	if ctx.Err() != nil && executionErr != nil {
		return
	}
	if executionErr != nil {
		span.RecordError(errors.New("migration job execution failed"))
		span.SetStatus(codes.Error, "migration job execution failed")
		w.handleExecutionError(ctx, jobCtx, job, executionErr)
		return
	}
	status := jobStatusForMigration(entry.Status)
	var code *string
	message := entry.ErrorMessage
	if message != nil {
		errorCode, _ := publicJobError(errors.New(*message))
		code = &errorCode
	}
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer finishCancel()
	if err := w.jobs.Finish(finishCtx, job, status, code, message, w.clock.Now()); err != nil {
		w.logger.ErrorContext(finishCtx, "finish migration job", "event", "migration_job_finalize_failed",
			"job_id", job.ID, "execution_id", entry.LatestExecutionID, "error", err.Error())
		span.RecordError(errors.New("migration job finalization failed"))
		span.SetStatus(codes.Error, "job finalization failed")
		return
	}
	w.logger.InfoContext(jobCtx, "migration job finished", append([]any{
		"event", "migration_job_finished", "job_id", job.ID, "execution_id", entry.LatestExecutionID,
		"status", status, "retry_count", job.RetryCount, "worker_id", workerID,
	}, observability.TraceFields(jobCtx)...)...)
}

func (w *Worker) heartbeat(ctx context.Context, job Job, cancel context.CancelFunc, done chan<- error) {
	ticker := time.NewTicker(w.config.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			done <- nil
			return
		case <-ticker.C:
			heartbeatCtx, heartbeatCancel := context.WithTimeout(context.WithoutCancel(ctx), w.config.HeartbeatInterval)
			err := w.jobs.Heartbeat(heartbeatCtx, job, w.clock.Now(), w.config.LeaseDuration)
			heartbeatCancel()
			if err != nil {
				cancel()
				done <- err
				return
			}
		}
	}
}

func (w *Worker) handleExecutionError(ctx, logCtx context.Context, job Job, executeErr error) {
	operationCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	current, err := w.jobs.Get(operationCtx, job.DatabaseID, job.ID)
	if err != nil {
		w.logger.ErrorContext(logCtx, "reload migration job after execution error", "event", "migration_job_reload_failed",
			"job_id", job.ID, "error", err.Error())
		return
	}
	if current.ExecutionID != nil {
		w.markUncertain(operationCtx, logCtx, current, "execution returned an error after an attempt was recorded; inspect target state before retry")
		return
	}
	errorCode, publicMessage := publicJobError(executeErr)
	if isRetryableQueueError(executeErr) && current.RetryCount < w.config.MaxRetries {
		next := w.clock.Now().Add(w.retryDelay(current.RetryCount))
		if err := w.jobs.ScheduleRetry(operationCtx, job, next, errorCode, publicMessage, w.clock.Now()); err != nil {
			w.logger.ErrorContext(logCtx, "schedule migration job retry", "event", "migration_job_retry_schedule_failed",
				"job_id", job.ID, "error", err.Error())
			return
		}
		w.logger.WarnContext(logCtx, "migration job scheduled for retry", append([]any{
			"event", "migration_job_retry_scheduled", "job_id", job.ID, "retry_count", current.RetryCount + 1,
			"next_attempt_at", next, "error_code", errorCode,
		}, observability.TraceFields(logCtx)...)...)
		return
	}
	if isRetryableQueueError(executeErr) && current.RetryCount >= w.config.MaxRetries {
		errorCode = "RETRY_LIMIT_REACHED"
		publicMessage = "temporary execution failure exceeded the retry limit"
	}
	code, message := errorCode, publicMessage
	if err := w.jobs.Finish(operationCtx, job, JobFailed, &code, &message, w.clock.Now()); err != nil {
		w.logger.ErrorContext(logCtx, "record failed migration job", "event", "migration_job_failure_record_failed",
			"job_id", job.ID, "error", err.Error())
		return
	}
	w.logger.WarnContext(logCtx, "migration job failed before execution", append([]any{
		"event", "migration_job_failed", "job_id", job.ID, "error_code", errorCode,
		"error", publicMessage,
	}, observability.TraceFields(logCtx)...)...)
}

func (w *Worker) markUncertain(ctx, logCtx context.Context, job Job, reason string) {
	lockCtx, cancel := context.WithTimeout(ctx, w.config.RecoveryLockTimeout)
	defer cancel()
	lock, err := w.service.locker.Acquire(lockCtx, job.DatabaseID)
	if err != nil {
		w.logger.WarnContext(logCtx, "could not inspect migration after uncertain execution", "event", "migration_job_reconciliation_deferred",
			"job_id", job.ID, "error", err.Error())
		return
	}
	defer func() {
		releaseCtx, releaseCancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer releaseCancel()
		_ = lock.Release(releaseCtx)
	}()
	if job.ExecutionID != nil {
		attempt, err := w.attempts.GetHistory(ctx, job.DatabaseID, *job.ExecutionID)
		if err != nil {
			w.logger.ErrorContext(logCtx, "inspect uncertain migration attempt", "event", "migration_attempt_inspection_failed",
				"job_id", job.ID, "execution_id", *job.ExecutionID, "error", err.Error())
			return
		}
		if attempt.Status == string(StatusRunning) {
			message := reason
			if _, err := w.attempts.FinishExecution(ctx, attempt, StatusNeedsReconciliation, &message, w.clock.Now()); err != nil {
				w.logger.ErrorContext(logCtx, "mark uncertain migration attempt", "event", "migration_attempt_reconciliation_failed",
					"job_id", job.ID, "execution_id", attempt.ID, "error", err.Error())
				return
			}
		}
		if attempt.Status != string(StatusRunning) {
			status := jobStatusForExecutionStatus(attempt.Status)
			if status.Terminal() {
				if err := w.jobs.Finish(ctx, job, status, nil, attempt.ErrorMessage, w.clock.Now()); err != nil {
					w.logger.ErrorContext(logCtx, "recover terminal migration attempt", "event", "migration_job_recovery_failed",
						"job_id", job.ID, "execution_id", attempt.ID, "error", err.Error())
				}
				return
			}
		}
	}
	errorCode, message := "MIGRATION_OUTCOME_UNCERTAIN", reason
	if err := w.jobs.Finish(ctx, job, JobNeedsReconciliation, &errorCode, &message, w.clock.Now()); err != nil {
		w.logger.ErrorContext(logCtx, "mark migration job for reconciliation", "event", "migration_job_reconciliation_failed",
			"job_id", job.ID, "error", err.Error())
		return
	}
	w.logger.ErrorContext(logCtx, "migration job needs reconciliation", append([]any{
		"event", "migration_job_needs_reconciliation", "job_id", job.ID,
		"execution_id", valueOrEmpty(job.ExecutionID), "error", message,
	}, observability.TraceFields(logCtx)...)...)
}

func (w *Worker) recoverExpired(ctx context.Context) error {
	jobs, err := w.jobs.ListExpired(ctx, w.clock.Now(), w.config.Concurrency*4)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if job.ExecutionID == nil {
			requeued, err := w.jobs.RequeueExpiredWithoutExecution(ctx, job, w.clock.Now())
			if err != nil {
				return err
			}
			if requeued {
				w.logger.WarnContext(ctx, "requeued job after worker lease expired before execution",
					"event", "migration_job_requeued_after_restart", "job_id", job.ID,
					"retry_count", job.RetryCount+1)
			}
			continue
		}
		w.recoverExpiredExecution(ctx, job)
	}
	return nil
}

func (w *Worker) recoverExpiredExecution(ctx context.Context, expired Job) {
	lockCtx, cancel := context.WithTimeout(ctx, w.config.RecoveryLockTimeout)
	defer cancel()
	lock, err := w.service.locker.Acquire(lockCtx, expired.DatabaseID)
	if err != nil {
		return
	}
	defer func() {
		releaseCtx, releaseCancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer releaseCancel()
		_ = lock.Release(releaseCtx)
	}()
	current, err := w.jobs.Get(ctx, expired.DatabaseID, expired.ID)
	if err != nil || current.Status != JobRunning || current.leaseToken == nil || expired.leaseToken == nil ||
		*current.leaseToken != *expired.leaseToken || current.leaseExpiresAt == nil || current.leaseExpiresAt.After(w.clock.Now()) {
		return
	}
	attempt, err := w.attempts.GetHistory(ctx, expired.DatabaseID, *current.ExecutionID)
	if err != nil {
		w.logger.ErrorContext(ctx, "load expired migration execution", "event", "migration_job_recovery_failed",
			"job_id", current.ID, "execution_id", valueOrEmpty(current.ExecutionID), "error", err.Error())
		return
	}
	status := jobStatusForExecutionStatus(attempt.Status)
	if attempt.Status == string(StatusRunning) {
		message := "worker lease expired while the target outcome was unknown; inspect the target and reconcile before retry"
		if _, err := w.attempts.FinishExecution(ctx, attempt, StatusNeedsReconciliation, &message, w.clock.Now()); err != nil {
			w.logger.ErrorContext(ctx, "mark expired execution for reconciliation", "event", "migration_job_recovery_failed",
				"job_id", current.ID, "execution_id", attempt.ID, "error", err.Error())
			return
		}
		status = JobNeedsReconciliation
	}
	if !status.Terminal() {
		status = JobNeedsReconciliation
	}
	var code *string
	message := attempt.ErrorMessage
	if status == JobNeedsReconciliation {
		value := "MIGRATION_OUTCOME_UNCERTAIN"
		code = &value
		if message == nil {
			value := "worker stopped during execution; inspect the target before retry"
			message = &value
		}
	}
	if err := w.jobs.Finish(ctx, current, status, code, message, w.clock.Now()); err != nil {
		w.logger.ErrorContext(ctx, "finalize expired migration job", "event", "migration_job_recovery_failed",
			"job_id", current.ID, "execution_id", attempt.ID, "error", err.Error())
		return
	}
	w.logger.WarnContext(ctx, "recovered expired migration job", "event", "migration_job_recovered",
		"job_id", current.ID, "execution_id", attempt.ID, "status", status)
}

func (w *Worker) retryDelay(retryCount int) time.Duration {
	delay := w.config.RetryBaseDelay
	for index := 0; index < retryCount && delay < w.config.RetryMaxDelay/2; index++ {
		delay *= 2
	}
	if delay > w.config.RetryMaxDelay {
		delay = w.config.RetryMaxDelay
	}
	return delay + time.Duration(rand.Int64N(int64(delay/4)+1))
}

func (w *Worker) wait(ctx context.Context) {
	timer := time.NewTimer(w.config.PollInterval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func jobStatusForMigration(status Status) JobStatus {
	switch status {
	case StatusSucceeded, StatusRolledBack:
		return JobSucceeded
	case StatusPartial:
		return JobPartial
	case StatusPartiallyReverted:
		return JobPartiallyReverted
	case StatusInterrupted, StatusNeedsReconciliation:
		return JobNeedsReconciliation
	default:
		return JobFailed
	}
}

func jobStatusForExecutionStatus(status string) JobStatus {
	switch Status(status) {
	case StatusSucceeded:
		return JobSucceeded
	case StatusPartial:
		return JobPartial
	case StatusPartiallyReverted:
		return JobPartiallyReverted
	case StatusNeedsReconciliation, StatusInterrupted:
		return JobNeedsReconciliation
	case StatusFailed:
		return JobFailed
	default:
		return ""
	}
}

func isRetryableQueueError(err error) bool {
	if errors.Is(err, targetdb.ErrTargetUnavailable) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		return strings.HasPrefix(pgError.Code, "08") || pgError.Code == "53300" || pgError.Code == "40001"
	}
	var networkError net.Error
	return errors.As(err, &networkError) && (networkError.Timeout() || networkError.Temporary())
}

func publicJobError(err error) (string, string) {
	switch {
	case errors.Is(err, targetdb.ErrTargetUnavailable):
		return "TARGET_UNAVAILABLE", "target database is temporarily unavailable"
	case errors.Is(err, ErrRevisionChanged):
		return "MIGRATION_REVISION_CHANGED", "migration changed after this job was approved; approve and enqueue the current revision"
	case errors.Is(err, ErrWrongOrder):
		return "MIGRATION_ORDER_CONFLICT", "an earlier migration blocks this job"
	case errors.Is(err, ErrIrreversible):
		return "MIGRATION_IRREVERSIBLE", "this migration cannot be rolled back"
	case errors.Is(err, ErrSQLPolicy):
		return "SQL_POLICY_VIOLATION", "migration SQL violates execution policy"
	case errors.Is(err, ErrConflict):
		return "MIGRATION_CONFLICT", "migration state does not allow this action"
	case strings.Contains(strings.ToLower(err.Error()), "approval"):
		return "APPROVAL_REQUIRED", "required leader approval is missing or stale"
	case errors.Is(err, authz.ErrDatabaseEditForbidden):
		return "DATABASE_EDIT_FORBIDDEN", "database edit access is required"
	default:
		return "MIGRATION_JOB_FAILED", "migration job could not be executed"
	}
}
