package integration_test

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
)

func TestPostgresTransactionRetryPreservesHistoryAndBatchCheckpoints(t *testing.T) {
	settings := loadTargetSettings(t)
	harness := newPlatformHarness(t)
	harness.createUser("retry_admin", auth.PlatformRoleAdmin)
	editorID := harness.createUser("retry_editor", auth.PlatformRoleUser)
	admin := harness.login("retry_admin")
	database := objectField(harness.call(http.MethodPost, "/api/v1/admin/databases", map[string]any{
		"name": harness.name("transaction_retry"), "host": settings.host, "port": settings.port,
		"databaseName": settings.database, "username": settings.user, "password": settings.password, "sslMode": "disable",
	}, admin, http.StatusCreated), "database")["id"].(string)
	group := objectField(harness.call(http.MethodPost, "/api/v1/admin/groups", map[string]string{
		"name": harness.name("retry_reviewers"),
	}, admin, http.StatusCreated), "group")["id"].(string)
	groupPath := "/api/v1/admin/groups/" + group
	harness.call(http.MethodPost, groupPath+"/members/"+editorID, nil, admin, http.StatusCreated)
	harness.call(http.MethodPut, groupPath+"/databases/"+database, map[string]string{"accessLevel": "view_edit"}, admin, http.StatusOK)
	harness.call(http.MethodPut, groupPath+"/leader", map[string]string{"userId": editorID}, admin, http.StatusOK)
	editor := harness.login("retry_editor")
	base := "/api/v1/databases/" + database + "/migrations"

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	target, err := pgx.Connect(ctx, settings.url())
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close(context.Background())
	table := pgx.Identifier{harness.name("retry_rows")}.Sanitize()
	sequence := pgx.Identifier{harness.name("retry_seq")}.Sanitize()
	trigger := pgx.Identifier{harness.name("retry_trigger")}.Sanitize()
	defer func() {
		_, _ = target.Exec(context.Background(), "DROP TABLE IF EXISTS "+table+
			"; DROP FUNCTION IF EXISTS "+trigger+"(); DROP SEQUENCE IF EXISTS "+sequence)
	}()
	// Sequence increments survive transaction aborts. Fail once in each
	// direction, after the first batch in that direction has already committed.
	fixture := fmt.Sprintf(`CREATE TABLE %s (id bigint PRIMARY KEY, value int NOT NULL, previous_value int);
		INSERT INTO %s VALUES (1,10,NULL),(2,20,NULL),(3,30,NULL);
		CREATE SEQUENCE %s;
		CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.value=101 THEN
				RAISE EXCEPTION 'retry limit fixture' USING ERRCODE='40001';
			END IF;
			IF NEW.value=100 AND nextval('%s')=1 THEN
				RAISE EXCEPTION 'retry single transaction' USING ERRCODE='40001';
			END IF;
			IF NEW.id=2 THEN
				IF (NEW.value=99 OR OLD.value=99) AND nextval('%s') IN (3,5) THEN
					RAISE EXCEPTION 'retry batch transaction' USING ERRCODE='40P01';
				END IF;
			END IF;
			RETURN NEW;
		END $$;
		CREATE TRIGGER retry_once BEFORE UPDATE ON %s FOR EACH ROW EXECUTE FUNCTION %s();`,
		table, table, sequence, trigger, sequence, sequence, table, trigger)
	if _, err := target.Exec(ctx, fixture); err != nil {
		t.Fatal(err)
	}
	harness.startWorker()
	defer harness.stopWorker()

	create := func(payload map[string]any) string {
		t.Helper()
		return harness.call(http.MethodPost, base, payload, editor, http.StatusCreated)["id"].(string)
	}
	run := func(id, route, action string) map[string]any {
		t.Helper()
		harness.approveMigrationAction(base, id, action, editor)
		response := harness.do(http.MethodPost, base+"/"+id+"/"+route, nil, &editor, true)
		if response.Code != http.StatusAccepted {
			t.Fatalf("enqueue %s: %d %s", action, response.Code, response.Body.String())
		}
		return harness.waitForJob(response.Header().Get("Location"), editor)
	}
	expectRows := func(values, backups []int32) {
		t.Helper()
		var gotValues, gotBackups []int32
		if err := target.QueryRow(ctx, "SELECT array_agg(value ORDER BY id),array_agg(COALESCE(previous_value,-1) ORDER BY id) FROM "+table).
			Scan(&gotValues, &gotBackups); err != nil || !slices.Equal(gotValues, values) || !slices.Equal(gotBackups, backups) {
			t.Fatalf("values %v, backups %v, err %v; want %v and %v", gotValues, gotBackups, err, values, backups)
		}
	}
	verifyRetry := func(job map[string]any, action, previousAction, previousStatus string) {
		t.Helper()
		if job["retryCount"] != float64(1) || job["status"] != "succeeded" {
			t.Fatalf("expected a successful job after one retry: %v", job)
		}
		executionPath := "/api/v1/databases/" + database + "/migration-executions/"
		attempt := harness.call(http.MethodGet, executionPath+job["executionId"].(string), nil, editor, http.StatusOK)
		previousID, _ := attempt["relatedExecutionId"].(string)
		if attempt["action"] != action || previousID == "" {
			t.Fatalf("missing retry attempt link: %v", attempt)
		}
		previous := harness.call(http.MethodGet, executionPath+previousID, nil, editor, http.StatusOK)
		if previous["action"] != previousAction || previous["status"] != previousStatus {
			t.Fatalf("original attempt history: %v", previous)
		}
	}
	exhausted := create(map[string]any{
		"name": "retry limit", "sql": "UPDATE " + table + " SET value=101 WHERE id=1",
		"downSql": "UPDATE " + table + " SET value=10 WHERE id=1",
	})
	if job := run(exhausted, "execute", "apply"); job["retryCount"] != float64(1) || job["status"] != "failed" {
		t.Fatalf("expected the job to stop at the retry limit: %v", job)
	}
	expectRows([]int32{10, 20, 30}, []int32{-1, -1, -1})
	normal := create(map[string]any{
		"name": "retry single", "sql": "UPDATE " + table + " SET value=100 WHERE id=1",
		"downSql": "UPDATE " + table + " SET value=10 WHERE id=1",
	})
	verifyRetry(run(normal, "execute", "apply"), "retry", "apply", "failed")
	expectRows([]int32{100, 20, 30}, []int32{-1, -1, -1})

	batch := create(map[string]any{
		"name": "retry batch", "executionMode": "batch", "batchSize": 1,
		"batchSelectSql": "SELECT id FROM " + table + " WHERE id>$1 ORDER BY id LIMIT $2",
		"sql":            "UPDATE " + table + " SET previous_value=value,value=99 WHERE id>$1 AND id<=$2",
		"downSql":        "UPDATE " + table + " SET value=previous_value,previous_value=NULL WHERE id>$1 AND id<=$2 AND previous_value IS NOT NULL",
	})
	verifyRetry(run(batch, "execute", "apply"), "retry", "apply", "partial")
	expectRows([]int32{99, 99, 99}, []int32{100, 20, 30})
	down := run(batch, "rollback", "rollback")
	if down["retryCount"] != float64(1) || down["status"] != "succeeded" {
		t.Fatalf("expected rollback to resume after one retry: %v", down)
	}
	var partialRollbacks int
	if err := harness.database.QueryRow(ctx, `SELECT count(*)
		FROM migration_execution_attempts a JOIN migration_execution_events e ON e.execution_id=a.id
		WHERE a.migration_id=$1::uuid AND a.action='rollback' AND e.event='partially_reverted'`, batch).Scan(&partialRollbacks); err != nil || partialRollbacks != 1 {
		t.Fatalf("expected a partially reverted attempt before rollback resumed, got %d: %v", partialRollbacks, err)
	}
	expectRows([]int32{100, 20, 30}, []int32{-1, -1, -1})
}
