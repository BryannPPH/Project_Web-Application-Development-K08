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
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/migration"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/platformfeature"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/session"
)

const platformTestPassword = "integration-password-123"

type platformHarness struct {
	t                       *testing.T
	database                *pgxpool.Pool
	handler                 http.Handler
	logs                    *bytes.Buffer
	prefix                  string
	users                   map[string]string // username -> id
	worker                  *migration.Worker
	workerCancel            context.CancelFunc
	workerDone              chan error
	workerCleanupRegistered bool
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
	options := platformfeature.Options{
		CredentialCipher:     cipher,
		TargetConnectTimeout: 3 * time.Second,
		ViewerQueryTimeout:   10 * time.Second,
		MigrationWorker: migration.WorkerConfig{
			Concurrency: 1, PollInterval: 50 * time.Millisecond, LeaseDuration: 3 * time.Second,
			HeartbeatInterval: time.Second, RecoveryLockTimeout: 300 * time.Millisecond,
			MaxRetries: 1, RetryBaseDelay: 20 * time.Millisecond, RetryMaxDelay: 20 * time.Millisecond,
		},
	}
	registrar, worker, err := platformfeature.Build(ctx, options, app.FeatureDependencies{
		Database:       pool,
		Logger:         logger,
		PasswordHasher: passwords,
		SessionRevoker: sessions,
	})
	if err != nil {
		t.Fatalf("build platform access feature: %v", err)
	}
	harness.worker = worker
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

func (h *platformHarness) startWorker() {
	h.t.Helper()
	if h.workerCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.workerDone = make(chan error, 1)
	h.workerCancel = cancel
	go func() { h.workerDone <- h.worker.Run(ctx) }()
	if !h.workerCleanupRegistered {
		h.workerCleanupRegistered = true
		h.t.Cleanup(h.stopWorker)
	}
}

func (h *platformHarness) stopWorker() {
	h.t.Helper()
	if h.workerCancel == nil {
		return
	}
	h.workerCancel()
	select {
	case err := <-h.workerDone:
		if err != nil {
			h.t.Errorf("stop migration worker: %v", err)
		}
	case <-time.After(5 * time.Second):
		h.t.Error("migration worker did not stop")
	}
	h.workerCancel = nil
	h.workerDone = nil
}

func (h *platformHarness) waitForJob(path string, login platformLogin) map[string]any {
	h.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		job := h.call(http.MethodGet, path, nil, login, http.StatusOK)
		status, _ := job["status"].(string)
		switch status {
		case "succeeded", "failed", "partial", "partially_reverted", "needs_reconciliation":
			return job
		}
		time.Sleep(25 * time.Millisecond)
	}
	h.t.Fatalf("migration job did not reach a terminal state: %s", path)
	return nil
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
	return h.doWithHeaders(method, path, body, login, withCSRF, nil)
}

func (h *platformHarness) doWithHeaders(method, path string, body any, login *platformLogin, withCSRF bool, headers map[string]string) *httptest.ResponseRecorder {
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
	for name, value := range headers {
		request.Header.Set(name, value)
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
	tx, err := h.database.Begin(ctx)
	if err != nil {
		h.t.Errorf("begin fixture cleanup: %v", err)
		return
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	for _, statement := range []string{
		`ALTER TABLE migration_execution_events DISABLE TRIGGER migration_execution_events_append_only`,
		`ALTER TABLE migration_execution_attempts DISABLE TRIGGER migration_execution_attempts_append_only`,
	} {
		if _, err := tx.Exec(ctx, statement); err != nil {
			h.t.Errorf("cleanup trigger setup %q: %v", statement, err)
			return
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM migration_execution_events WHERE execution_id IN (
			SELECT a.id FROM migration_execution_attempts a
			JOIN migration_entries m ON m.id=a.migration_id
			JOIN managed_databases d ON d.id=m.database_id WHERE d.name LIKE $1)`, like); err != nil {
		h.t.Errorf("cleanup migration execution events: %v", err)
		return
	}
	if _, err := tx.Exec(ctx, `DELETE FROM migration_jobs
		WHERE database_id IN (SELECT id FROM managed_databases WHERE name LIKE $1)`, like); err != nil {
		h.t.Errorf("cleanup migration jobs: %v", err)
		return
	}
	statements := []string{
		`DELETE FROM migration_entries WHERE database_id IN (SELECT id FROM managed_databases WHERE name LIKE $1)`,
		`DELETE FROM groups WHERE name LIKE $1`,
		`DELETE FROM managed_databases WHERE name LIKE $1`,
		`DELETE FROM users WHERE username LIKE $1`,
	}
	for {
		command, err := tx.Exec(ctx, `DELETE FROM migration_execution_attempts a
			WHERE a.database_id IN (SELECT id FROM managed_databases WHERE name LIKE $1)
			AND NOT EXISTS (SELECT 1 FROM migration_execution_attempts child WHERE child.related_execution_id=a.id)`, like)
		if err != nil {
			h.t.Errorf("cleanup migration execution attempts: %v", err)
			return
		}
		if command.RowsAffected() == 0 {
			break
		}
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement, like); err != nil {
			h.t.Errorf("cleanup %q: %v", statement, err)
			return
		}
	}
	for _, statement := range []string{
		`ALTER TABLE migration_execution_events ENABLE TRIGGER migration_execution_events_append_only`,
		`ALTER TABLE migration_execution_attempts ENABLE TRIGGER migration_execution_attempts_append_only`,
	} {
		if _, err := tx.Exec(ctx, statement); err != nil {
			h.t.Errorf("cleanup trigger restore %q: %v", statement, err)
			return
		}
	}
	if err := tx.Commit(ctx); err != nil {
		h.t.Errorf("commit fixture cleanup: %v", err)
	}
}
