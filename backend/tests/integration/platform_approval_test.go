package integration_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
)

func TestApprovalThroughProductionHTTPStack(t *testing.T) {
	settings := loadTargetSettings(t)
	harness := newPlatformHarness(t)

	// ---- Persiapan: admin, 1 database, 3 group -------------------------------
	harness.createUser("ap_admin", auth.PlatformRoleAdmin)
	devID := harness.createUser("ap_dev", auth.PlatformRoleUser)         // leader Tim Dev (view_edit)
	analystID := harness.createUser("ap_analyst", auth.PlatformRoleUser) // leader Analis (view_only)
	otherID := harness.createUser("ap_other", auth.PlatformRoleUser)     // leader Marketing (tanpa grant)
	admin := harness.login("ap_admin")

	created := harness.call(http.MethodPost, "/api/v1/admin/databases", map[string]any{
		"name": harness.name("ap_target"), "host": settings.host, "port": settings.port,
		"databaseName": settings.database, "username": settings.user, "password": settings.password,
		"sslMode": "disable",
	}, admin, http.StatusCreated)
	databaseID, _ := objectField(created, "database")["id"].(string)

	newGroup := func(suffix string) string {
		body := harness.call(http.MethodPost, "/api/v1/admin/groups", map[string]any{"name": harness.name(suffix)}, admin, http.StatusCreated)
		id, _ := objectField(body, "group")["id"].(string)
		return id
	}
	devGroup, analystGroup, marketingGroup := newGroup("ap_dev_group"), newGroup("ap_analyst_group"), newGroup("ap_marketing_group")
	addMember := func(group, user string) {
		harness.call(http.MethodPost, "/api/v1/admin/groups/"+group+"/members/"+user, nil, admin, http.StatusCreated)
	}
	addMember(devGroup, devID)
	addMember(devGroup, otherID) // other bisa MELIHAT database lewat Tim Dev, tapi bukan leader-nya
	addMember(analystGroup, analystID)
	addMember(marketingGroup, otherID)
	harness.call(http.MethodPut, "/api/v1/admin/groups/"+devGroup+"/databases/"+databaseID,
		map[string]any{"accessLevel": "view_edit"}, admin, http.StatusOK)
	harness.call(http.MethodPut, "/api/v1/admin/groups/"+analystGroup+"/databases/"+databaseID,
		map[string]any{"accessLevel": "view_only"}, admin, http.StatusOK)

	// ---- 1. Leader harus anggota group ---------------------------------------
	if response := harness.do(http.MethodPut, "/api/v1/admin/groups/"+analystGroup+"/leader",
		map[string]any{"userId": devID}, &admin, true); response.Code != http.StatusBadRequest || errorCode(response) != "LEADER_NOT_MEMBER" {
		t.Fatalf("non-member leader = %d %s", response.Code, response.Body.String())
	}
	leaderBody := harness.call(http.MethodPut, "/api/v1/admin/groups/"+devGroup+"/leader", map[string]any{"userId": devID}, admin, http.StatusOK)
	if objectField(leaderBody, "group")["leaderUserId"] != devID {
		t.Fatalf("set leader response = %v", leaderBody)
	}
	harness.call(http.MethodPut, "/api/v1/admin/groups/"+analystGroup+"/leader", map[string]any{"userId": analystID}, admin, http.StatusOK)
	harness.call(http.MethodPut, "/api/v1/admin/groups/"+marketingGroup+"/leader", map[string]any{"userId": otherID}, admin, http.StatusOK)

	dev, analyst, other := harness.login("ap_dev"), harness.login("ap_analyst"), harness.login("ap_other")

	// ---- Migration yang membuat tabel di target -------------------------------
	target, err := pgx.Connect(context.Background(), settings.url())
	if err != nil {
		t.Fatalf("connect target: %v", err)
	}
	t.Cleanup(func() { _ = target.Close(context.Background()) })
	table := pgx.Identifier{"approval_it_" + fmt.Sprint(time.Now().UnixNano())}.Sanitize()
	t.Cleanup(func() { _, _ = target.Exec(context.Background(), "DROP TABLE IF EXISTS "+table) })
	tableExists := func() bool {
		var exists bool
		_ = target.QueryRow(context.Background(), "SELECT to_regclass($1) IS NOT NULL", table).Scan(&exists)
		return exists
	}

	base := "/api/v1/databases/" + databaseID + "/migrations"
	migration := harness.call(http.MethodPost, base, map[string]string{
		"name": "create approval table", "sql": "CREATE TABLE " + table + " (id bigint PRIMARY KEY)",
	}, dev, http.StatusCreated)
	migrationID, _ := migration["id"].(string)
	if migration["revision"] != float64(1) {
		t.Fatalf("new migration revision = %v, want 1", migration["revision"])
	}
	approvals := base + "/" + migrationID + "/approvals"
	decide := func(login platformLogin, revision int, decision string) *httpResponse {
		body := map[string]any{"action": "apply", "revision": revision, "decision": decision}
		if decision == "rejected" {
			body["note"] = "needs an index first"
		}
		return wrap(harness.do(http.MethodPost, approvals, body, &login, true))
	}
	execute := func() *httpResponse {
		return wrap(harness.do(http.MethodPost, base+"/"+migrationID+"/execute", nil, &dev, true))
	}

	// ---- 2. Eksekusi tanpa approval ------------------------------------------
	if response := execute(); response.code != http.StatusConflict || response.errorCode != "APPROVAL_REQUIRED" {
		t.Fatalf("execute without approval = %d %s", response.code, response.body)
	}
	if tableExists() {
		t.Fatal("SQL ran without approval")
	}
	review := objectField(harness.call(http.MethodGet, approvals+"?action=apply", nil, analyst, http.StatusOK), "review")
	if review["status"] != "pending" || review["requiredCount"] != float64(2) || review["canDecide"] != true {
		t.Fatalf("initial review = %v", review)
	}

	// ---- 3. Akses lintas group -----------------------------------------------
	if response := decide(other, 1, "approved"); response.code != http.StatusForbidden || response.errorCode != "APPROVAL_NOT_LEADER" {
		t.Fatalf("leader of a group without access = %d %s", response.code, response.body)
	}

	// ---- 4. Approval sebagian ------------------------------------------------
	if response := decide(dev, 1, "approved"); response.code != http.StatusCreated {
		t.Fatalf("dev approval = %d %s", response.code, response.body)
	}
	if response := execute(); response.code != http.StatusConflict || response.errorCode != "APPROVAL_REQUIRED" {
		t.Fatalf("execute with partial approval = %d %s", response.code, response.body)
	}

	// ---- 6 & 7. Edit setelah approval -> revisi 2, keputusan revisi 1 basi ------
	edited := harness.call(http.MethodPatch, base+"/"+migrationID, map[string]string{
		"name": "create approval table", "sql": "CREATE TABLE " + table + " (id bigint PRIMARY KEY, note text)",
	}, dev, http.StatusOK)
	if edited["revision"] != float64(2) {
		t.Fatalf("edited revision = %v, want 2", edited["revision"])
	}
	if response := decide(analyst, 1, "approved"); response.code != http.StatusConflict || response.errorCode != "APPROVAL_STALE_REVISION" {
		t.Fatalf("approval of an old revision = %d %s", response.code, response.body)
	}
	review = objectField(harness.call(http.MethodGet, approvals, nil, dev, http.StatusOK), "review")
	if review["revision"] != float64(2) || review["approvedCount"] != float64(0) {
		t.Fatalf("review after edit must start over: %v", review)
	}

	// ---- 5. Semua leader setuju (revisi 2) -> eksekusi berhasil -----------------
	if response := decide(dev, 2, "approved"); response.code != http.StatusCreated {
		t.Fatalf("dev approval rev 2 = %d %s", response.code, response.body)
	}
	final := decide(analyst, 2, "approved")
	if final.code != http.StatusCreated || objectField(final.json, "review")["status"] != "approved" {
		t.Fatalf("analyst approval rev 2 = %d %s", final.code, final.body)
	}
	if response := execute(); response.code != http.StatusOK || response.json["status"] != "succeeded" {
		t.Fatalf("approved execution = %d %s", response.code, response.body)
	}
	if !tableExists() {
		t.Fatal("approved migration did not create the target table")
	}

	// ---- 8. Leader dikeluarkan -> leader kosong -> blocked ----------------------
	harness.call(http.MethodDelete, "/api/v1/admin/groups/"+analystGroup+"/members/"+analystID, nil, admin, http.StatusOK)
	groupBody := harness.call(http.MethodGet, "/api/v1/admin/groups/"+analystGroup, nil, admin, http.StatusOK)
	if objectField(groupBody, "group")["leaderUserId"] != nil {
		t.Fatalf("removed member must stop being leader: %v", groupBody)
	}
	next := harness.call(http.MethodPost, base, map[string]string{"name": "after leader removal", "sql": "SELECT 1"}, dev, http.StatusCreated)
	nextID, _ := next["id"].(string)
	blocked := objectField(harness.call(http.MethodGet, base+"/"+nextID+"/approvals", nil, dev, http.StatusOK), "review")
	if blocked["status"] != "blocked" {
		t.Fatalf("review with a leaderless group = %v, want blocked", blocked)
	}
}

// approveApply: semua leader yang diberikan menyetujui revisi TERKINI migration
// (dipakai test migration lama yang fokus ke eksekusi, bukan ke approval).
func (h *platformHarness) approveApply(base, migrationID string, leaders ...platformLogin) {
	h.t.Helper()
	path := base + "/" + migrationID + "/approvals"
	review := objectField(h.call(http.MethodGet, path, nil, leaders[0], http.StatusOK), "review")
	revision, _ := review["revision"].(float64)
	for _, leader := range leaders {
		h.call(http.MethodPost, path, map[string]any{"action": "apply", "revision": int(revision), "decision": "approved"},
			leader, http.StatusCreated)
	}
}

type httpResponse struct {
	code      int
	errorCode string
	body      string
	json      map[string]any
}

// wrap: ringkas hasil httptest supaya assertion di atas pendek.
func wrap(recorder *httptest.ResponseRecorder) *httpResponse {
	return &httpResponse{
		code:      recorder.Code,
		errorCode: errorCode(recorder),
		body:      recorder.Body.String(),
		json:      decodeMap(recorder.Body.Bytes()),
	}
}
