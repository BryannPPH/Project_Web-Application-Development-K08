package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/session"
)

type DatabaseHealthChecker interface {
	Ping(context.Context) error
}

type Dependencies struct {
	Logger                *slog.Logger
	Database              DatabaseHealthChecker
	DatabaseHealthTimeout time.Duration
	Authentication        LoginService
	Sessions              AuthSessionService
	SessionCookie         session.CookieConfig
	LoginLimiter          LoginAttemptLimiter
	TrustedOrigins        []string
	// RegisterProtectedRoutes mendaftarkan route fitur yang terproteksi.
	RegisterProtectedRoutes func(mux *http.ServeMux, security *AuthMiddleware)
}

func NewHandler(deps Dependencies) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", health)
	mux.HandleFunc("GET /api/v1/ready", ready(deps.Database, deps.DatabaseHealthTimeout))

	var security *AuthMiddleware
	if deps.Sessions != nil {
		security = NewAuthMiddleware(deps.Sessions, deps.SessionCookie, deps.Logger, deps.TrustedOrigins...)
	}
	if deps.Authentication != nil && deps.Sessions != nil {
		loginLimiter := deps.LoginLimiter
		if loginLimiter == nil {
			loginLimiter = newDefaultLoginLimiter()
		}
		registerAuthRoutes(
			mux,
			deps.Authentication,
			deps.Sessions,
			security,
			deps.SessionCookie,
			loginLimiter,
			deps.Logger,
		)
	}
	if deps.RegisterProtectedRoutes != nil && security != nil {
		deps.RegisterProtectedRoutes(mux, security)
	}
	mux.HandleFunc("/", notFound)

	return requestID(
		accessLog(deps.Logger,
			recoverPanic(deps.Logger,
				securityHeaders(mux),
			),
		),
	)
}

func health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func ready(database DatabaseHealthChecker, timeout time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		if err := database.Ping(ctx); err != nil {
			writeError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "service is not ready")
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	}
}

func notFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, http.StatusNotFound, "NOT_FOUND", "resource not found")
}
