package platformdb

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/config"
)

var (
	ErrInvalidConfiguration = errors.New("invalid platform database configuration")
	ErrUnavailable          = errors.New("platform database is unavailable")
)

func Open(ctx context.Context, cfg config.PlatformDatabaseConfig) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, ErrInvalidConfiguration
	}

	poolConfig.MinConns = cfg.MinConnections
	poolConfig.MaxConns = cfg.MaxConnections
	poolConfig.MaxConnIdleTime = cfg.MaxConnectionIdle
	poolConfig.MaxConnLifetime = cfg.MaxConnectionLife

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, ErrUnavailable
	}

	healthCtx, cancel := context.WithTimeout(ctx, cfg.HealthTimeout)
	defer cancel()

	if err := pool.Ping(healthCtx); err != nil {
		pool.Close()
		return nil, ErrUnavailable
	}

	return pool, nil
}
