package migration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb"
)

func NewModule(
	ctx context.Context,
	platformDatabase *pgxpool.Pool,
	authorizer Authorizer,
	targets targetdb.Provider,
	logger *slog.Logger,
	config ServiceConfig,
) (*Handler, error) {
	if ctx == nil || platformDatabase == nil || authorizer == nil || targets == nil {
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
		NewPGExecutor(targets),
		RealClock{},
		logger,
		config,
	)
	if err != nil {
		return nil, err
	}
	if err := recoverRunning(ctx, repository, logger, time.Now().UTC()); err != nil {
		return nil, err
	}
	return newHandler(service, logger, service.maxSQLBytes), nil
}

const recoveryReason = "migration execution was interrupted by a server restart; manual reconciliation is required"

func recoverRunning(ctx context.Context, repository Repository, logger *slog.Logger, before time.Time) error {
	recovered, err := repository.RecoverRunning(ctx, before, recoveryReason)
	if err != nil {
		return fmt.Errorf("recover interrupted migrations: %w", err)
	}
	if recovered > 0 {
		logger.WarnContext(ctx, "recovered interrupted migrations", "count", recovered)
	}
	return nil
}
