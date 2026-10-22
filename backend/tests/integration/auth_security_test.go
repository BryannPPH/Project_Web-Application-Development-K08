package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
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

type authSecurityHarness struct {
	database         *pgxpool.Pool
	handler          http.Handler
	logs             *bytes.Buffer
	username         string
	inactiveUsername string
	password         string
	secrets          []string
}

type authLogin struct {
	cookie    *http.Cookie
	csrfToken string
	sessionID string
}

type apiErrorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func TestAuthSecurityScenarios(t *testing.T) {
	harness := newAuthSecurityHarness(t)

	t.Run("credential errors are indistinguishable", func(t *testing.T) {
		attempts := []struct {
			username string
			password string
		}{
			{username: "missing_" + harness.username, password: harness.password},
			{username: harness.username, password: "wrong-password"},
			{username: harness.inactiveUsername, password: harness.password},
		}

		var expected apiErrorResponse
		for index, attempt := range attempts {
			recorder := harness.request(http.MethodPost, "/api/v1/auth/login", map[string]string{
				"username": attempt.username,
				"password": attempt.password,
			}, nil, "")
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("attempt %d status = %d, want 401", index, recorder.Code)
			}
			var response apiErrorResponse
			if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
				t.Fatalf("decode attempt %d response: %v", index, err)
			}
			if index == 0 {
				expected = response
				continue
			}
			if response.Error.Code != expected.Error.Code || response.Error.Message != expected.Error.Message {
				t.Fatalf("attempt %d response = %+v, want code/message %+v", index, response, expected)
			}
		}
	})

	t.Run("missing and invalid sessions are rejected", func(t *testing.T) {
		missing := harness.request(http.MethodGet, "/api/v1/auth/me", nil, nil, "")
		if missing.Code != http.StatusUnauthorized {
			t.Fatalf("missing session status = %d, want 401", missing.Code)
		}

		invalidCookie := &http.Cookie{Name: "dbmp_session", Value: "invalid-session-token"}
		invalid := harness.request(http.MethodGet, "/api/v1/auth/me?token=query-secret", nil, invalidCookie, "")
		if invalid.Code != http.StatusUnauthorized {
			t.Fatalf("invalid session status = %d, want 401", invalid.Code)
		}
	})

	t.Run("idle and absolute expiration are enforced", func(t *testing.T) {
		idleSession := harness.login(t)
		if _, err := harness.database.Exec(
			context.Background(),
			"UPDATE sessions SET last_activity_at = NOW() - INTERVAL '31 minutes' WHERE id = $1",
			idleSession.sessionID,
		); err != nil {
			t.Fatalf("expire idle session: %v", err)
		}
		idle := harness.request(http.MethodGet, "/api/v1/auth/me", nil, idleSession.cookie, "")
		if idle.Code != http.StatusUnauthorized {
			t.Fatalf("idle session status = %d, want 401", idle.Code)
		}

		absoluteSession := harness.login(t)
		if _, err := harness.database.Exec(
			context.Background(),
			`UPDATE sessions
			 SET absolute_expires_at = created_at + INTERVAL '1 microsecond'
			 WHERE id = $1`,
			absoluteSession.sessionID,
		); err != nil {
			t.Fatalf("expire absolute session: %v", err)
		}
		absolute := harness.request(http.MethodGet, "/api/v1/auth/me", nil, absoluteSession.cookie, "")
		if absolute.Code != http.StatusUnauthorized {
			t.Fatalf("absolute session status = %d, want 401", absolute.Code)
		}
	})

	t.Run("authenticated mutation requires csrf", func(t *testing.T) {
		login := harness.login(t)
		withoutCSRF := harness.request(http.MethodPost, "/api/v1/auth/logout", nil, login.cookie, "")
		if withoutCSRF.Code != http.StatusForbidden {
			t.Fatalf("logout without csrf status = %d, want 403", withoutCSRF.Code)
		}

		var revoked bool
		if err := harness.database.QueryRow(
			context.Background(),
			"SELECT revoked_at IS NOT NULL FROM sessions WHERE id = $1",
			login.sessionID,
		).Scan(&revoked); err != nil {
			t.Fatalf("read session revocation: %v", err)
		}
		if revoked {
			t.Fatal("request without csrf revoked the session")
		}

		withCSRF := harness.request(http.MethodPost, "/api/v1/auth/logout", nil, login.cookie, login.csrfToken)
		if withCSRF.Code != http.StatusOK {
			t.Fatalf("logout with csrf status = %d, want 200", withCSRF.Code)
		}
	})

	logs := harness.logs.String()
	secrets := append([]string{
		harness.password,
		"wrong-password",
		"invalid-session-token",
		"query-secret",
	}, harness.secrets...)
	for _, secret := range secrets {
		if strings.Contains(logs, secret) {
			t.Fatalf("access log exposed secret %q", secret)
		}
	}
}

func newAuthSecurityHarness(t *testing.T) *authSecurityHarness {
	t.Helper()

	databaseURL := os.Getenv("TEST_PLATFORM_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_PLATFORM_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	database, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(database.Close)
	if err := database.Ping(ctx); err != nil {
		t.Fatalf("ping test database: %v", err)
	}

	suffix := time.Now().UTC().Format("20060102150405.000000000")
	username := "auth_security_active_" + suffix
	inactiveUsername := "auth_security_inactive_" + suffix
	password := "security-password-123"
	passwordHash, err := auth.NewArgon2idHasher().Hash(password)
	if err != nil {
		t.Fatalf("hash fixture password: %v", err)
	}

	var activeUserID string
	if err := database.QueryRow(ctx, `
		INSERT INTO users (username, password_hash, platform_role, is_active)
		VALUES ($1, $2, 'user', TRUE)
		RETURNING id::text`, username, passwordHash).Scan(&activeUserID); err != nil {
		t.Fatalf("create active fixture user: %v", err)
	}
	var inactiveUserID string
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := database.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", activeUserID); err != nil {
			t.Errorf("delete active fixture user: %v", err)
		}
		if inactiveUserID != "" {
			if _, err := database.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", inactiveUserID); err != nil {
				t.Errorf("delete inactive fixture user: %v", err)
			}
		}
	})
	if err := database.QueryRow(ctx, `
		INSERT INTO users (username, password_hash, platform_role, is_active)
		VALUES ($1, $2, 'user', FALSE)
		RETURNING id::text`, inactiveUsername, passwordHash).Scan(&inactiveUserID); err != nil {
		t.Fatalf("create inactive fixture user: %v", err)
	}

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
		auth.NewArgon2idHasher(),
		sessionService,
	)
	if err != nil {
		t.Fatalf("create authentication service: %v", err)
	}

	logs := &bytes.Buffer{}
	handler := httpapi.NewHandler(httpapi.Dependencies{
		Logger:                slog.New(slog.NewTextHandler(logs, nil)),
		Database:              database,
		DatabaseHealthTimeout: time.Second,
		Authentication:        authenticationService,
		Sessions:              sessionService,
		SessionCookie:         session.CookieConfig{Name: "dbmp_session"},
	})

	return &authSecurityHarness{
		database:         database,
		handler:          handler,
		logs:             logs,
		username:         username,
		inactiveUsername: inactiveUsername,
		password:         password,
	}
}

func (h *authSecurityHarness) login(t *testing.T) authLogin {
	t.Helper()

	recorder := h.request(http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": h.username,
		"password": h.password,
	}, nil, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("login status = %d; body=%s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.NewDecoder(bytes.NewReader(recorder.Body.Bytes())).Decode(&response); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("login cookies = %+v, want one cookie", cookies)
	}
	cookie := cookies[0]
	if strings.Contains(recorder.Body.String(), h.password) || strings.Contains(recorder.Body.String(), cookie.Value) {
		t.Fatal("login response exposed a password or session token")
	}

	tokenHash := sha256.Sum256([]byte(cookie.Value))
	var sessionID string
	if err := h.database.QueryRow(
		context.Background(),
		"SELECT id::text FROM sessions WHERE current_token_hash = $1",
		tokenHash[:],
	).Scan(&sessionID); err != nil {
		t.Fatalf("find login session: %v", err)
	}
	h.secrets = append(h.secrets, cookie.Value, response.CSRFToken)

	return authLogin{
		cookie:    cookie,
		csrfToken: response.CSRFToken,
		sessionID: sessionID,
	}
}

func (h *authSecurityHarness) request(
	method string,
	path string,
	body any,
	cookie *http.Cookie,
	csrfToken string,
) *httptest.ResponseRecorder {
	var requestBody bytes.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			panic(fmt.Sprintf("encode test request: %v", err))
		}
		requestBody = *bytes.NewReader(encoded)
	}

	request := httptest.NewRequest(method, path, &requestBody)
	request.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		request.AddCookie(cookie)
	}
	if csrfToken != "" {
		request.Header.Set("X-CSRF-Token", csrfToken)
	}
	recorder := httptest.NewRecorder()
	h.handler.ServeHTTP(recorder, request)
	return recorder
}
