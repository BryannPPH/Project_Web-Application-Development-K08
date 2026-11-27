package integration_test

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/migration"
)

func TestBatchScopeExcludesInsertedRowsAndRejectsUnsafeRollback(t *testing.T) {
	settings := loadTargetSettings(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	target, err := pgx.Connect(ctx, settings.url())
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close(context.Background())

	table := pgx.Identifier{"migration_scope_" + strconv.FormatInt(time.Now().UnixNano(), 36)}.Sanitize()
	exec := func(sql string) {
		t.Helper()
		if _, err := target.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	exec("CREATE TABLE " + table + " (id bigint PRIMARY KEY, value int NOT NULL)")
	defer func() { _, _ = target.Exec(context.Background(), "DROP TABLE "+table) }()
	exec("INSERT INTO " + table + " VALUES (1,0),(3,0),(5,0)")
	var applyID, rollbackID string
	if err := target.QueryRow(ctx, "SELECT gen_random_uuid()::text, gen_random_uuid()::text").Scan(&applyID, &rollbackID); err != nil {
		t.Fatal(err)
	}

	selector := "SELECT id FROM " + table + " WHERE id>$1 ORDER BY id LIMIT $2"
	up := "UPDATE " + table + " SET value=value+1 WHERE id>$1 AND id<=$2 RETURNING value; -- approved SQL"
	down := "UPDATE " + table + " SET value=value-1 WHERE id>$1 AND id<=$2"
	size := 1
	entry := migration.Entry{ExecutionMode: migration.ExecutionModeBatch, BatchSelectSQL: &selector, BatchSize: &size, DownSQL: &down}
	executor := migration.NewPGExecutor(integrationTargetProvider{url: settings.url()})
	apply := executor.ExecuteBatch(ctx, "test", applyID, migration.ExecutionPlan{
		Entry: entry, Action: migration.ActionApply, SQLSnapshot: up,
	}, func(event migration.BatchCommitEvent) error {
		if event.BatchNumber == 1 {
			_, err := target.Exec(ctx, "INSERT INTO "+table+" VALUES (6,0)")
			return err
		}
		return nil
	})
	if apply.Status != migration.StatusSucceeded {
		t.Fatalf("apply result: %#v", apply)
	}
	assertValue := func(id, want int) {
		t.Helper()
		var got int
		if err := target.QueryRow(ctx, "SELECT value FROM "+table+" WHERE id=$1", id).Scan(&got); err != nil || got != want {
			t.Fatalf("row %d = %d, want %d, error %v", id, got, want, err)
		}
	}
	assertValue(5, 1)
	assertValue(6, 0)
	exec("INSERT INTO " + table + " VALUES (4,0)")

	rollback := func() migration.ExecutionResult {
		return executor.ExecuteBatch(ctx, "test", rollbackID, migration.ExecutionPlan{
			Entry: entry, Action: migration.ActionRollback, SQLSnapshot: down, CheckpointRootID: applyID,
		}, nil)
	}
	result := rollback()
	if result.Status != migration.StatusFailed || result.PublicError == nil || !strings.Contains(*result.PublicError, "original batch scope") {
		t.Fatalf("unsafe rollback result: %#v", result)
	}
	assertValue(4, 0)
	assertValue(5, 1)

	exec("DELETE FROM " + table + " WHERE id=5")
	if result := rollback(); result.Status != migration.StatusFailed {
		t.Fatalf("rollback accepted a replacement cursor with the same row count: %#v", result)
	}
	assertValue(4, 0)
	exec("DELETE FROM " + table + " WHERE id=4; INSERT INTO " + table + " VALUES (5,1)")
	if result := rollback(); result.Status != migration.StatusSucceeded {
		t.Fatalf("rollback after fixing scope: %#v", result)
	}
	assertValue(1, 0)
	assertValue(3, 0)
	assertValue(5, 0)
	assertValue(6, 0)

	if err := target.QueryRow(ctx, "SELECT gen_random_uuid()::text").Scan(&applyID); err != nil {
		t.Fatal(err)
	}
	legacy := executor.ExecuteBatch(ctx, "test", applyID, migration.ExecutionPlan{
		Entry: entry, Action: migration.ActionApply, SQLSnapshot: up,
	}, nil)
	if legacy.Status != migration.StatusSucceeded {
		t.Fatalf("prepare legacy checkpoint: %#v", legacy)
	}
	if _, err := target.Exec(ctx, "DELETE FROM dbmp_migration_batch_scope WHERE root_execution_id=$1::uuid", applyID); err != nil {
		t.Fatal(err)
	}
	if result := rollback(); result.Status != migration.StatusFailed || result.PublicError == nil || !strings.Contains(*result.PublicError, "cannot verify the original row scope") {
		t.Fatalf("rollback of legacy checkpoint: %#v", result)
	}
	assertValue(6, 1)
}

func TestUnresolvedBatchBlocksAliasAndRejectedJobsRemainInHistory(t *testing.T) {
	settings := loadTargetSettings(t)
	harness := newPlatformHarness(t)
	harness.createUser("scope_admin", auth.PlatformRoleAdmin)
	editorID := harness.createUser("scope_editor", auth.PlatformRoleUser)
	admin := harness.login("scope_admin")
	group := objectField(harness.call(http.MethodPost, "/api/v1/admin/groups", map[string]string{
		"name": harness.name("scope_group"),
	}, admin, http.StatusCreated), "group")["id"].(string)
	harness.call(http.MethodPost, "/api/v1/admin/groups/"+group+"/members/"+editorID, nil, admin, http.StatusCreated)
	harness.call(http.MethodPut, "/api/v1/admin/groups/"+group+"/leader", map[string]string{"userId": editorID}, admin, http.StatusOK)
	makeTarget := func(alias, host string) string {
		db := objectField(harness.call(http.MethodPost, "/api/v1/admin/databases", map[string]any{
			"name": harness.name(alias), "host": host, "port": settings.port, "databaseName": settings.database,
			"username": settings.user, "password": settings.password, "sslMode": "disable",
		}, admin, http.StatusCreated), "database")["id"].(string)
		harness.call(http.MethodPut, "/api/v1/admin/groups/"+group+"/databases/"+db,
			map[string]string{"accessLevel": "view_edit"}, admin, http.StatusOK)
		return "/api/v1/databases/" + db
	}
	aliasHost := settings.host
	if settings.host == "localhost" {
		aliasHost = "127.0.0.1"
	}
	first, alias := makeTarget("scope_first", settings.host), makeTarget("scope_alias", aliasHost)
	editor := harness.login("scope_editor")

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	target, err := pgx.Connect(ctx, settings.url())
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close(context.Background())
	table := pgx.Identifier{harness.name("scope_fixture")}.Sanitize()
	if _, err := target.Exec(ctx, "CREATE TABLE "+table+
		"(id bigint PRIMARY KEY, value int NOT NULL CHECK (id<>2 OR value=0)); INSERT INTO "+table+" VALUES (1,0),(2,0),(3,0)"); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = target.Exec(context.Background(), "DROP TABLE "+table) }()

	create := func(base string, payload map[string]any) string {
		return harness.call(http.MethodPost, base+"/migrations", payload, editor, http.StatusCreated)["id"].(string)
	}
	enqueue := func(base, id, action string) map[string]any {
		t.Helper()
		r := harness.do(http.MethodPost, base+"/migrations/"+id+"/"+action, nil, &editor, true)
		if r.Code != http.StatusAccepted {
			t.Fatalf("enqueue %s = %d %s", action, r.Code, r.Body.String())
		}
		return harness.waitForJob(r.Header().Get("Location"), editor)
	}
	apply := func(base, id string) map[string]any {
		harness.approveApply(base+"/migrations", id, editor)
		return enqueue(base, id, "execute")
	}
	batch := create(first, map[string]any{
		"name": "partial batch", "sql": "UPDATE " + table + " SET value=1 WHERE id>$1 AND id<=$2",
		"downSql":       "UPDATE " + table + " SET value=0 WHERE id>$1 AND id<=$2",
		"executionMode": "batch", "batchSize": 1, "batchSelectSql": "SELECT id FROM " + table + " WHERE id>$1 ORDER BY id LIMIT $2",
	})
	harness.startWorker()
	if job := apply(first, batch); job["status"] != "partial" {
		t.Fatalf("expected partial batch: %v", job)
	}
	harness.stopWorker()
	harness.startWorker()

	other := create(alias, map[string]any{"name": "alias apply", "sql": "UPDATE " + table + " SET value=2 WHERE id=3"})
	aliasJob := apply(alias, other)
	if aliasJob["status"] != "failed" || aliasJob["errorCode"] != "TARGET_BATCH_BLOCKED" || aliasJob["executionId"] != nil {
		t.Fatalf("expected alias to fail before SQL: %v", aliasJob)
	}
	var value int
	if err := target.QueryRow(ctx, "SELECT value FROM "+table+" WHERE id=3").Scan(&value); err != nil || value != 0 {
		t.Fatalf("blocked alias changed the target: value=%d error=%v", value, err)
	}

	harness.approveMigrationAction(first+"/migrations", batch, "rollback", editor)
	if job := enqueue(first, batch, "rollback"); job["status"] != "succeeded" {
		t.Fatalf("expected recovery to remain available: %v", job)
	}
	if job := enqueue(alias, other, "execute"); job["status"] != "succeeded" {
		t.Fatalf("expected alias to run after recovery: %v", job)
	}
	create(alias, map[string]any{"name": "earlier draft", "sql": "SELECT 1"})
	blocked := create(alias, map[string]any{"name": "blocked attempt", "sql": "SELECT 2"})
	orderJob := apply(alias, blocked)
	if orderJob["status"] != "failed" || orderJob["errorCode"] != "MIGRATION_ORDER_CONFLICT" || orderJob["executionId"] != nil {
		t.Fatalf("unexpected order rejection: %v", orderJob)
	}

	history := harness.call(http.MethodGet, alias+"/migration-history?hideRollback=true", nil, editor, http.StatusOK)
	for _, job := range []map[string]any{aliasJob, orderJob} {
		id := job["jobId"].(string)
		found := false
		for _, raw := range history["items"].([]any) {
			item := raw.(map[string]any)
			if item["jobId"] == id {
				found = item["status"] == "failed" && item["executionId"] == nil && item["errorCode"] == job["errorCode"]
			}
		}
		if !found {
			t.Fatalf("rejected job %s missing from history: %v", id, history)
		}
		detail := harness.call(http.MethodGet, alias+"/migration-executions/"+id, nil, editor, http.StatusOK)
		if detail["jobId"] != id || detail["sqlSnapshot"] != "" || detail["executionId"] != nil {
			t.Fatalf("rejected job presented as SQL execution: %v", detail)
		}
		harness.call(http.MethodGet, first+"/migration-executions/"+id, nil, editor, http.StatusNotFound)
	}
}
