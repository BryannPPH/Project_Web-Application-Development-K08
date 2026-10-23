// Package platformaccess merangkai modul Person 2 (authz, user, group, managed
// database, targetdb, viewer) menjadi satu app.FeatureFactory.
//
// Composition root (internal/app) tetap milik integration owner; package ini
// hanya membangun dependency dan mendaftarkan route melalui kontrak
// app.WithFeatureFactory, tanpa global singleton.
package platformaccess

import (
	"errors"
	"net/http"
	"time"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/app"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/database"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/group"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/httpapi"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/user"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/viewer"
)

// Options mengatur dependency yang berasal dari environment.
type Options struct {
	CredentialCipher     *database.CredentialCipher
	TargetConnectTimeout time.Duration
	ViewerQueryTimeout   time.Duration
}

// Components berisi service yang dapat dipakai modul lain (mis. migration
// Person 3) tanpa mengimpor repository konkret.
type Components struct {
	Authz          authz.Service
	TargetProvider targetdb.Provider
}

// Build membuat seluruh service dan mengembalikan registrar beserta kontrak publik.
func Build(options Options, dependencies app.FeatureDependencies) (app.ProtectedRouteRegistrar, Components, error) {
	if options.CredentialCipher == nil {
		return nil, Components{}, errors.New("platform access feature requires a credential cipher")
	}
	if dependencies.Database == nil || dependencies.PasswordHasher == nil || dependencies.SessionRevoker == nil {
		return nil, Components{}, errors.New("platform access feature requires database, password hasher, and session revoker")
	}

	policy := authz.NewPolicy(authz.NewPGRepository(dependencies.Database))

	users, err := user.NewService(
		user.NewPGRepository(dependencies.Database),
		policy,
		dependencies.PasswordHasher,
		dependencies.SessionRevoker,
	)
	if err != nil {
		return nil, Components{}, err
	}
	groups, err := group.NewService(group.NewPGRepository(dependencies.Database), policy, dependencies.SessionRevoker)
	if err != nil {
		return nil, Components{}, err
	}
	databases, err := database.NewService(
		database.NewPGRepository(dependencies.Database),
		options.CredentialCipher,
		policy,
		nil,
	)
	if err != nil {
		return nil, Components{}, err
	}
	provider := targetdb.NewPGProvider(databases, options.TargetConnectTimeout, dependencies.Logger)
	viewers, err := viewer.NewService(policy, provider, options.ViewerQueryTimeout)
	if err != nil {
		return nil, Components{}, err
	}

	handlers := []interface {
		Register(*http.ServeMux, *httpapi.AuthMiddleware)
	}{
		user.NewHandler(users, dependencies.Logger),
		group.NewHandler(groups, dependencies.Logger),
		database.NewHandler(databases, dependencies.Logger),
		viewer.NewHandler(viewers, dependencies.Logger),
	}
	registrar := func(mux *http.ServeMux, security *httpapi.AuthMiddleware) {
		for _, handler := range handlers {
			handler.Register(mux, security)
		}
	}
	return registrar, Components{Authz: policy, TargetProvider: provider}, nil
}

// FeatureFactory membungkus Build untuk app.WithFeatureFactory.
func FeatureFactory(options Options) app.FeatureFactory {
	return func(dependencies app.FeatureDependencies) (app.ProtectedRouteRegistrar, error) {
		registrar, _, err := Build(options, dependencies)
		return registrar, err
	}
}
