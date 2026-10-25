package migration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
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
	clock       Clock
	logger      *slog.Logger
	maxSQLBytes int
	timeout     time.Duration
}

type ServiceConfig struct {
	MaxSQLBytes      int
	ExecutionTimeout time.Duration
}

func NewService(repository Repository, authorizer Authorizer, locker Locker, executor Executor, clock Clock, logger *slog.Logger, config ServiceConfig) (*Service, error) {
	if repository == nil || authorizer == nil || locker == nil || executor == nil {
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
	return &Service{repository: repository, authorizer: authorizer, locker: locker, executor: executor,
		clock: clock, logger: logger, maxSQLBytes: config.MaxSQLBytes, timeout: config.ExecutionTimeout}, nil
}

func (s *Service) CreateDraft(ctx context.Context, principal auth.Principal, databaseID, name, sqlText string) (Entry, error) {
	if err := s.authorizer.RequireDatabaseEdit(ctx, principal, databaseID); err != nil {
		return Entry{}, err
	}
	name, sqlText, err := s.validateDraft(name, sqlText)
	if err != nil {
		return Entry{}, err
	}
	entry, err := s.repository.CreateDraft(ctx, databaseID, name, sqlText, principal.UserID, s.clock.Now())
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
	if err := s.authorizer.RequireDatabaseEdit(ctx, principal, databaseID); err != nil {
		entry.ErrorMessage = sanitizeForViewer(entry.ErrorMessage)
	} else {
		entry.CanEdit = true
	}
	return entry, nil
}

func (s *Service) Execute(ctx context.Context, principal auth.Principal, databaseID, migrationID string) (Entry, error) {
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
	if err := validateSQLPolicy(entry.SQL); err != nil {
		return Entry{}, err
	}
	if err := s.executor.Validate(ctx, databaseID, entry.SQL); err != nil {
		return Entry{}, err
	}

	creatorUsername := entry.CreatedByUsername
	entry, err = s.repository.MarkRunning(ctx, databaseID, migrationID, principal.UserID, s.clock.Now())
	if err != nil {
		return Entry{}, err
	}

	executionCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	result := s.executor.Execute(executionCtx, databaseID, entry.SQL)
	if result.Status != StatusSucceeded && result.Status != StatusFailed && result.Status != StatusInterrupted {
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
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 160 || strings.TrimSpace(sqlText) == "" || len([]byte(sqlText)) > s.maxSQLBytes {
		return "", "", ErrInvalidInput
	}
	if err := validateSQLPolicy(sqlText); err != nil {
		return "", "", err
	}
	return name, sqlText, nil
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
	return sanitizeTargetError(err)
}
