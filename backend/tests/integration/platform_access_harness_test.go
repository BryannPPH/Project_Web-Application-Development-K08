package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/app"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/database"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/httpapi"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/platformaccess"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/session"
)

const platformTestPassword = "integration-password-123"

// platformHarness menyalakan seluruh stack HTTP (auth + fitur Person 2) di atas
// PostgreSQL platform sungguhan. Fixture diberi prefix unik dan dibersihkan.
type platformHarness struct {
	t        *testing.T
	database *pgxpool.Pool
	handler  http.Handler
	logs     *bytes.Buffer
	prefix   string
	users    map[string]string // username -> id
}

type platformLogin struct {
	cookie    *http.Cookie
	csrfToken string
}

func newPlatformHarness(t *testing.T) *platformHarness {
	t.Helper()
	databaseURL := os.Getenv("TEST_PLATFORM_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_PLATFORM_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect platform database: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping platform database: %v", err)
	}

	harness := &platformHarness{
		t:        t,
		database: pool,
		logs:     &bytes.Buffer{},
		prefix:   "p2it_" + strconv.FormatInt(time.Now().UnixNano(), 36) + "_",
		users:    make(map[string]string),
	}
	t.Cleanup(harness.cleanup)

	logger := slog.New(slog.NewJSONHandler(harness.logs, nil))
	passwords := auth.NewArgon2idHasher()
	sessions, err := session.NewService(
		session.NewPGRepository(pool),
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
	authentication, err := auth.NewService(auth.NewPGUserRepository(pool), passwords, sessions)
	if err != nil {
		t.Fatalf("create auth service: %v", err)
	}
	cipher, err := database.NewCredentialCipher(bytes.Repeat([]byte{0x5a}, 32), 1)
	if err != nil {
		t.Fatalf("create credential cipher: %v", err)
	}
	registrar, _, err := platformaccess.Build(platformaccess.Options{
		CredentialCipher:     cipher,
		TargetConnectTimeout: 3 * time.Second,
		ViewerQueryTimeout:   10 * time.Second,
	}, app.FeatureDependencies{
		Database:       pool,
		Logger:         logger,
		PasswordHasher: passwords,
		SessionRevoker: sessions,
	})
	if err != nil {
		t.Fatalf("build platform access feature: %v", err)
	}
	harness.handler = httpapi.NewHandler(httpapi.Dependencies{
		Logger:                  logger,
		Database:                pool,
		DatabaseHealthTimeout:   time.Second,
		Authentication:          authentication,
		Sessions:                sessions,
		SessionCookie:           session.CookieConfig{Name: "dbmp_session"},
		RegisterProtectedRoutes: registrar,
	})
	return harness
}

func (h *platformHarness) name(suffix string) string {
	return h.prefix + suffix
}

// createUser membuat fixture user langsung di database (bypass API admin).
func (h *platformHarness) createUser(suffix string, role auth.PlatformRole) string {
	h.t.Helper()
	hash, err := auth.NewArgon2idHasher().Hash(platformTestPassword)
	if err != nil {
		h.t.Fatalf("hash fixture password: %v", err)
	}
	username := h.name(suffix)
	var id string
	if err := h.database.QueryRow(context.Background(), `
		INSERT INTO users (username, password_hash, platform_role, is_active)
		VALUES ($1, $2, $3, TRUE) RETURNING id::text`, username, hash, string(role)).Scan(&id); err != nil {
		h.t.Fatalf("create fixture user: %v", err)
	}
	h.users[username] = id
	return id
}

func (h *platformHarness) login(suffix string) platformLogin {
	h.t.Helper()
	username := h.name(suffix)
	recorder := h.do(http.MethodPost, "/api/v1/auth/login",
		map[string]string{"username": username, "password": platformTestPassword}, nil, false)
	if recorder.Code != http.StatusOK {
		h.t.Fatalf("login %s status = %d body = %s", username, recorder.Code, recorder.Body.String())
	}
	var body struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		h.t.Fatalf("decode login: %v", err)
	}
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == "dbmp_session" {
			return platformLogin{cookie: cookie, csrfToken: body.CSRFToken}
		}
	}
	h.t.Fatal("login did not set session cookie")
	return platformLogin{}
}

// do mengirim request. withCSRF=false mensimulasikan mutation tanpa token CSRF.
func (h *platformHarness) do(method, path string, body any, login *platformLogin, withCSRF bool) *httptest.ResponseRecorder {
	h.t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			h.t.Fatalf("encode body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if login != nil {
		request.AddCookie(login.cookie)
		if withCSRF {
			request.Header.Set("X-CSRF-Token", login.csrfToken)
		}
	}
	recorder := httptest.NewRecorder()
	h.handler.ServeHTTP(recorder, request)
	return recorder
}

// call adalah do() dengan CSRF dan pemeriksaan status.
func (h *platformHarness) call(method, path string, body any, login platformLogin, wantStatus int) map[string]any {
	h.t.Helper()
	recorder := h.do(method, path, body, &login, true)
	if recorder.Code != wantStatus {
		h.t.Fatalf("%s %s status = %d, want %d; body = %s", method, path, recorder.Code, wantStatus, recorder.Body.String())
	}
	if recorder.Body.Len() == 0 {
		return nil
	}
	var decoded map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		h.t.Fatalf("decode %s %s: %v; body=%s", method, path, err, recorder.Body.String())
	}
	return decoded
}

func errorCode(recorder *httptest.ResponseRecorder) string {
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(recorder.Body.Bytes(), &body)
	return body.Error.Code
}

func decodeMap(raw []byte) map[string]any {
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil
	}
	return decoded
}

func objectField(body map[string]any, key string) map[string]any {
	value, _ := body[key].(map[string]any)
	return value
}

func (h *platformHarness) cleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	like := strings.ReplaceAll(h.prefix, "_", `\_`) + "%"
	statements := []string{
		`DELETE FROM migration_entries WHERE database_id IN (SELECT id FROM managed_databases WHERE name LIKE $1)`,
		`DELETE FROM groups WHERE name LIKE $1`,
		`DELETE FROM managed_databases WHERE name LIKE $1`,
		`DELETE FROM users WHERE username LIKE $1`,
	}
	for _, statement := range statements {
		if _, err := h.database.Exec(ctx, statement, like); err != nil {
			h.t.Errorf("cleanup %q: %v", statement, err)
		}
	}
}
