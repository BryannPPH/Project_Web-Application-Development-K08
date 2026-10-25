package platformfeature

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/app"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/database"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/httpapi"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/migration"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/platformaccess"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/schemadiff"
)

const defaultStartupTimeout = 10 * time.Second

type Options struct {
	CredentialCipher     *database.CredentialCipher
	TargetConnectTimeout time.Duration
	ViewerQueryTimeout   time.Duration
	Migration            migration.ServiceConfig
	StartupTimeout       time.Duration
}

// FeatureFactory membangun platform access terlebih dahulu agar authz dan
// target provider yang sama dapat dipakai oleh viewer serta migration.
func FeatureFactory(startupContext context.Context, options Options) app.FeatureFactory {
	return func(dependencies app.FeatureDependencies) (app.ProtectedRouteRegistrar, error) {
		if startupContext == nil {
			return nil, errors.New("platform feature requires a startup context")
		}

		platformRegistrar, components, err := platformaccess.Build(platformaccess.Options{
			CredentialCipher:     options.CredentialCipher,
			TargetConnectTimeout: options.TargetConnectTimeout,
			ViewerQueryTimeout:   options.ViewerQueryTimeout,
		}, dependencies)
		if err != nil {
			return nil, fmt.Errorf("configure platform access: %w", err)
		}

		startupTimeout := options.StartupTimeout
		if startupTimeout <= 0 {
			startupTimeout = defaultStartupTimeout
		}
		ctx, cancel := context.WithTimeout(startupContext, startupTimeout)
		defer cancel()

		migrationHandler, err := migration.NewModule(
			ctx,
			dependencies.Database,
			components.Authz,
			components.TargetProvider,
			dependencies.Logger,
			options.Migration,
		)
		if err != nil {
			return nil, fmt.Errorf("configure migration: %w", err)
		}

		// Bonus: analisis execution plan dan schema diff memakai authz serta
		// target provider yang sama dengan viewer dan migration.
		explainService, err := migration.NewExplainService(components.Authz, components.TargetProvider, options.Migration)
		if err != nil {
			return nil, fmt.Errorf("configure migration explain: %w", err)
		}
		explainHandler := migration.NewExplainHandler(explainService, dependencies.Logger, options.Migration.MaxSQLBytes)
		diffService, err := schemadiff.NewService(components.Authz, components.TargetProvider)
		if err != nil {
			return nil, fmt.Errorf("configure schema diff: %w", err)
		}
		diffHandler := schemadiff.NewHandler(diffService, dependencies.Logger)

		return func(mux *http.ServeMux, security *httpapi.AuthMiddleware) {
			platformRegistrar(mux, security)
			migrationHandler.RegisterRoutes(mux, security)
			explainHandler.RegisterRoutes(mux, security)
			diffHandler.RegisterRoutes(mux, security)
		}, nil
	}
}
