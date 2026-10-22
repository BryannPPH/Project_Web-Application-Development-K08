package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/session"
)

type stubLoginService struct {
	result auth.LoginResult
	err    error
}

func (s stubLoginService) Login(context.Context, string, string) (auth.LoginResult, error) {
	return s.result, s.err
}

type stubSessionService struct {
	authentication  session.Authentication
	authenticateErr error
	revokedID       string
}

func (s *stubSessionService) Authenticate(context.Context, string) (session.Authentication, error) {
	return s.authentication, s.authenticateErr
}

func (s *stubSessionService) ValidateCSRF(_ session.Authentication, token string) error {
	if token != "valid-csrf" {
		return session.ErrInvalidCSRF
	}
	return nil
}

func (s *stubSessionService) RotateCSRF(context.Context, string) (string, error) {
	return "rotated-csrf", nil
}

func (s *stubSessionService) Revoke(_ context.Context, sessionID string) error {
	s.revokedID = sessionID
	return nil
}

func authTestHandler(login LoginService, sessions AuthSessionService) http.Handler {
	return NewHandler(Dependencies{
		Logger:                discardLogger(),
		Database:              databaseChecker{},
		DatabaseHealthTimeout: time.Second,
		Authentication:        login,
		Sessions:              sessions,
		SessionCookie: session.CookieConfig{
			Name:   "dbmp_session",
			Secure: true,
		},
	})
}

func TestLoginSetsOpaqueCookieWithoutReturningSessionIdentifier(t *testing.T) {
	t.Parallel()

	expiresAt := time.Now().Add(time.Hour).UTC()
	handler := authTestHandler(stubLoginService{result: auth.LoginResult{
		Principal: auth.Principal{
			UserID:       "user-id",
			Username:     "alice",
			PlatformRole: auth.PlatformRoleUser,
		},
		Session: auth.SessionCredentials{
			SessionToken:      "super-secret-session-token",
			CSRFToken:         "csrf-response-token",
			AbsoluteExpiresAt: expiresAt,
		},
	}}, &stubSessionService{})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{
		"username":"alice",
		"password":"correct password"
	}`))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", recorder.Header().Get("Cache-Control"))
	}
	if strings.Contains(recorder.Body.String(), "super-secret-session-token") {
		t.Fatal("response body exposed the session identifier")
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookie count = %d, want 1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != "dbmp_session" || cookie.Value != "super-secret-session-token" {
		t.Fatalf("session cookie = %+v", cookie)
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie is missing security attributes: %+v", cookie)
	}

	var response loginResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.CSRFToken != "csrf-response-token" || response.User.Username != "alice" {
		t.Fatalf("login response = %+v", response)
	}
}

func TestLoginUsesGenericInvalidCredentialResponse(t *testing.T) {
	t.Parallel()

	handler := authTestHandler(stubLoginService{err: auth.ErrInvalidCredentials}, &stubSessionService{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/login",
		strings.NewReader(`{"username":"missing","password":"wrong"}`),
	)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
	var response errorBody
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Error.Code != "INVALID_CREDENTIALS" {
		t.Fatalf("error code = %q, want INVALID_CREDENTIALS", response.Error.Code)
	}
}

func TestMeReturnsPrincipalButNoSessionData(t *testing.T) {
	t.Parallel()

	sessions := &stubSessionService{authentication: session.Authentication{
		SessionID: "internal-session-id",
		Principal: auth.Principal{
			UserID:       "user-id",
			Username:     "alice",
			PlatformRole: auth.PlatformRoleUser,
		},
		AbsoluteExpiresAt: time.Now().Add(time.Hour),
	}}
	handler := authTestHandler(stubLoginService{}, sessions)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	request.AddCookie(&http.Cookie{Name: "dbmp_session", Value: "opaque-token"})
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "internal-session-id") || strings.Contains(recorder.Body.String(), "opaque-token") {
		t.Fatal("me response exposed session data")
	}
}

func TestProtectedMutationRequiresCSRF(t *testing.T) {
	t.Parallel()

	sessions := &stubSessionService{authentication: session.Authentication{
		SessionID:         "session-id",
		Principal:         auth.Principal{UserID: "user-id", PlatformRole: auth.PlatformRoleUser},
		AbsoluteExpiresAt: time.Now().Add(time.Hour),
	}}
	middleware := NewAuthMiddleware(
		sessions,
		session.CookieConfig{Name: "dbmp_session"},
		discardLogger(),
	)
	protected := middleware.ProtectMutation(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, tt := range []struct {
		name       string
		csrf       string
		wantStatus int
	}{
		{name: "missing", wantStatus: http.StatusForbidden},
		{name: "wrong", csrf: "wrong", wantStatus: http.StatusForbidden},
		{name: "valid", csrf: "valid-csrf", wantStatus: http.StatusNoContent},
	} {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/mutation", nil)
			request.AddCookie(&http.Cookie{Name: "dbmp_session", Value: "opaque-token"})
			request.Header.Set("X-CSRF-Token", tt.csrf)
			protected.ServeHTTP(recorder, request)
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}
		})
	}
}

func TestLogoutIsIdempotentButProtectsActiveSessionWithCSRF(t *testing.T) {
	t.Parallel()

	sessions := &stubSessionService{authentication: session.Authentication{
		SessionID:         "session-id",
		Principal:         auth.Principal{UserID: "user-id", PlatformRole: auth.PlatformRoleUser},
		AbsoluteExpiresAt: time.Now().Add(time.Hour),
	}}
	handler := authTestHandler(stubLoginService{}, sessions)

	missingRecorder := httptest.NewRecorder()
	handler.ServeHTTP(missingRecorder, httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil))
	if missingRecorder.Code != http.StatusOK {
		t.Fatalf("logout without cookie status = %d, want 200", missingRecorder.Code)
	}

	forbiddenRecorder := httptest.NewRecorder()
	forbiddenRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	forbiddenRequest.AddCookie(&http.Cookie{Name: "dbmp_session", Value: "opaque-token"})
	handler.ServeHTTP(forbiddenRecorder, forbiddenRequest)
	if forbiddenRecorder.Code != http.StatusForbidden {
		t.Fatalf("logout without csrf status = %d, want 403", forbiddenRecorder.Code)
	}

	successRecorder := httptest.NewRecorder()
	successRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	successRequest.AddCookie(&http.Cookie{Name: "dbmp_session", Value: "opaque-token"})
	successRequest.Header.Set("X-CSRF-Token", "valid-csrf")
	handler.ServeHTTP(successRecorder, successRequest)
	if successRecorder.Code != http.StatusOK || sessions.revokedID != "session-id" {
		t.Fatalf("logout result status=%d revoked=%q", successRecorder.Code, sessions.revokedID)
	}
}

func TestRejectedLogoutPreservesConcurrentSessionRotation(t *testing.T) {
	t.Parallel()

	sessions := &stubSessionService{authentication: session.Authentication{
		SessionID:           "session-id",
		Principal:           auth.Principal{UserID: "user-id", PlatformRole: auth.PlatformRoleUser},
		AbsoluteExpiresAt:   time.Now().Add(time.Hour),
		RotatedSessionToken: "new-session-token",
	}}
	handler := authTestHandler(stubLoginService{}, sessions)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	request.AddCookie(&http.Cookie{Name: "dbmp_session", Value: "old-session-token"})
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Value != "new-session-token" {
		t.Fatalf("rotation cookies = %+v, want new session token", cookies)
	}
}

func TestMeRejectsMissingOrInvalidSession(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		cookie     bool
		sessionErr error
	}{
		{name: "missing cookie"},
		{name: "invalid session", cookie: true, sessionErr: session.ErrUnauthenticated},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := authTestHandler(stubLoginService{}, &stubSessionService{authenticateErr: tt.sessionErr})
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
			if tt.cookie {
				request.AddCookie(&http.Cookie{Name: "dbmp_session", Value: "invalid"})
			}
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", recorder.Code)
			}
		})
	}
}

func TestAuthenticationInfrastructureErrorIsNotTreatedAsAnonymous(t *testing.T) {
	t.Parallel()

	handler := authTestHandler(stubLoginService{}, &stubSessionService{authenticateErr: errors.New("database unavailable")})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	request.AddCookie(&http.Cookie{Name: "dbmp_session", Value: "opaque-token"})
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
}
