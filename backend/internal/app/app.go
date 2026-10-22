package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/config"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/httpapi"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/session"
)

type App struct {
	server          *http.Server
	logger          *slog.Logger
	shutdownTimeout time.Duration
	listen          func(network, address string) (net.Listener, error)
}

func New(cfg config.Config, logger *slog.Logger, database *pgxpool.Pool) (*App, error) {
	passwordHasher := auth.NewArgon2idHasher()
	sessionService, err := session.NewService(
		session.NewPGRepository(database),
		session.NewRandomTokenSource(),
		session.RealClock{},
		session.Config{
			IdleTimeout:        cfg.Authentication.SessionIdleTimeout,
			AbsoluteTimeout:    cfg.Authentication.SessionAbsoluteTimeout,
			RotationInterval:   cfg.Authentication.SessionRotationInterval,
			PreviousTokenGrace: cfg.Authentication.SessionPreviousTokenGrace,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("configure session service: %w", err)
	}
	authenticationService, err := auth.NewService(
		auth.NewPGUserRepository(database),
		passwordHasher,
		sessionService,
	)
	if err != nil {
		return nil, fmt.Errorf("configure authentication service: %w", err)
	}
	cookie := session.CookieConfig{
		Name:   cfg.Authentication.SessionCookieName,
		Secure: cfg.Authentication.SessionCookieSecure,
	}

	handler := httpapi.NewHandler(httpapi.Dependencies{
		Logger:                logger,
		Database:              database,
		DatabaseHealthTimeout: cfg.PlatformDatabase.HealthTimeout,
		Authentication:        authenticationService,
		Sessions:              sessionService,
		SessionCookie:         cookie,
	})

	return &App{
		server: &http.Server{
			Addr:              cfg.HTTP.Address,
			Handler:           handler,
			ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
			ReadTimeout:       cfg.HTTP.ReadTimeout,
			WriteTimeout:      cfg.HTTP.WriteTimeout,
			IdleTimeout:       cfg.HTTP.IdleTimeout,
			ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
		},
		logger:          logger,
		shutdownTimeout: cfg.HTTP.ShutdownTimeout,
		listen:          net.Listen,
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	listen := a.listen
	if listen == nil {
		listen = net.Listen
	}
	listener, err := listen("tcp", a.server.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", a.server.Addr, err)
	}

	a.logger.Info("HTTP server started", "address", listener.Addr().String())

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- a.server.Serve(listener)
	}()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.shutdownTimeout)
	defer cancel()

	if err := a.server.Shutdown(shutdownCtx); err != nil {
		_ = a.server.Close()
		return fmt.Errorf("graceful HTTP shutdown: %w", err)
	}

	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve HTTP during shutdown: %w", err)
	}

	a.logger.Info("HTTP server stopped")
	return nil
}
