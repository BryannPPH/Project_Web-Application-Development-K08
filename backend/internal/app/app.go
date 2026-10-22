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

type ProtectedRouteRegistrar func(mux *http.ServeMux, security *httpapi.AuthMiddleware)

type FeatureDependencies struct {
	Database       *pgxpool.Pool
	Logger         *slog.Logger
	PasswordHasher auth.PasswordHasher
	SessionRevoker auth.SessionRevoker
}

type FeatureFactory func(dependencies FeatureDependencies) (ProtectedRouteRegistrar, error)

type Option func(*appOptions) error

type appOptions struct {
	featureFactories []FeatureFactory
}

func WithFeatureFactory(factory FeatureFactory) Option {
	return func(options *appOptions) error {
		if factory == nil {
			return errors.New("feature factory is required")
		}
		options.featureFactories = append(options.featureFactories, factory)
		return nil
	}
}

func New(cfg config.Config, logger *slog.Logger, database *pgxpool.Pool, options ...Option) (*App, error) {
	resolvedOptions := appOptions{}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("app option is required")
		}
		if err := option(&resolvedOptions); err != nil {
			return nil, fmt.Errorf("configure app option: %w", err)
		}
	}

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
	loginLimiter, err := httpapi.NewFixedWindowLoginLimiter(
		cfg.Authentication.LoginRateLimitAttempts,
		cfg.Authentication.LoginRateLimitWindow,
	)
	if err != nil {
		return nil, fmt.Errorf("configure login rate limiter: %w", err)
	}
	protectedRoutes, err := buildFeatureRegistrar(resolvedOptions.featureFactories, FeatureDependencies{
		Database:       database,
		Logger:         logger,
		PasswordHasher: passwordHasher,
		SessionRevoker: sessionService,
	})
	if err != nil {
		return nil, err
	}

	handler := httpapi.NewHandler(httpapi.Dependencies{
		Logger:                logger,
		Database:              database,
		DatabaseHealthTimeout: cfg.PlatformDatabase.HealthTimeout,
		Authentication:        authenticationService,
		Sessions:              sessionService,
		SessionCookie:         cookie,
		LoginLimiter:          loginLimiter,
		TrustedOrigins:        cfg.HTTP.TrustedOrigins,
		RegisterProtectedRoutes: func(mux *http.ServeMux, security *httpapi.AuthMiddleware) {
			if protectedRoutes != nil {
				protectedRoutes(mux, security)
			}
		},
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

func buildFeatureRegistrar(
	factories []FeatureFactory,
	dependencies FeatureDependencies,
) (ProtectedRouteRegistrar, error) {
	registrars := make([]ProtectedRouteRegistrar, 0, len(factories))
	for index, factory := range factories {
		registrar, err := factory(dependencies)
		if err != nil {
			return nil, fmt.Errorf("configure feature %d: %w", index+1, err)
		}
		if registrar != nil {
			registrars = append(registrars, registrar)
		}
	}
	if len(registrars) == 0 {
		return nil, nil
	}
	return func(mux *http.ServeMux, security *httpapi.AuthMiddleware) {
		for _, registrar := range registrars {
			registrar(mux, security)
		}
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
