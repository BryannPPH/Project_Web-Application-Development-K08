package integration_test

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
)

const placeholderID = "11111111-1111-4111-8111-111111111111"

// adminFamilyRoutes mencakup setiap keluarga route admin.
var adminFamilyRoutes = []struct {
	method string
	path   string
	body   any
}{
	{http.MethodGet, "/api/v1/admin/users", nil},
	{http.MethodPost, "/api/v1/admin/users", map[string]any{"username": "nobody", "password": "password-123"}},
	{http.MethodGet, "/api/v1/admin/users/" + placeholderID, nil},
	{http.MethodPatch, "/api/v1/admin/users/" + placeholderID, map[string]any{"isActive": false}},
	{http.MethodPost, "/api/v1/admin/users/" + placeholderID + "/reset-password", map[string]any{"password": "password-123"}},
	{http.MethodGet, "/api/v1/admin/groups", nil},
	{http.MethodPost, "/api/v1/admin/groups", map[string]any{"name": "nobody"}},
	{http.MethodPatch, "/api/v1/admin/groups/" + placeholderID, map[string]any{"name": "x"}},
	{http.MethodDelete, "/api/v1/admin/groups/" + placeholderID, nil},
	{http.MethodPost, "/api/v1/admin/groups/" + placeholderID + "/members/" + placeholderID, nil},
	{http.MethodDelete, "/api/v1/admin/groups/" + placeholderID + "/members/" + placeholderID, nil},
	{http.MethodPut, "/api/v1/admin/groups/" + placeholderID + "/databases/" + placeholderID, map[string]any{"accessLevel": "view_edit"}},
	{http.MethodDelete, "/api/v1/admin/groups/" + placeholderID + "/databases/" + placeholderID, nil},
	{http.MethodGet, "/api/v1/admin/databases", nil},
	{http.MethodPost, "/api/v1/admin/databases", map[string]any{"name": "x"}},
	{http.MethodPatch, "/api/v1/admin/databases/" + placeholderID, map[string]any{"name": "x"}},
	{http.MethodDelete, "/api/v1/admin/databases/" + placeholderID, nil},
	{http.MethodPost, "/api/v1/admin/databases/" + placeholderID + "/test", nil},
}

func TestAdminRoutesRequireAuthenticationAdminRoleAndCSRF(t *testing.T) {
	harness := newPlatformHarness(t)
	harness.createUser("admin", auth.PlatformRoleAdmin)
	harness.createUser("regular", auth.PlatformRoleUser)
	admin := harness.login("admin")
	regular := harness.login("regular")

	for _, route := range adminFamilyRoutes {
		unauthenticated := harness.do(route.method, route.path, route.body, nil, false)
		if unauthenticated.Code != http.StatusUnauthorized {
			t.Errorf("unauthenticated %s %s = %d, want 401", route.method, route.path, unauthenticated.Code)
		}

		forbidden := harness.do(route.method, route.path, route.body, &regular, true)
		if forbidden.Code != http.StatusForbidden || errorCode(forbidden) != "FORBIDDEN" {
			t.Errorf("regular user %s %s = %d %s, want 403 FORBIDDEN", route.method, route.path, forbidden.Code, errorCode(forbidden))
		}

		if route.method != http.MethodGet {
			missingCSRF := harness.do(route.method, route.path, route.body, &admin, false)
			if missingCSRF.Code != http.StatusForbidden || errorCode(missingCSRF) != "INVALID_CSRF" {
				t.Errorf("admin without CSRF %s %s = %d %s, want 403 INVALID_CSRF", route.method, route.path, missingCSRF.Code, errorCode(missingCSRF))
			}
		}
	}
}

func TestAdminManagesUsersGroupsDatabasesAndGrants(t *testing.T) {
	harness := newPlatformHarness(t)
	harness.createUser("admin", auth.PlatformRoleAdmin)
	admin := harness.login("admin")
	ctx := context.Background()

	// --- User ---
	created := harness.call(http.MethodPost, "/api/v1/admin/users", map[string]any{
		"username": harness.name("analyst"), "password": "analyst-password-1", "platformRole": "user",
	}, admin, http.StatusCreated)
	analyst := objectField(created, "user")
	analystID, _ := analyst["id"].(string)
	harness.users[harness.name("analyst")] = analystID
	if analyst["platformRole"] != "user" || analyst["isActive"] != true {
		t.Fatalf("created user = %v", analyst)
	}

	duplicate := harness.do(http.MethodPost, "/api/v1/admin/users", map[string]any{
		"username": strings.ToUpper(harness.name("analyst")), "password": "analyst-password-1",
	}, &admin, true)
	if duplicate.Code != http.StatusConflict || errorCode(duplicate) != "USERNAME_TAKEN" {
		t.Fatalf("duplicate username = %d %s", duplicate.Code, duplicate.Body.String())
	}
	invalid := harness.do(http.MethodPost, "/api/v1/admin/users", map[string]any{
		"username": harness.name("short-pass"), "password": "short",
	}, &admin, true)
	if invalid.Code != http.StatusBadRequest || errorCode(invalid) != "VALIDATION_FAILED" {
		t.Fatalf("invalid password = %d %s", invalid.Code, invalid.Body.String())
	}

	list := harness.do(http.MethodGet, "/api/v1/admin/users", nil, &admin, false)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), harness.name("analyst")) {
		t.Fatalf("list users = %d", list.Code)
	}
	if strings.Contains(list.Body.String(), "argon2id") || strings.Contains(strings.ToLower(list.Body.String()), "password") {
		t.Fatal("user list leaked password hash fields")
	}

	renamed := harness.call(http.MethodPatch, "/api/v1/admin/users/"+analystID,
		map[string]any{"username": harness.name("analyst2")}, admin, http.StatusOK)
	if objectField(renamed, "user")["username"] != harness.name("analyst2") {
		t.Fatalf("rename = %v", renamed)
	}

	// --- Revocation: reset password dan deaktivasi mencabut session lama ---
	analystLogin := harness.loginWithPassword("analyst2", "analyst-password-1")
	harness.call(http.MethodPost, "/api/v1/admin/users/"+analystID+"/reset-password",
		map[string]any{"password": "fresh-password-22"}, admin, http.StatusOK)
	if me := harness.do(http.MethodGet, "/api/v1/auth/me", nil, &analystLogin, false); me.Code != http.StatusUnauthorized {
		t.Fatalf("session after password reset = %d, want 401", me.Code)
	}
	var activeSessions int
	_ = harness.database.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked_at IS NULL`, analystID).Scan(&activeSessions)
	if activeSessions != 0 {
		t.Fatalf("active sessions after reset = %d", activeSessions)
	}

	// --- Group & membership ---
	groupBody := harness.call(http.MethodPost, "/api/v1/admin/groups",
		map[string]any{"name": harness.name("readers"), "description": "Read only analysts"}, admin, http.StatusCreated)
	groupID, _ := objectField(groupBody, "group")["id"].(string)
	if dup := harness.do(http.MethodPost, "/api/v1/admin/groups", map[string]any{"name": harness.name("READERS")}, &admin, true); dup.Code != http.StatusConflict {
		t.Fatalf("duplicate group = %d", dup.Code)
	}
	harness.call(http.MethodPost, "/api/v1/admin/groups/"+groupID+"/members/"+analystID, nil, admin, http.StatusCreated)
	again := harness.call(http.MethodPost, "/api/v1/admin/groups/"+groupID+"/members/"+analystID, nil, admin, http.StatusOK)
	if again["added"] != false {
		t.Fatalf("idempotent add = %v", again)
	}
	members := harness.call(http.MethodGet, "/api/v1/admin/groups/"+groupID+"/members", nil, admin, http.StatusOK)
	if list, _ := members["members"].([]any); len(list) != 1 {
		t.Fatalf("members = %v", members)
	}
	if missing := harness.do(http.MethodPost, "/api/v1/admin/groups/"+groupID+"/members/"+placeholderID, nil, &admin, true); missing.Code != http.StatusNotFound {
		t.Fatalf("add unknown user = %d", missing.Code)
	}

	// --- Managed database ---
	secret := "managed-db-secret-pass"
	dbBody := harness.call(http.MethodPost, "/api/v1/admin/databases", map[string]any{
		"name": harness.name("warehouse"), "host": "127.0.0.1", "port": 1,
		"databaseName": "warehouse", "username": "warehouse_reader", "password": secret, "sslMode": "disable",
	}, admin, http.StatusCreated)
	managed := objectField(dbBody, "database")
	databaseID, _ := managed["id"].(string)
	if managed["hasCredential"] != true {
		t.Fatalf("hasCredential = %v", managed["hasCredential"])
	}
	for _, key := range []string{"password", "credential", "credentialCiphertext"} {
		if _, exists := managed[key]; exists {
			t.Fatalf("database response exposes %q", key)
		}
	}
	var ciphertext []byte
	if err := harness.database.QueryRow(ctx, `SELECT credential_ciphertext FROM managed_databases WHERE id = $1`, databaseID).Scan(&ciphertext); err != nil {
		t.Fatalf("read ciphertext: %v", err)
	}
	if bytes.Contains(ciphertext, []byte(secret)) || len(ciphertext) < 12+16 {
		t.Fatal("credential is not encrypted at rest")
	}
	if dup := harness.do(http.MethodPost, "/api/v1/admin/databases", map[string]any{
		"name": strings.ToUpper(harness.name("warehouse")), "host": "127.0.0.1", "databaseName": "w", "username": "u", "password": "p",
	}, &admin, true); dup.Code != http.StatusConflict || errorCode(dup) != "DATABASE_NAME_TAKEN" {
		t.Fatalf("duplicate database = %d %s", dup.Code, dup.Body.String())
	}
	if bad := harness.do(http.MethodPost, "/api/v1/admin/databases", map[string]any{
		"name": harness.name("bad"), "host": "evil host", "databaseName": "w", "username": "u", "password": "p",
	}, &admin, true); bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid host = %d", bad.Code)
	}

	test := harness.call(http.MethodPost, "/api/v1/admin/databases/"+databaseID+"/test", nil, admin, http.StatusOK)
	if test["success"] != false || test["reason"] == "" {
		t.Fatalf("test unreachable connection = %v", test)
	}

	// --- Grant ---
	grant := harness.call(http.MethodPut, "/api/v1/admin/groups/"+groupID+"/databases/"+databaseID,
		map[string]any{"accessLevel": "view_only"}, admin, http.StatusOK)
	if objectField(grant, "grant")["accessLevel"] != "view_only" {
		t.Fatalf("grant = %v", grant)
	}
	if bad := harness.do(http.MethodPut, "/api/v1/admin/groups/"+groupID+"/databases/"+databaseID,
		map[string]any{"accessLevel": "owner"}, &admin, true); bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid access level = %d", bad.Code)
	}

	// Anggota group melihat database; admin tanpa grant tidak.
	analystLogin = harness.loginWithPassword("analyst2", "fresh-password-22")
	accessible := harness.call(http.MethodGet, "/api/v1/databases", nil, analystLogin, http.StatusOK)
	if list, _ := accessible["databases"].([]any); len(list) != 1 {
		t.Fatalf("accessible databases = %v", accessible)
	}
	if strings.Contains(harness.do(http.MethodGet, "/api/v1/databases", nil, &admin, false).Body.String(), databaseID) {
		t.Fatal("admin without grant can list the managed database")
	}
	if detail := harness.do(http.MethodGet, "/api/v1/databases/"+databaseID, nil, &admin, false); detail.Code != http.StatusNotFound {
		t.Fatalf("admin without grant detail = %d, want 404", detail.Code)
	}

	// Upgrade grant (privilege naik) mencabut session anggota.
	harness.call(http.MethodPut, "/api/v1/admin/groups/"+groupID+"/databases/"+databaseID,
		map[string]any{"accessLevel": "view_edit"}, admin, http.StatusOK)
	if me := harness.do(http.MethodGet, "/api/v1/auth/me", nil, &analystLogin, false); me.Code != http.StatusUnauthorized {
		t.Fatalf("session after privilege elevation = %d, want 401", me.Code)
	}

	// Revoke grant berlaku seketika.
	analystLogin = harness.loginWithPassword("analyst2", "fresh-password-22")
	harness.call(http.MethodDelete, "/api/v1/admin/groups/"+groupID+"/databases/"+databaseID, nil, admin, http.StatusNoContent)
	if detail := harness.do(http.MethodGet, "/api/v1/databases/"+databaseID, nil, &analystLogin, false); detail.Code != http.StatusNotFound {
		t.Fatalf("detail after revoke = %d, want 404", detail.Code)
	}

	// Deaktivasi mencabut session.
	harness.call(http.MethodPatch, "/api/v1/admin/users/"+analystID, map[string]any{"isActive": false}, admin, http.StatusOK)
	if me := harness.do(http.MethodGet, "/api/v1/auth/me", nil, &analystLogin, false); me.Code != http.StatusUnauthorized {
		t.Fatalf("session after deactivation = %d, want 401", me.Code)
	}

	// Database dengan riwayat migration tidak boleh dihapus.
	if _, err := harness.database.Exec(ctx, `
		INSERT INTO migration_entries (database_id, version, name, sql_text, created_by)
		VALUES ($1, 1, 'init', 'SELECT 1', $2)`, databaseID, harness.users[harness.name("admin")]); err != nil {
		t.Fatalf("insert migration history: %v", err)
	}
	if del := harness.do(http.MethodDelete, "/api/v1/admin/databases/"+databaseID, nil, &admin, true); del.Code != http.StatusConflict || errorCode(del) != "DATABASE_HAS_MIGRATIONS" {
		t.Fatalf("delete database with history = %d %s", del.Code, del.Body.String())
	}

	harness.call(http.MethodDelete, "/api/v1/admin/groups/"+groupID, nil, admin, http.StatusNoContent)
	if gone := harness.do(http.MethodGet, "/api/v1/admin/groups/"+groupID, nil, &admin, false); gone.Code != http.StatusNotFound {
		t.Fatalf("deleted group = %d", gone.Code)
	}

	if strings.Contains(harness.logs.String(), secret) {
		t.Fatal("logs contain managed database password")
	}
}

func (h *platformHarness) loginWithPassword(suffix, password string) platformLogin {
	h.t.Helper()
	recorder := h.do(http.MethodPost, "/api/v1/auth/login",
		map[string]string{"username": h.name(suffix), "password": password}, nil, false)
	if recorder.Code != http.StatusOK {
		h.t.Fatalf("login %s = %d %s", suffix, recorder.Code, recorder.Body.String())
	}
	var csrf string
	if body := decodeMap(recorder.Body.Bytes()); body != nil {
		csrf, _ = body["csrfToken"].(string)
	}
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == "dbmp_session" {
			return platformLogin{cookie: cookie, csrfToken: csrf}
		}
	}
	h.t.Fatal("login did not set cookie")
	return platformLogin{}
}
