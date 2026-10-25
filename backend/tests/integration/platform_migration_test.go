package integration_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
)

func TestMigrationThroughProductionHTTPStack(t *testing.T) {
	settings := loadTargetSettings(t)
	harness := newPlatformHarness(t)

	harness.createUser("admin_migration", auth.PlatformRoleAdmin)
	viewerID := harness.createUser("migration_viewer", auth.PlatformRoleUser)
	editorID := harness.createUser("migration_editor", auth.PlatformRoleUser)
	editorUsername := harness.name("migration_editor")
	harness.createUser("migration_outsider", auth.PlatformRoleUser)
	admin := harness.login("admin_migration")

	createdDatabase := harness.call(http.MethodPost, "/api/v1/admin/databases", map[string]any{
		"name":         harness.name("migration_target"),
		"host":         settings.host,
		"port":         settings.port,
		"databaseName": settings.database,
		"username":     settings.user,
		"password":     settings.password,
		"sslMode":      "disable",
	}, admin, http.StatusCreated)
	databaseID, _ := objectField(createdDatabase, "database")["id"].(string)
	if databaseID == "" {
		t.Fatal("managed database response did not contain an id")
	}

	readers := harness.call(http.MethodPost, "/api/v1/admin/groups", map[string]any{
		"name": harness.name("migration_readers"),
	}, admin, http.StatusCreated)
	writers := harness.call(http.MethodPost, "/api/v1/admin/groups", map[string]any{
		"name": harness.name("migration_writers"),
	}, admin, http.StatusCreated)
	readersID, _ := objectField(readers, "group")["id"].(string)
	writersID, _ := objectField(writers, "group")["id"].(string)
	harness.call(http.MethodPost, "/api/v1/admin/groups/"+readersID+"/members/"+viewerID, nil, admin, http.StatusCreated)
	harness.call(http.MethodPost, "/api/v1/admin/groups/"+writersID+"/members/"+editorID, nil, admin, http.StatusCreated)
	harness.call(http.MethodPut, "/api/v1/admin/groups/"+readersID+"/databases/"+databaseID,
		map[string]any{"accessLevel": "view_only"}, admin, http.StatusOK)
	harness.call(http.MethodPut, "/api/v1/admin/groups/"+writersID+"/databases/"+databaseID,
		map[string]any{"accessLevel": "view_edit"}, admin, http.StatusOK)

	viewer := harness.login("migration_viewer")
	editor := harness.login("migration_editor")
	outsider := harness.login("migration_outsider")
	base := "/api/v1/databases/" + databaseID + "/migrations"

	if response := harness.do(http.MethodGet, base, nil, nil, false); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated history = %d %s", response.Code, response.Body.String())
	}
	for name, login := range map[string]platformLogin{"outsider": outsider, "admin": admin} {
		response := harness.do(http.MethodGet, base, nil, &login, false)
		if response.Code != http.StatusNotFound || errorCode(response) != "DATABASE_NOT_FOUND" {
			t.Fatalf("%s history = %d %s", name, response.Code, response.Body.String())
		}
	}
	if response := harness.do(http.MethodPost, base,
		map[string]string{"name": "forbidden", "sql": "SELECT 1"}, &viewer, true); response.Code != http.StatusForbidden ||
		errorCode(response) != "DATABASE_EDIT_FORBIDDEN" {
		t.Fatalf("view-only create = %d %s", response.Code, response.Body.String())
	}
	if response := harness.do(http.MethodPost, base,
		map[string]string{"name": "missing csrf", "sql": "SELECT 1"}, &editor, false); response.Code != http.StatusForbidden ||
		errorCode(response) != "INVALID_CSRF" {
		t.Fatalf("missing csrf create = %d %s", response.Code, response.Body.String())
	}

	target, err := pgx.Connect(context.Background(), settings.url())
	if err != nil {
		t.Fatalf("connect target database: %v", err)
	}
	t.Cleanup(func() { _ = target.Close(context.Background()) })
	tableName := "migration_http_" + fmt.Sprintf("%d", time.Now().UnixNano())
	quotedTable := pgx.Identifier{tableName}.Sanitize()
	t.Cleanup(func() {
		_, _ = target.Exec(context.Background(), "DROP TABLE IF EXISTS "+quotedTable)
	})

	successSQL := "CREATE TABLE " + quotedTable + " (id bigint PRIMARY KEY); INSERT INTO " + quotedTable + " VALUES (1)"
	createdResponse := harness.do(http.MethodPost, base,
		map[string]string{"name": "create integration table", "sql": "SELECT 1"}, &editor, true)
	if createdResponse.Code != http.StatusCreated {
		t.Fatalf("create migration = %d %s", createdResponse.Code, createdResponse.Body.String())
	}
	if got := createdResponse.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("migration Cache-Control = %q, want no-store", got)
	}
	created := decodeMap(createdResponse.Body.Bytes())
	migrationID, _ := created["id"].(string)
	if migrationID == "" || created["status"] != "draft" {
		t.Fatalf("created migration = %v", created)
	}
	updated := harness.call(http.MethodPatch, base+"/"+migrationID,
		map[string]string{"name": "create integration table", "sql": successSQL}, editor, http.StatusOK)
	if updated["sql"] != successSQL {
		t.Fatalf("updated migration SQL = %v", updated["sql"])
	}
	executed := harness.call(http.MethodPost, base+"/"+migrationID+"/execute", nil, editor, http.StatusOK)
	if executed["status"] != "succeeded" || executed["executedBy"] != editorID ||
		executed["createdByUsername"] != editorUsername || executed["executedByUsername"] != editorUsername ||
		executed["finishedAt"] == nil {
		t.Fatalf("successful execution = %v", executed)
	}

	failureSQL := "INSERT INTO " + quotedTable + " VALUES (2); INSERT INTO migration_table_that_does_not_exist VALUES (1)"
	failedDraft := harness.call(http.MethodPost, base,
		map[string]string{"name": "rollback integration table", "sql": failureSQL}, editor, http.StatusCreated)
	failedID, _ := failedDraft["id"].(string)
	failed := harness.call(http.MethodPost, base+"/"+failedID+"/execute", nil, editor, http.StatusOK)
	if failed["status"] != "failed" || failed["errorMessage"] == nil {
		t.Fatalf("failed execution = %v", failed)
	}

	var rows int
	if err := target.QueryRow(context.Background(), "SELECT count(*) FROM "+quotedTable).Scan(&rows); err != nil {
		t.Fatalf("inspect target table: %v", err)
	}
	if rows != 1 {
		t.Fatalf("target row count = %d, want 1 after rollback", rows)
	}

	history := harness.call(http.MethodGet, base, nil, viewer, http.StatusOK)
	items, _ := history["items"].([]any)
	if history["canEdit"] != false || history["total"] != float64(2) || len(items) != 2 {
		t.Fatalf("view-only history = %v", history)
	}
	latest, _ := items[0].(map[string]any)
	if latest["createdByUsername"] != editorUsername || latest["executedByUsername"] != editorUsername {
		t.Fatalf("migration history usernames = %v", latest)
	}
	errorMessage, _ := latest["errorMessage"].(string)
	if errorMessage == "" || strings.Contains(errorMessage, "migration_table_that_does_not_exist") {
		t.Fatalf("view-only error message was not sanitized: %q", errorMessage)
	}
	detail := harness.call(http.MethodGet, base+"/"+failedID, nil, viewer, http.StatusOK)
	if detail["canEdit"] != false || detail["status"] != "failed" ||
		detail["createdByUsername"] != editorUsername || detail["executedByUsername"] != editorUsername {
		t.Fatalf("view-only migration detail = %v", detail)
	}
}
