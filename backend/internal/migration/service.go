package migration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/observability"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/requestmeta"
	shareduuid "github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/shared/uuid"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	DefaultMaxSQLBytes = 256 * 1024
	DefaultPageSize    = 50
	MaxPageSize        = 100
)

type Service struct {
	repository  Repository
	authorizer  Authorizer
	locker      Locker
	executor    Executor
	approvals   ApprovalGate
	clock       Clock
	logger      *slog.Logger
	maxSQLBytes int
	timeout     time.Duration
}

type ServiceConfig struct {
	MaxSQLBytes      int
	ExecutionTimeout time.Duration
}

func NewService(repository Repository, authorizer Authorizer, locker Locker, executor Executor, approvals ApprovalGate, clock Clock, logger *slog.Logger, config ServiceConfig) (*Service, error) {
	// Gerbang approval wajib: tanpa itu migration bisa jalan tanpa persetujuan leader.
	if repository == nil || authorizer == nil || locker == nil || executor == nil || approvals == nil {
		return nil, errors.New("migration service dependencies are required")
	}
	if clock == nil {
		clock = RealClock{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	if config.MaxSQLBytes == 0 {
		config.MaxSQLBytes = DefaultMaxSQLBytes
	}
	if config.ExecutionTimeout == 0 {
		config.ExecutionTimeout = 2 * time.Minute
	}
	if config.MaxSQLBytes < 1 || config.ExecutionTimeout < time.Second {
		return nil, errors.New("invalid migration service configuration")
	}
	return &Service{repository: repository, authorizer: authorizer, locker: locker, executor: executor, approvals: approvals,
		clock: clock, logger: logger, maxSQLBytes: config.MaxSQLBytes, timeout: config.ExecutionTimeout}, nil
}

func (s *Service) CreateDraft(ctx context.Context, principal auth.Principal, databaseID, name, sqlText string) (Entry, error) {
	reason := "No down SQL was supplied by this legacy Milestone 1 client."
	return s.CreateDraftWithDefinition(ctx, principal, databaseID, Definition{
		Name: name, SQL: sqlText, IrreversibleReason: &reason, ExecutionMode: ExecutionModeSingle,
	})
}

func (s *Service) CreateDraftWithDefinition(ctx context.Context, principal auth.Principal, databaseID string, definition Definition) (Entry, error) {
	if err := s.authorizer.RequireDatabaseEdit(ctx, principal, databaseID); err != nil {
		return Entry{}, err
	}
	definition, err := s.validateDefinition(definition, true)
	if err != nil {
		return Entry{}, err
	}
	var entry Entry
	if definitions, ok := s.repository.(DefinitionRepository); ok {
		entry, err = definitions.CreateDraftDefinition(ctx, databaseID, definition, principal.UserID, s.clock.Now())
	} else {
		entry, err = s.repository.CreateDraft(ctx, databaseID, definition.Name, definition.SQL, principal.UserID, s.clock.Now())
	}
	entry.CanEdit = err == nil
	if err == nil {
		entry.CreatedByUsername = principal.Username
	}
	return entry, err
}

func (s *Service) UpdateDraft(ctx context.Context, principal auth.Principal, databaseID, migrationID, name, sqlText string) (Entry, error) {
	if err := s.authorizer.RequireDatabaseEdit(ctx, principal, databaseID); err != nil {
		return Entry{}, err
	}
	name, sqlText, err := s.validateDraft(name, sqlText)
	if err != nil {
		return Entry{}, err
	}
	entry, err := s.repository.UpdateDraft(ctx, databaseID, migrationID, name, sqlText, s.clock.Now())
	entry.CanEdit = err == nil
	return entry, err
}

func (s *Service) UpdateDraftWithDefinition(ctx context.Context, principal auth.Principal, databaseID, migrationID string, definition Definition) (Entry, error) {
	if err := s.authorizer.RequireDatabaseEdit(ctx, principal, databaseID); err != nil {
		return Entry{}, err
	}
	definition, err := s.validateDefinition(definition, false)
	if err != nil {
		return Entry{}, err
	}
	definitions, ok := s.repository.(DefinitionRepository)
	if !ok {
		return s.repository.UpdateDraft(ctx, databaseID, migrationID, definition.Name, definition.SQL, s.clock.Now())
	}
	entry, err := definitions.UpdateDraftDefinition(ctx, databaseID, migrationID, definition, s.clock.Now())
	entry.CanEdit = err == nil
	return entry, err
}

func (s *Service) List(ctx context.Context, principal auth.Principal, databaseID string, options ListOptions) (Page, error) {
	if err := s.authorizer.RequireDatabaseView(ctx, principal, databaseID); err != nil {
		return Page{}, err
	}
	if options.Limit == 0 {
		options.Limit = DefaultPageSize
	}
	if options.Limit < 1 || options.Limit > MaxPageSize || options.Offset < 0 {
		return Page{}, ErrInvalidInput
	}
	page, err := s.repository.List(ctx, databaseID, options)
	if err != nil {
		return Page{}, err
	}
	if err := s.authorizer.RequireDatabaseEdit(ctx, principal, databaseID); err != nil {
		for index := range page.Items {
			page.Items[index].ErrorMessage = sanitizeForViewer(page.Items[index].ErrorMessage)
		}
	} else {
		page.CanEdit = true
		for index := range page.Items {
			page.Items[index].CanEdit = true
			page.Items[index].CanReconcile = needsReconciliation(page.Items[index].Status)
		}
	}
	return page, nil
}

func (s *Service) Get(ctx context.Context, principal auth.Principal, databaseID, migrationID string) (Entry, error) {
	if err := s.authorizer.RequireDatabaseView(ctx, principal, databaseID); err != nil {
		return Entry{}, err
	}
	entry, err := s.repository.Get(ctx, databaseID, migrationID)
	if err != nil {
		return Entry{}, err
	}
	if entry.Status == StatusPartial {
		if attempts, ok := s.repository.(AttemptRepository); ok {
			latest, err := attempts.LatestAttempt(ctx, entry.ID)
			if err != nil {
				return Entry{}, err
			}
			source, err := partialSourceAttempt(ctx, attempts, entry, latest)
			if err != nil {
				return Entry{}, err
			}
			entry.PartialAction = source.Action
		}
	}
	if err := s.authorizer.RequireDatabaseEdit(ctx, principal, databaseID); err != nil {
		entry.ErrorMessage = sanitizeForViewer(entry.ErrorMessage)
	} else {
		entry.CanEdit = true
		entry.CanReconcile = needsReconciliation(entry.Status)
	}
	return entry, nil
}

func (s *Service) Reconcile(ctx context.Context, principal auth.Principal, databaseID, migrationID, note string) (Entry, error) {
	if err := s.authorizer.RequireDatabaseEdit(ctx, principal, databaseID); err != nil {
		return Entry{}, err
	}
	note = strings.TrimSpace(note)
	if len([]rune(note)) < 10 || len([]rune(note)) > 1000 {
		return Entry{}, ErrInvalidInput
	}
	lock, err := s.locker.Acquire(ctx, databaseID)
	if err != nil {
		return Entry{}, fmt.Errorf("acquire reconciliation lock: %w", err)
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if releaseErr := lock.Release(releaseCtx); releaseErr != nil {
			s.logger.Error("release migration reconciliation lock", "database_id", databaseID, "error", releaseErr)
		}
	}()
	if err := s.authorizer.RequireDatabaseEdit(ctx, principal, databaseID); err != nil {
		return Entry{}, err
	}
	entry, err := s.repository.Get(ctx, databaseID, migrationID)
	if err != nil {
		return Entry{}, err
	}
	if !needsReconciliation(entry.Status) {
		return Entry{}, ErrConflict
	}
	creatorUsername, executorUsername := entry.CreatedByUsername, entry.ExecutedByUsername
	entry, err = s.repository.Reconcile(ctx, databaseID, migrationID, principal.UserID, note, s.clock.Now())
	if err != nil {
		return Entry{}, err
	}
	entry.CanEdit = true
	entry.CreatedByUsername = creatorUsername
	entry.ExecutedByUsername = executorUsername
	entry.ReconciledByUsername = &principal.Username
	return entry, nil
}

func needsReconciliation(status Status) bool {
	return status == StatusInterrupted || status == StatusNeedsReconciliation
}

func (s *Service) Execute(ctx context.Context, principal auth.Principal, databaseID, migrationID string) (Entry, error) {
	return s.ExecuteAction(ctx, principal, databaseID, migrationID, ActionApply)
}

func (s *Service) Retry(ctx context.Context, principal auth.Principal, databaseID, migrationID string) (Entry, error) {
	return s.ExecuteAction(ctx, principal, databaseID, migrationID, ActionRetry)
}

func (s *Service) Rollback(ctx context.Context, principal auth.Principal, databaseID, migrationID string) (Entry, error) {
	return s.ExecuteAction(ctx, principal, databaseID, migrationID, ActionRollback)
}

func (s *Service) Forward(ctx context.Context, principal auth.Principal, databaseID, migrationID string) (Entry, error) {
	return s.ExecuteAction(ctx, principal, databaseID, migrationID, ActionForward)
}

func (s *Service) ExecuteAction(ctx context.Context, principal auth.Principal, databaseID, migrationID string,
	action ExecutionAction) (resultEntry Entry, resultErr error) {
	ctx = auth.WithPrincipal(ctx, principal)
	ctx, executionSpan := otel.Tracer("dbmp/migration").Start(ctx, "migration.execute",
		trace.WithAttributes(attribute.String("db.system", "postgresql"), attribute.String("db.namespace", databaseID),
			attribute.String("db.migration.id", migrationID), attribute.String("db.migration.action", string(action))))
	defer func() {
		if resultErr != nil {
			executionSpan.RecordError(errors.New("migration execution failed"))
			executionSpan.SetStatus(codes.Error, "migration execution failed")
		} else if resultEntry.Status != "" {
			executionSpan.SetAttributes(attribute.String("db.migration.status", string(resultEntry.Status)))
			if resultEntry.Status != StatusSucceeded {
				executionSpan.SetStatus(codes.Error, string(resultEntry.Status))
			}
		}
		executionSpan.End()
	}()
	queuedJob, isQueued := queuedExecutionFromContext(ctx)
	repository, ok := s.repository.(AttemptRepository)
	if !ok {
		if action == ActionApply {
			return s.executeLegacy(ctx, principal, databaseID, migrationID)
		}
		return Entry{}, ErrInvalidInput
	}
	if err := s.authorizer.RequireDatabaseEdit(ctx, principal, databaseID); err != nil {
		return Entry{}, err
	}
	lockStarted := time.Now()
	lockCtx, lockSpan := otel.Tracer("dbmp/migration").Start(ctx, "migration.lock.acquire",
		trace.WithAttributes(attribute.String("db.namespace", databaseID)))
	lock, err := s.locker.Acquire(lockCtx, databaseID)
	lockWait := time.Since(lockStarted)
	lockSpan.SetAttributes(attribute.Int64("db.lock.wait_ms", lockWait.Milliseconds()))
	if err != nil {
		lockSpan.RecordError(errors.New("migration lock unavailable"))
		lockSpan.SetStatus(codes.Error, "migration lock unavailable")
		lockSpan.End()
		return Entry{}, fmt.Errorf("acquire execution lock: %w", err)
	}
	lockSpan.End()
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if releaseErr := lock.Release(releaseCtx); releaseErr != nil {
			s.logger.Error("release migration execution lock", "database_id", databaseID, "error", releaseErr)
		}
	}()
	if err := s.authorizer.RequireDatabaseEdit(ctx, principal, databaseID); err != nil {
		return Entry{}, err
	}
	plan, err := repository.PlanExecution(ctx, databaseID, migrationID, action, principal.UserID)
	if err != nil {
		return Entry{}, err
	}
	plan.LockWait = lockWait
	if isQueued {
		if plan.Entry.Revision != queuedJob.RequestedRevision || plan.Entry.ExecutionMode != queuedJob.ExecutionMode {
			return Entry{}, ErrRevisionChanged
		}
		plan.JobID = queuedJob.ID
		plan.JobLeaseToken = valueOrEmpty(queuedJob.leaseToken)
		plan.JobWorkerID = valueOrEmpty(queuedJob.workerID)
	}
	approvalAction := string(action)
	if action == ActionRetry {
		approvalAction = string(ActionApply)
	}
	if err := s.approvals.RequireApproved(ctx, databaseID, migrationID, approvalAction, plan.Entry.Revision); err != nil {
		return Entry{}, err
	}
	if err := validateSQLPolicy(plan.SQLSnapshot); err != nil {
		return Entry{}, err
	}
	if executor, ok := s.executor.(*PGExecutor); ok {
		targetCtx, unlockTarget, err := executor.lockExecutionTarget(ctx, databaseID)
		if err != nil {
			return Entry{}, err
		}
		defer unlockTarget()
		ctx = targetCtx
		if guard, ok := repository.(interface {
			unresolvedBatchRoots(context.Context, string) ([]string, error)
		}); ok && hasLockedPostgreSQLTarget(ctx) {
			roots, err := guard.unresolvedBatchRoots(ctx, databaseID)
			if err != nil {
				return Entry{}, err
			}
			if err := checkUnresolvedTargetBatches(ctx, roots); err != nil {
				return Entry{}, err
			}
		}
	}
	var batchExecutor BatchExecutor
	if plan.Entry.ExecutionMode == ExecutionModeBatch {
		var ok bool
		batchExecutor, ok = s.executor.(BatchExecutor)
		if !ok {
			return Entry{}, ErrInvalidInput
		}
		if err := batchExecutor.ValidateBatch(ctx, databaseID, plan.Entry); err != nil {
			return Entry{}, err
		}
	}
	if err := s.executor.Validate(ctx, databaseID, plan.SQLSnapshot); err != nil {
		return Entry{}, err
	}
	executionID, err := shareduuid.New()
	if err != nil {
		return Entry{}, err
	}
	plan.ExecutionID = executionID
	plan.ActorUsername = principal.Username
	if plan.CheckpointRootID == "" {
		plan.CheckpointRootID = executionID
	}
	attempt, err := repository.StartExecution(ctx, plan, principal.UserID, principal.Username, s.clock.Now())
	if err != nil {
		return Entry{}, err
	}
	activity := targetdb.Activity{MigrationID: migrationID, ExecutionID: attempt.ID, Action: string(action),
		Attempt: 1, CorrelationID: requestmeta.RequestID(ctx)}
	if isQueued {
		activity.JobID = queuedJob.ID
		activity.Attempt = queuedJob.RetryCount + 1
		if queuedJob.CorrelationID != nil {
			activity.CorrelationID = *queuedJob.CorrelationID
		}
	}
	ctx = targetdb.WithActivity(ctx, activity)
	startFields := []any{
		"event", "migration_sql_started", "actor", principal.Username, "database_id", databaseID,
		"migration_id", migrationID, "execution_id", attempt.ID, "revision", attempt.Revision,
		"action", action, "query", redactSQLForLog(attempt.SQLSnapshot), "attempt", activity.Attempt,
		"lock_wait_ms", lockWait.Milliseconds(), "job_id", activity.JobID,
	}
	startFields = append(startFields, observability.TraceFields(ctx)...)
	s.logger.InfoContext(ctx, "migration SQL execution started", startFields...)

	executionCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	var result ExecutionResult
	if plan.Entry.ExecutionMode == ExecutionModeBatch {
		batchCtx, batchSpan := otel.Tracer("dbmp/migration").Start(executionCtx, "migration.batch.execute",
			trace.WithAttributes(attribute.String("db.namespace", databaseID),
				attribute.String("db.migration.id", migrationID), attribute.String("db.execution.id", attempt.ID)))
		result = batchExecutor.ExecuteBatch(batchCtx, databaseID, attempt.ID, plan, func(batch BatchCommitEvent) error {
			recordCtx, recordCancel := context.WithTimeout(context.WithoutCancel(executionCtx), 5*time.Second)
			defer recordCancel()
			return repository.RecordBatchEvent(recordCtx, attempt.ID, batch, s.clock.Now())
		})
		batchSpan.SetAttributes(attribute.String("db.migration.status", string(result.Status)))
		if result.InternalError != nil {
			batchSpan.RecordError(errors.New("batch migration failed"))
			batchSpan.SetStatus(codes.Error, "batch migration failed")
		}
		batchSpan.End()
	} else {
		result = s.executor.Execute(executionCtx, databaseID, plan.SQLSnapshot)
	}
	if result.Status != StatusSucceeded && result.Status != StatusFailed && result.Status != StatusInterrupted &&
		result.Status != StatusNeedsReconciliation && result.Status != StatusPartial && result.Status != StatusPartiallyReverted {
		result.Status = StatusInterrupted
		message := "migration outcome is uncertain; manual reconciliation is required"
		result.PublicError = &message
	}
	if plan.Entry.ExecutionMode == ExecutionModeBatch && result.Status != StatusSucceeded {
		recordCtx, recordCancel := context.WithTimeout(context.WithoutCancel(executionCtx), 5*time.Second)
		var recordErr error
		if result.BatchFailure != nil {
			if recorder, ok := s.repository.(BatchFailureRecorder); ok {
				recordErr = recorder.RecordBatchFailure(recordCtx, attempt.ID, string(result.Status), *result.BatchFailure,
					result.PublicError, s.clock.Now())
			} else {
				recordErr = repository.AppendExecutionEvent(recordCtx, attempt.ID, string(result.Status), EventBatchFailed,
					result.PublicError, nil, nil, nil, nil, s.clock.Now())
			}
		} else {
			recordErr = repository.AppendExecutionEvent(recordCtx, attempt.ID, string(result.Status), EventBatchFailed,
				result.PublicError, nil, nil, nil, nil, s.clock.Now())
		}
		if recordErr != nil {
			s.logger.ErrorContext(ctx, "record failed batch in platform history", "event", "migration_batch_failure_record_failed",
				"execution_id", attempt.ID, "error", safeError(recordErr))
		}
		recordCancel()
	}
	finalizeCtx, finalizeCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer finalizeCancel()
	entry, finishErr := repository.FinishExecution(finalizeCtx, attempt, result.Status, result.PublicError, s.clock.Now())
	if finishErr != nil {
		s.logger.Error("CRITICAL: target execution finished but history finalization failed",
			"event", "migration_history_finalize_failed", "database_id", databaseID,
			"migration_id", migrationID, "execution_id", attempt.ID, "target_status", result.Status,
			"execution_error", safeError(result.InternalError), "history_error", safeError(finishErr))
		return Entry{}, fmt.Errorf("finalize migration history: %w", finishErr)
	}
	if result.InternalError != nil {
		finishFields := []any{"event", "migration_sql_finished",
			"actor", principal.Username, "database_id", databaseID, "migration_id", migrationID,
			"execution_id", attempt.ID, "status", result.Status, "query", redactSQLForLog(attempt.SQLSnapshot),
			"error", safeError(result.InternalError)}
		finishFields = append(finishFields, observability.TraceFields(ctx)...)
		s.logger.WarnContext(ctx, "migration execution did not succeed", finishFields...)
	} else {
		finishFields := []any{"event", "migration_sql_finished",
			"actor", principal.Username, "database_id", databaseID, "migration_id", migrationID,
			"execution_id", attempt.ID, "status", result.Status, "query", redactSQLForLog(attempt.SQLSnapshot)}
		finishFields = append(finishFields, observability.TraceFields(ctx)...)
		s.logger.InfoContext(ctx, "migration SQL execution finished", finishFields...)
	}
	entry.CanEdit = true
	entry.CanReconcile = needsReconciliation(entry.Status)
	entry.CreatedByUsername = plan.Entry.CreatedByUsername
	entry.ExecutedByUsername = &principal.Username
	entry.attemptStatus = result.Status
	entry.attemptError = result.PublicError
	return entry, nil
}

func (s *Service) executeLegacy(ctx context.Context, principal auth.Principal, databaseID, migrationID string) (Entry, error) {
	if err := s.authorizer.RequireDatabaseEdit(ctx, principal, databaseID); err != nil {
		return Entry{}, err
	}
	lock, err := s.locker.Acquire(ctx, databaseID)
	if err != nil {
		return Entry{}, fmt.Errorf("acquire execution lock: %w", err)
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if releaseErr := lock.Release(releaseCtx); releaseErr != nil {
			s.logger.Error("release migration execution lock", "database_id", databaseID, "error", releaseErr)
		}
	}()

	if err := s.authorizer.RequireDatabaseEdit(ctx, principal, databaseID); err != nil {
		return Entry{}, err
	}
	entry, err := s.repository.Get(ctx, databaseID, migrationID)
	if err != nil {
		return Entry{}, err
	}
	if entry.Status != StatusDraft {
		return Entry{}, ErrConflict
	}
	// Dicek setelah lock, untuk revisi yang SQL-nya akan dijalankan.
	if err := s.approvals.RequireApproved(ctx, databaseID, migrationID, "apply", entry.Revision); err != nil {
		return Entry{}, err
	}
	if err := validateSQLPolicy(entry.SQL); err != nil {
		return Entry{}, err
	}
	if err := s.executor.Validate(ctx, databaseID, entry.SQL); err != nil {
		return Entry{}, err
	}

	creatorUsername := entry.CreatedByUsername
	entry, err = s.repository.MarkRunning(ctx, databaseID, migrationID, principal.UserID, entry.Revision, s.clock.Now())
	if err != nil {
		return Entry{}, err
	}

	executionCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	result := s.executor.Execute(executionCtx, databaseID, entry.SQL)
	if result.Status != StatusSucceeded && result.Status != StatusFailed && result.Status != StatusInterrupted && result.Status != StatusNeedsReconciliation {
		result.Status = StatusInterrupted
		message := "migration outcome is uncertain; manual reconciliation is required"
		result.PublicError = &message
	}

	finalizeCtx, finalizeCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer finalizeCancel()
	terminal, terminalErr := s.repository.MarkTerminal(finalizeCtx, databaseID, migrationID, result.Status, result.PublicError, s.clock.Now())
	if terminalErr != nil {
		s.logger.Error("CRITICAL: target execution finished but history finalization failed",
			"database_id", databaseID, "migration_id", migrationID, "target_status", result.Status,
			"execution_error", safeError(result.InternalError), "history_error", safeError(terminalErr))
		return Entry{}, fmt.Errorf("finalize migration history: %w", terminalErr)
	}
	if result.InternalError != nil {
		s.logger.Warn("migration execution did not succeed", "database_id", databaseID,
			"migration_id", migrationID, "status", result.Status, "error", safeError(result.InternalError))
	}
	terminal.CanEdit = true
	terminal.CreatedByUsername = creatorUsername
	terminal.ExecutedByUsername = &principal.Username
	return terminal, nil
}

func (s *Service) validateDraft(name, sqlText string) (string, string, error) {
	reason := "No down SQL was supplied by this legacy Milestone 1 client."
	definition, err := s.validateDefinition(Definition{Name: name, SQL: sqlText,
		IrreversibleReason: &reason, ExecutionMode: ExecutionModeSingle}, true)
	return definition.Name, definition.SQL, err
}

func (s *Service) validateDefinition(definition Definition, create bool) (Definition, error) {
	definition.Name = strings.TrimSpace(definition.Name)
	definition.ExecutionMode = normalizeExecutionMode(definition.ExecutionMode)
	if definition.Name == "" || len(definition.Name) > 160 || strings.TrimSpace(definition.SQL) == "" ||
		len([]byte(definition.SQL)) > s.maxSQLBytes {
		return Definition{}, ErrInvalidInput
	}
	if definition.DownSQL == nil && definition.IrreversibleReason == nil && create {
		reason := "No down SQL was supplied by this legacy Milestone 1 client."
		definition.IrreversibleReason = &reason
	}
	if (definition.DownSQL == nil) == (definition.IrreversibleReason == nil) {
		return Definition{}, ErrInvalidInput
	}
	if definition.DownSQL != nil && (strings.TrimSpace(*definition.DownSQL) == "" || len([]byte(*definition.DownSQL)) > s.maxSQLBytes) {
		return Definition{}, ErrInvalidInput
	}
	if definition.IrreversibleReason != nil {
		reason := strings.TrimSpace(*definition.IrreversibleReason)
		if reason == "" || len([]rune(reason)) > 1000 {
			return Definition{}, ErrInvalidInput
		}
		definition.IrreversibleReason = &reason
	}
	if err := validateSQLPolicy(definition.SQL); err != nil {
		return Definition{}, err
	}
	if definition.DownSQL != nil {
		if err := validateSQLPolicy(*definition.DownSQL); err != nil {
			return Definition{}, err
		}
	}
	switch definition.ExecutionMode {
	case ExecutionModeSingle:
		if definition.BatchSelectSQL != nil || definition.BatchSize != nil {
			return Definition{}, ErrInvalidInput
		}
	case ExecutionModeBatch:
		if definition.BatchSelectSQL == nil || definition.BatchSize == nil || *definition.BatchSize < 1 || *definition.BatchSize > 10000 ||
			len([]byte(*definition.BatchSelectSQL)) > s.maxSQLBytes || strings.TrimSpace(*definition.BatchSelectSQL) == "" {
			return Definition{}, ErrInvalidInput
		}
		words, err := topLevelStatementWords(*definition.BatchSelectSQL)
		if err != nil || len(words) != 1 || len(words[0]) == 0 || words[0][0] != "SELECT" ||
			!strings.Contains(*definition.BatchSelectSQL, "$1") || !strings.Contains(*definition.BatchSelectSQL, "$2") ||
			!strings.Contains(strings.ToUpper(*definition.BatchSelectSQL), "LIMIT") {
			return Definition{}, ErrSQLPolicy
		}
		upWords, upErr := topLevelStatementWords(definition.SQL)
		if upErr != nil || len(upWords) != 1 || len(upWords[0]) == 0 || !batchStatement(upWords[0][0]) ||
			!strings.Contains(definition.SQL, "$1") || !strings.Contains(definition.SQL, "$2") {
			return Definition{}, ErrInvalidInput
		}
		if definition.DownSQL != nil {
			downWords, downErr := topLevelStatementWords(*definition.DownSQL)
			if downErr != nil || len(downWords) != 1 || len(downWords[0]) == 0 || !batchStatement(downWords[0][0]) ||
				!strings.Contains(*definition.DownSQL, "$1") || !strings.Contains(*definition.DownSQL, "$2") {
				return Definition{}, ErrInvalidInput
			}
		}
	default:
		return Definition{}, ErrInvalidInput
	}
	return definition, nil
}

func batchStatement(word string) bool {
	return word == "UPDATE" || word == "DELETE" || word == "INSERT"
}

func sanitizeForViewer(message *string) *string {
	if message == nil {
		return nil
	}
	safe := "migration failed; contact a database editor for details"
	return &safe
}

func safeError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, targetdb.ErrTargetUnavailable) {
		return "target database unavailable"
	}
	type sqlStateError interface{ SQLState() string }
	var stateError sqlStateError
	if errors.As(err, &stateError) && stateError.SQLState() != "" {
		return "database operation failed (SQLSTATE " + stateError.SQLState() + ")"
	}
	return "migration operation failed"
}

func redactSQLForLog(sqlText string) string {
	return targetdb.RedactSQLStatement(sqlText)
}

func (s *Service) ListHistory(ctx context.Context, principal auth.Principal, databaseID string, options HistoryOptions) (HistoryPage, error) {
	if err := s.authorizer.RequireDatabaseView(ctx, principal, databaseID); err != nil {
		return HistoryPage{}, err
	}
	if options.Limit == 0 {
		options.Limit = DefaultPageSize
	}
	if options.Limit < 1 || options.Limit > MaxPageSize || options.Offset < 0 {
		return HistoryPage{}, ErrInvalidInput
	}
	repository, ok := s.repository.(AttemptRepository)
	if !ok {
		return HistoryPage{}, ErrConflict
	}
	page, err := repository.ListHistory(ctx, databaseID, options)
	if err != nil {
		return HistoryPage{}, err
	}
	if err := s.authorizer.RequireDatabaseEdit(ctx, principal, databaseID); err != nil {
		for index := range page.Items {
			page.Items[index].ErrorMessage = sanitizeForViewer(page.Items[index].ErrorMessage)
			for eventIndex := range page.Items[index].Events {
				page.Items[index].Events[eventIndex].ErrorMessage = sanitizeForViewer(page.Items[index].Events[eventIndex].ErrorMessage)
			}
		}
	}
	return page, nil
}

func (s *Service) GetExecution(ctx context.Context, principal auth.Principal, databaseID, executionID string) (ExecutionAttempt, error) {
	if err := s.authorizer.RequireDatabaseView(ctx, principal, databaseID); err != nil {
		return ExecutionAttempt{}, err
	}
	repository, ok := s.repository.(AttemptRepository)
	if !ok {
		return ExecutionAttempt{}, ErrConflict
	}
	attempt, err := repository.GetHistory(ctx, databaseID, executionID)
	if err != nil {
		return ExecutionAttempt{}, err
	}
	if err := s.authorizer.RequireDatabaseEdit(ctx, principal, databaseID); err != nil {
		attempt.ErrorMessage = sanitizeForViewer(attempt.ErrorMessage)
		for index := range attempt.Events {
			attempt.Events[index].ErrorMessage = sanitizeForViewer(attempt.Events[index].ErrorMessage)
		}
	}
	return attempt, nil
}

func (s *Service) GetBatchProgress(ctx context.Context, principal auth.Principal, databaseID, executionID string) (BatchProgress, error) {
	if err := s.authorizer.RequireDatabaseView(ctx, principal, databaseID); err != nil {
		return BatchProgress{}, err
	}
	repository, ok := s.repository.(AttemptRepository)
	if !ok {
		return BatchProgress{}, ErrConflict
	}
	attempt, err := repository.GetHistory(ctx, databaseID, executionID)
	if err != nil {
		return BatchProgress{}, err
	}
	if attempt.JobID != nil {
		return BatchProgress{}, ErrNoAttempt
	}
	executor, ok := s.executor.(BatchExecutor)
	if !ok {
		return BatchProgress{}, ErrConflict
	}
	return executor.BatchProgress(ctx, databaseID, attempt)
}
