package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/approval"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/config"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/database"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/migration"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/observability"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/platformdb"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "worker stopped: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	logger, closeLog, err := newLogger(cfg.LogLevel)
	if err != nil {
		return err
	}
	defer closeLog()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serviceName := os.Getenv("OTEL_SERVICE_NAME")
	if serviceName == "" {
		serviceName = "dbmp-worker"
	}
	shutdownTracing, err := observability.SetupTracing(ctx, serviceName)
	if err != nil {
		return fmt.Errorf("configure tracing: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTracing(shutdownCtx); err != nil {
			logger.Warn("flush traces", "error", err)
		}
	}()

	platformDatabase, err := platformdb.Open(ctx, cfg.PlatformDatabase)
	if err != nil {
		return err
	}
	defer platformDatabase.Close()
	credentialCipher, err := database.LoadCredentialCipher(os.LookupEnv)
	if err != nil {
		return fmt.Errorf("load managed database credential key: %w", err)
	}
	policy := authz.NewPolicy(authz.NewPGRepository(platformDatabase))
	databases, err := database.NewService(database.NewPGRepository(platformDatabase), credentialCipher, policy, nil)
	if err != nil {
		return fmt.Errorf("configure managed databases: %w", err)
	}
	provider := targetdb.NewLoggingProvider(
		targetdb.NewProvider(databases, targetdb.DefaultConnectTimeout, logger), logger)
	approvals, err := approval.NewService(approval.NewPGRepository(platformDatabase), policy, nil)
	if err != nil {
		return fmt.Errorf("configure migration approvals: %w", err)
	}
	repository := migration.NewPGRepository(platformDatabase)
	service, err := migration.NewService(repository, policy, migration.NewPGAdvisoryLocker(platformDatabase),
		migration.NewPGExecutorWithLogger(provider, logger), approvals, migration.RealClock{}, logger,
		migration.ServiceConfig{})
	if err != nil {
		return fmt.Errorf("configure migration service: %w", err)
	}
	workerConfig, err := migration.LoadWorkerConfig()
	if err != nil {
		return fmt.Errorf("load migration worker configuration: %w", err)
	}
	worker, err := migration.NewWorker(migration.NewPGJobRepository(platformDatabase), service, workerConfig, logger)
	if err != nil {
		return fmt.Errorf("configure migration worker: %w", err)
	}
	return worker.Run(ctx)
}

func newLogger(level slog.Level) (*slog.Logger, func(), error) {
	var writer io.Writer = os.Stdout
	logPath := os.Getenv("APP_LOG_FILE")
	if logPath == "" {
		return slog.New(slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: level})).With("service.name", "dbmp-worker"), func() {}, nil
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0750); err != nil {
		return nil, nil, fmt.Errorf("create application log directory: %w", err)
	}
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, nil, fmt.Errorf("open application log file: %w", err)
	}
	writer = io.MultiWriter(os.Stdout, file)
	return slog.New(slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: level})).With("service.name", "dbmp-worker"), func() { _ = file.Close() }, nil
}
