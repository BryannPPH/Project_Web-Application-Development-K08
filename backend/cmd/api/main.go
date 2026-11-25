package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/app"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/config"
	manageddb "github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/database"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/metrics"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/observability"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/platformdb"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/platformfeature"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "api stopped: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	logWriter := io.Writer(os.Stdout)
	if logPath := strings.TrimSpace(os.Getenv("APP_LOG_FILE")); logPath != "" {
		if err := os.MkdirAll(filepath.Dir(logPath), 0750); err != nil {
			return fmt.Errorf("create application log directory: %w", err)
		}
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("open application log file: %w", err)
		}
		defer logFile.Close()
		logWriter = io.MultiWriter(os.Stdout, logFile)
	}
	logger := slog.New(slog.NewJSONHandler(logWriter, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	})).With("service.name", "dbmp-backend")

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()
	shutdownTracing, err := observability.SetupTracing(ctx, "dbmp-backend")
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

	database, err := platformdb.Open(ctx, cfg.PlatformDatabase)
	if err != nil {
		return err
	}
	defer database.Close()

	logger.Info("platform database connection established")

	// Startup gagal bila key credential managed database hilang atau tidak valid.
	credentialCipher, err := manageddb.LoadCredentialCipher(os.LookupEnv)
	if err != nil {
		return fmt.Errorf("load managed database credential key: %w", err)
	}

	application, err := app.New(cfg, logger, database,
		app.WithFeatureFactory(platformfeature.FeatureFactory(ctx, platformfeature.Options{
			CredentialCipher: credentialCipher,
		})),
		// GET /metrics untuk Prometheus; METRICS_TOKEN (opsional) mewajibkan Bearer token.
		app.WithFeatureFactory(metrics.FeatureFactory(metrics.Options{Token: os.Getenv("METRICS_TOKEN")})),
	)
	if err != nil {
		return err
	}
	return application.Run(ctx)
}
