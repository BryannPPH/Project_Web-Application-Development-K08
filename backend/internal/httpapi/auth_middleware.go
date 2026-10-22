package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/session"
)

// SessionAuthenticator menyediakan operasi keamanan sesi untuk HTTP.
type SessionAuthenticator interface {
	Authenticate(ctx context.Context, rawToken string) (session.Authentication, error)
	ValidateCSRF(authentication session.Authentication, rawToken string) error
}

type sessionAuthenticationContextKey struct{}

func withSessionAuthentication(ctx context.Context, authentication session.Authentication) context.Context {
	ctx = context.WithValue(ctx, sessionAuthenticationContextKey{}, authentication)
	return auth.WithPrincipal(ctx, authentication.Principal)
}

func sessionAuthenticationFromContext(ctx context.Context) (session.Authentication, bool) {
	authentication, ok := ctx.Value(sessionAuthenticationContextKey{}).(session.Authentication)
	return authentication, ok
}

// AuthMiddleware memusatkan autentikasi cookie dan perlindungan CSRF.
type AuthMiddleware struct {
	sessions SessionAuthenticator
	cookie   session.CookieConfig
	logger   *slog.Logger
}

func NewAuthMiddleware(
	sessions SessionAuthenticator,
	cookie session.CookieConfig,
	logger *slog.Logger,
) *AuthMiddleware {
	return &AuthMiddleware{sessions: sessions, cookie: cookie, logger: logger}
}

// Authenticate menyisipkan principal dari cookie sesi yang valid.
func (m *AuthMiddleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authentication, present, err := m.authenticate(w, r)
		if err != nil {
			m.internalError(w, r, "authenticate optional session", err)
			return
		}
		if !present {
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r.WithContext(withSessionAuthentication(r.Context(), authentication)))
	})
}

func (m *AuthMiddleware) RequireAuthenticated(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, exists := sessionAuthenticationFromContext(r.Context()); exists {
			next.ServeHTTP(w, r)
			return
		}

		authentication, present, err := m.authenticate(w, r)
		if err != nil {
			m.internalError(w, r, "authenticate required session", err)
			return
		}
		if !present {
			writeError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
			return
		}
		next.ServeHTTP(w, r.WithContext(withSessionAuthentication(r.Context(), authentication)))
	})
}

// RequireCSRF memvalidasi token setelah autentikasi berhasil.
func (m *AuthMiddleware) RequireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authentication, ok := sessionAuthenticationFromContext(r.Context())
		if !ok {
			writeError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
			return
		}
		if err := m.sessions.ValidateCSRF(authentication, r.Header.Get("X-CSRF-Token")); err != nil {
			if errors.Is(err, session.ErrInvalidCSRF) {
				writeError(w, r, http.StatusForbidden, "INVALID_CSRF", "csrf validation failed")
				return
			}
			m.internalError(w, r, "validate csrf token", err)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (m *AuthMiddleware) ProtectMutation(next http.Handler) http.Handler {
	return m.RequireAuthenticated(m.RequireCSRF(next))
}

func (m *AuthMiddleware) authenticate(w http.ResponseWriter, r *http.Request) (session.Authentication, bool, error) {
	cookie, err := r.Cookie(m.cookie.Name)
	if errors.Is(err, http.ErrNoCookie) {
		return session.Authentication{}, false, nil
	}
	if err != nil || cookie.Value == "" {
		http.SetCookie(w, m.cookie.ClearCookie())
		return session.Authentication{}, false, nil
	}

	authentication, err := m.sessions.Authenticate(r.Context(), cookie.Value)
	if errors.Is(err, session.ErrUnauthenticated) {
		http.SetCookie(w, m.cookie.ClearCookie())
		return session.Authentication{}, false, nil
	}
	if err != nil {
		return session.Authentication{}, false, err
	}
	if authentication.RotatedSessionToken != "" {
		http.SetCookie(w, m.cookie.NewCookie(
			authentication.RotatedSessionToken,
			authentication.AbsoluteExpiresAt,
		))
	}
	return authentication, true, nil
}

func (m *AuthMiddleware) internalError(w http.ResponseWriter, r *http.Request, operation string, err error) {
	m.logger.ErrorContext(r.Context(), operation,
		"request_id", requestIDFromContext(r.Context()),
		"error", err,
	)
	writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
}
