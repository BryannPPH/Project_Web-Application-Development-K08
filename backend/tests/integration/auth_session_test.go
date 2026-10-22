package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/httpapi"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/session"
)

// TestAuthSessionLifecycle menguji alur autentikasi dengan PostgreSQL asli.
func TestAuthSessionLifecycle(t *testing.T) {
	databaseURL := os.Getenv("TEST_PLATFORM_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_PLATFORM_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	database, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(database.Close)
	if err := database.Ping(ctx); err != nil {
		t.Fatalf("ping test database: %v", err)
	}

	username := "auth_integration_" + time.Now().UTC().Format("20060102150405.000000000")
	password := "integration-password-123"
	passwords := auth.NewArgon2idHasher()
	passwordHash, err := passwords.Hash(password)
	if err != nil {
		t.Fatalf("hash fixture password: %v", err)
	}
	var userID string
	if err := database.QueryRow(ctx, `
		INSERT INTO users (username, password_hash, platform_role, is_active)
		VALUES ($1, $2, 'user', TRUE)
		RETURNING id::text`, username, passwordHash).Scan(&userID); err != nil {
		t.Fatalf("create fixture user: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := database.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", userID); err != nil {
			t.Errorf("delete fixture user: %v", err)
		}
	})

	sessionService, err := session.NewService(
		session.NewPGRepository(database),
		session.NewRandomTokenSource(),
		session.RealClock{},
		session.Config{
			IdleTimeout:        30 * time.Minute,
			AbsoluteTimeout:    12 * time.Hour,
			RotationInterval:   15 * time.Minute,
			PreviousTokenGrace: 30 * time.Second,
		},
	)
	if err != nil {
		t.Fatalf("create session service: %v", err)
	}
	authenticationService, err := auth.NewService(
		auth.NewPGUserRepository(database),
		passwords,
		sessionService,
	)
	if err != nil {
		t.Fatalf("create authentication service: %v", err)
	}
	handler := httpapi.NewHandler(httpapi.Dependencies{
		Logger:                slog.New(slog.NewTextHandler(io.Discard, nil)),
		Database:              database,
		DatabaseHealthTimeout: time.Second,
		Authentication:        authenticationService,
		Sessions:              sessionService,
		SessionCookie:         session.CookieConfig{Name: "dbmp_session"},
	})

	loginRecorder := httptest.NewRecorder()
	loginRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/login",
		strings.NewReader(`{"username":"`+username+`","password":"`+password+`"}`),
	)
	handler.ServeHTTP(loginRecorder, loginRequest)
	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("login status = %d; body=%s", loginRecorder.Code, loginRecorder.Body.String())
	}
	var loginBody struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.NewDecoder(loginRecorder.Body).Decode(&loginBody); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if loginBody.CSRFToken == "" {
		t.Fatal("login response csrf token is empty")
	}
	cookies := loginRecorder.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "dbmp_session" {
		t.Fatalf("login cookies = %+v", cookies)
	}
	sessionCookie := cookies[0]

	var storedHash []byte
	var sessionID string
	var absoluteExpiresAt time.Time
	if err := database.QueryRow(ctx, `
		SELECT id::text, current_token_hash, absolute_expires_at
		FROM sessions
		WHERE user_id = $1 AND revoked_at IS NULL`, userID).Scan(&sessionID, &storedHash, &absoluteExpiresAt); err != nil {
		t.Fatalf("read stored session: %v", err)
	}
	expectedHash := sha256.Sum256([]byte(sessionCookie.Value))
	if !bytes.Equal(storedHash, expectedHash[:]) {
		t.Fatal("database did not store the SHA-256 session token hash")
	}
	if bytes.Contains(storedHash, []byte(sessionCookie.Value)) {
		t.Fatal("database stored the plaintext session identifier")
	}
	// Picu rotasi tanpa menunggu interval sebenarnya.
	if _, err := database.Exec(ctx, "UPDATE sessions SET rotated_at = $2 WHERE id = $1", sessionID, time.Now().Add(-16*time.Minute)); err != nil {
		t.Fatalf("age fixture session rotation: %v", err)
	}

	meRecorder := httptest.NewRecorder()
	meRequest := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	meRequest.AddCookie(sessionCookie)
	handler.ServeHTTP(meRecorder, meRequest)
	if meRecorder.Code != http.StatusOK || strings.Contains(meRecorder.Body.String(), sessionID) {
		t.Fatalf("me status=%d body=%s", meRecorder.Code, meRecorder.Body.String())
	}
	rotatedCookies := meRecorder.Result().Cookies()
	if len(rotatedCookies) != 1 || rotatedCookies[0].Value == sessionCookie.Value {
		t.Fatalf("rotation cookie = %+v, want a new opaque token", rotatedCookies)
	}
	rotatedCookie := rotatedCookies[0]
	var expiryAfterRotation time.Time
	if err := database.QueryRow(ctx, "SELECT absolute_expires_at FROM sessions WHERE id = $1", sessionID).Scan(&expiryAfterRotation); err != nil {
		t.Fatalf("read expiry after rotation: %v", err)
	}
	if !expiryAfterRotation.Equal(absoluteExpiresAt) {
		t.Fatalf("rotation extended absolute expiry from %v to %v", absoluteExpiresAt, expiryAfterRotation)
	}

	graceRecorder := httptest.NewRecorder()
	graceRequest := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	graceRequest.AddCookie(sessionCookie)
	handler.ServeHTTP(graceRecorder, graceRequest)
	if graceRecorder.Code != http.StatusOK {
		t.Fatalf("previous token during grace status = %d, want 200", graceRecorder.Code)
	}
	var expiryAfterActivity time.Time
	if err := database.QueryRow(ctx, "SELECT absolute_expires_at FROM sessions WHERE id = $1", sessionID).Scan(&expiryAfterActivity); err != nil {
		t.Fatalf("read expiry after activity: %v", err)
	}
	if !expiryAfterActivity.Equal(absoluteExpiresAt) {
		t.Fatalf("activity extended absolute expiry from %v to %v", absoluteExpiresAt, expiryAfterActivity)
	}
	if _, err := database.Exec(ctx, "UPDATE sessions SET previous_token_valid_until = $2 WHERE id = $1", sessionID, time.Now().Add(-time.Second)); err != nil {
		t.Fatalf("expire previous token grace: %v", err)
	}
	expiredGraceRecorder := httptest.NewRecorder()
	expiredGraceRequest := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	expiredGraceRequest.AddCookie(sessionCookie)
	handler.ServeHTTP(expiredGraceRecorder, expiredGraceRequest)
	if expiredGraceRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("previous token after grace status = %d, want 401", expiredGraceRecorder.Code)
	}

	logoutRecorder := httptest.NewRecorder()
	logoutRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logoutRequest.AddCookie(rotatedCookie)
	logoutRequest.Header.Set("X-CSRF-Token", loginBody.CSRFToken)
	handler.ServeHTTP(logoutRecorder, logoutRequest)
	if logoutRecorder.Code != http.StatusOK {
		t.Fatalf("logout status = %d; body=%s", logoutRecorder.Code, logoutRecorder.Body.String())
	}
	var revoked bool
	if err := database.QueryRow(ctx, "SELECT revoked_at IS NOT NULL FROM sessions WHERE id = $1", sessionID).Scan(&revoked); err != nil {
		t.Fatalf("read revoked session: %v", err)
	}
	if !revoked {
		t.Fatal("logout did not revoke the server-side session")
	}

	afterLogoutRecorder := httptest.NewRecorder()
	afterLogoutRequest := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	afterLogoutRequest.AddCookie(rotatedCookie)
	handler.ServeHTTP(afterLogoutRecorder, afterLogoutRequest)
	if afterLogoutRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout status = %d, want 401", afterLogoutRecorder.Code)
	}

	revokedLoginRecorder := httptest.NewRecorder()
	revokedLoginRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/login",
		strings.NewReader(`{"username":"`+username+`","password":"`+password+`"}`),
	)
	handler.ServeHTTP(revokedLoginRecorder, revokedLoginRequest)
	if revokedLoginRecorder.Code != http.StatusOK {
		t.Fatalf("revoke-all login status = %d; body=%s", revokedLoginRecorder.Code, revokedLoginRecorder.Body.String())
	}
	revokedCookies := revokedLoginRecorder.Result().Cookies()
	if len(revokedCookies) != 1 {
		t.Fatalf("revoke-all login cookies = %+v", revokedCookies)
	}
	if err := sessionService.RevokeAllForUser(ctx, userID); err != nil {
		t.Fatalf("revoke all user sessions: %v", err)
	}
	revokedRecorder := httptest.NewRecorder()
	revokedRequest := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	revokedRequest.AddCookie(revokedCookies[0])
	handler.ServeHTTP(revokedRecorder, revokedRequest)
	if revokedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("me after revoke-all status = %d, want 401", revokedRecorder.Code)
	}
}
