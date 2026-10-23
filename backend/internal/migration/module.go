package migration

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

func NewModule(
	platformDatabase *pgxpool.Pool,
	authorizer Authorizer,
	targets TargetProvider,
	logger *slog.Logger,
	config ServiceConfig,
) (*Handler, error) {
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
	return NewHandler(service, logger), nil
}
