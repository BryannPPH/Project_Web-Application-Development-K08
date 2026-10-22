package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/session"
)

const maxAuthRequestBytes = 16 << 10

type LoginService interface {
	Login(ctx context.Context, username, password string) (auth.LoginResult, error)
}

type AuthSessionService interface {
	SessionAuthenticator
	RotateCSRF(ctx context.Context, sessionID string) (string, error)
	Revoke(ctx context.Context, sessionID string) error
}

type authHandler struct {
	authentication LoginService
	sessions       AuthSessionService
	middleware     *AuthMiddleware
	cookie         session.CookieConfig
	loginLimiter   LoginAttemptLimiter
	logger         *slog.Logger
}

func registerAuthRoutes(
	mux *http.ServeMux,
	authentication LoginService,
	sessions AuthSessionService,
	middleware *AuthMiddleware,
	cookie session.CookieConfig,
	loginLimiter LoginAttemptLimiter,
	logger *slog.Logger,
) {
	handler := &authHandler{
		authentication: authentication,
		sessions:       sessions,
		middleware:     middleware,
		cookie:         cookie,
		loginLimiter:   loginLimiter,
		logger:         logger,
	}
	mux.Handle("POST /api/v1/auth/login", middleware.RequireBrowserOrigin(http.HandlerFunc(handler.login)))
	mux.Handle("POST /api/v1/auth/logout", middleware.RequireBrowserOrigin(http.HandlerFunc(handler.logout)))
	mux.Handle("GET /api/v1/auth/me", middleware.RequireAuthenticated(http.HandlerFunc(handler.me)))
	mux.Handle("GET /api/v1/auth/csrf", middleware.RequireAuthenticated(http.HandlerFunc(handler.csrf)))
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	User              auth.Principal `json:"user"`
	CSRFToken         string         `json:"csrfToken"`
	AbsoluteExpiresAt string         `json:"absoluteExpiresAt"`
}

func (h *authHandler) login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var request loginRequest
	if err := decodeJSONBody(w, r, &request); err != nil {
		writeError(w, r, http.StatusBadRequest, "INVALID_BODY", "invalid request body")
		return
	}
	if allowed, retryAfter := h.loginLimiter.Allow(request.Username, clientIdentifier(r)); !allowed {
		seconds := int64((retryAfter + time.Second - 1) / time.Second)
		if seconds < 1 {
			seconds = 1
		}
		w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
		writeError(w, r, http.StatusTooManyRequests, "LOGIN_RATE_LIMITED", "too many login attempts")
		return
	}

	result, err := h.authentication.Login(r.Context(), request.Username, request.Password)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		writeError(w, r, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid username or password")
		return
	}
	if err != nil {
		h.internalError(w, r, "login failed", err)
		return
	}

	http.SetCookie(w, h.cookie.NewCookie(result.Session.SessionToken, result.Session.AbsoluteExpiresAt))
	writeJSON(w, http.StatusOK, loginResponse{
		User:              result.Principal,
		CSRFToken:         result.Session.CSRFToken,
		AbsoluteExpiresAt: result.Session.AbsoluteExpiresAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	})
}

func clientIdentifier(r *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	if remoteAddress := strings.TrimSpace(r.RemoteAddr); remoteAddress != "" {
		return remoteAddress
	}
	return "unknown"
}

func (h *authHandler) me(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": principal})
}

func (h *authHandler) csrf(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	authentication, ok := sessionAuthenticationFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	token, err := h.sessions.RotateCSRF(r.Context(), authentication.SessionID)
	if errors.Is(err, session.ErrUnauthenticated) {
		http.SetCookie(w, h.cookie.ClearCookie())
		writeError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	if err != nil {
		h.internalError(w, r, "rotate csrf token", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"csrfToken": token})
}

// logout tetap aman saat dipanggil berulang kali.
func (h *authHandler) logout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	cookie, err := r.Cookie(h.cookie.Name)
	if errors.Is(err, http.ErrNoCookie) || (err == nil && cookie.Value == "") {
		http.SetCookie(w, h.cookie.ClearCookie())
		writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
		return
	}
	if err != nil {
		http.SetCookie(w, h.cookie.ClearCookie())
		writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
		return
	}

	authentication, err := h.sessions.Authenticate(r.Context(), cookie.Value)
	if errors.Is(err, session.ErrUnauthenticated) {
		http.SetCookie(w, h.cookie.ClearCookie())
		writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
		return
	}
	if err != nil {
		h.internalError(w, r, "authenticate logout session", err)
		return
	}
	// Pertahankan token hasil rotasi meskipun validasi CSRF gagal.
	if authentication.RotatedSessionToken != "" {
		http.SetCookie(w, h.cookie.NewCookie(
			authentication.RotatedSessionToken,
			authentication.AbsoluteExpiresAt,
		))
	}
	if err := h.sessions.ValidateCSRF(authentication, r.Header.Get("X-CSRF-Token")); err != nil {
		if errors.Is(err, session.ErrInvalidCSRF) {
			writeError(w, r, http.StatusForbidden, "INVALID_CSRF", "csrf validation failed")
			return
		}
		h.internalError(w, r, "validate logout csrf token", err)
		return
	}
	if err := h.sessions.Revoke(r.Context(), authentication.SessionID); err != nil {
		h.internalError(w, r, "revoke logout session", err)
		return
	}

	http.SetCookie(w, h.cookie.ClearCookie())
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (h *authHandler) internalError(w http.ResponseWriter, r *http.Request, operation string, err error) {
	h.logger.ErrorContext(r.Context(), operation,
		"request_id", requestIDFromContext(r.Context()),
		"error", err,
	)
	writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}
