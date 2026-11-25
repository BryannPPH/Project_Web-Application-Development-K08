package migration

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb"
)

func NewModule(
	ctx context.Context,
	platformDatabase *pgxpool.Pool,
	authorizer Authorizer,
	approvals ApprovalGate,
	targets targetdb.Provider,
	logger *slog.Logger,
	config ServiceConfig,
) (*Handler, error) {
	if ctx == nil || platformDatabase == nil || authorizer == nil || approvals == nil || targets == nil {
		return nil, errors.New("migration module dependencies are required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	repository := NewPGRepository(platformDatabase)
	service, err := NewService(
		repository,
		authorizer,
		NewPGAdvisoryLocker(platformDatabase),
		NewPGExecutorWithLogger(targets, logger),
		approvals,
		RealClock{},
		logger,
		config,
	)
	if err != nil {
		return nil, err
	}
	queue, err := NewQueueService(NewPGJobRepository(platformDatabase), repository, authorizer, approvals, RealClock{}, logger)
	if err != nil {
		return nil, err
	}
	return newHandlerWithJobs(service, queue, logger, service.maxSQLBytes), nil
}
