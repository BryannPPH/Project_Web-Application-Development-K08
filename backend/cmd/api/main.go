package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/app"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/config"
	manageddb "github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/database"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/platformaccess"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/platformdb"
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

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

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
		app.WithFeatureFactory(platformaccess.FeatureFactory(platformaccess.Options{
			CredentialCipher: credentialCipher,
		})),
	)
	if err != nil {
		return err
	}
	return application.Run(ctx)
}
