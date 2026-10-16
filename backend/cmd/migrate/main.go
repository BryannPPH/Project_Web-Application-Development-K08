package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/config"
	platformmigrations "github.com/BryannPPH/Project_Web-Application-Development-K08/backend/migrations"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "migration failed: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: go run ./cmd/migrate [up|status|version]")
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	databaseConfig, err := pgx.ParseConfig(cfg.PlatformDatabase.URL)
	if err != nil {
		return errors.New("invalid platform database configuration")
	}

	database := stdlib.OpenDB(*databaseConfig)
	defer database.Close()

	healthCtx, cancelHealthCheck := context.WithTimeout(ctx, cfg.PlatformDatabase.HealthTimeout)
	defer cancelHealthCheck()
	if err := ping(healthCtx, database); err != nil {
		return err
	}
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("configure migration dialect: %w", err)
	}
	goose.SetBaseFS(platformmigrations.Files)

	switch args[0] {
	case "up":
		return goose.UpContext(ctx, database, ".")
	case "status":
		return goose.StatusContext(ctx, database, ".")
	case "version":
		version, err := goose.GetDBVersionContext(ctx, database)
		if err != nil {
			return fmt.Errorf("read migration version: %w", err)
		}
		fmt.Println(version)
		return nil
	default:
		return fmt.Errorf("unknown command %q; use up, status, or version", args[0])
	}
}

func ping(ctx context.Context, database *sql.DB) error {
	if err := database.PingContext(ctx); err != nil {
		return errors.New("platform database is unavailable")
	}
	return nil
}
