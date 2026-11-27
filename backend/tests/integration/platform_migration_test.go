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
	// Kedua group punya akses, jadi setiap eksekusi butuh approval leader keduanya.
	harness.call(http.MethodPut, "/api/v1/admin/groups/"+readersID+"/leader",
		map[string]any{"userId": viewerID}, admin, http.StatusOK)
	harness.call(http.MethodPut, "/api/v1/admin/groups/"+writersID+"/leader",
		map[string]any{"userId": editorID}, admin, http.StatusOK)

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
	tableExists := func() bool {
		var exists bool
		if err := target.QueryRow(context.Background(), `SELECT to_regclass($1) IS NOT NULL`, tableName).Scan(&exists); err != nil {
			t.Fatalf("inspect target table: %v", err)
		}
		return exists
	}
	t.Cleanup(func() {
		_, _ = target.Exec(context.Background(), "DROP TABLE IF EXISTS "+quotedTable)
	})

	successSQL := "CREATE TABLE " + quotedTable + " (id bigint PRIMARY KEY); INSERT INTO " + quotedTable + " VALUES (1)"
	downSQL := "DROP TABLE " + quotedTable
	createdResponse := harness.do(http.MethodPost, base,
		map[string]string{"name": "create integration table", "sql": "SELECT 1", "downSql": downSQL}, &editor, true)
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
	if updated["sql"] != successSQL || updated["downSql"] != downSQL {
		t.Fatalf("updated migration SQL = %v", updated["sql"])
	}
	harness.approveApply(base, migrationID, viewer, editor)
	idempotencyKey := "migration-http-" + migrationID
	enqueueHeaders := map[string]string{"Idempotency-Key": idempotencyKey}
	enqueuedResponse := harness.doWithHeaders(http.MethodPost, base+"/"+migrationID+"/execute", nil, &editor, true, enqueueHeaders)
	if enqueuedResponse.Code != http.StatusAccepted {
		t.Fatalf("enqueue successful execution = %d %s", enqueuedResponse.Code, enqueuedResponse.Body.String())
	}
	enqueued := decodeMap(enqueuedResponse.Body.Bytes())
	jobID, _ := enqueued["jobId"].(string)
	jobLocation := enqueuedResponse.Header().Get("Location")
	if jobID == "" || jobLocation == "" || !strings.HasSuffix(jobLocation, "/"+jobID) {
		t.Fatalf("accepted execution did not include jobId and Location: body=%v location=%q", enqueued, jobLocation)
	}
	if reloaded := harness.call(http.MethodGet, base+"/"+migrationID, nil, viewer, http.StatusOK); reloaded["latestJobId"] != jobID {
		t.Fatalf("migration detail should expose the queued job after reload: %v", reloaded)
	}
	duplicateResponse := harness.doWithHeaders(http.MethodPost, base+"/"+migrationID+"/execute", nil, &editor, true, enqueueHeaders)
	duplicate := decodeMap(duplicateResponse.Body.Bytes())
	if duplicateResponse.Code != http.StatusAccepted || duplicate["jobId"] != jobID {
		t.Fatalf("idempotent duplicate enqueue = %d %v, original jobId=%q", duplicateResponse.Code, duplicate, jobID)
	}
	command, err := harness.database.Exec(context.Background(), `UPDATE migration_jobs SET
		status='running',worker_id='expired-before-attempt',lease_token=gen_random_uuid(),
		lease_expires_at=CURRENT_TIMESTAMP-INTERVAL '1 second',started_at=CURRENT_TIMESTAMP
		WHERE id=$1::uuid AND status='queued' AND execution_id IS NULL`, jobID)
	if err != nil {
		t.Fatalf("expire job before execution attempt: %v", err)
	}
	if command.RowsAffected() != 1 {
		t.Fatalf("expired job update rows = %d, want 1", command.RowsAffected())
	}
	if tableExists() {
		t.Fatal("target changed before a worker started")
	}
	harness.startWorker()
	jobResult := harness.waitForJob(jobLocation, editor)
	if jobResult["status"] != "succeeded" || jobResult["retryCount"] != float64(1) {
		t.Fatalf("successful execution job = %v", jobResult)
	}
	executed := harness.call(http.MethodGet, base+"/"+migrationID, nil, viewer, http.StatusOK)
	if executed["status"] != "succeeded" || executed["executedBy"] != editorID ||
		executed["createdByUsername"] != editorUsername || executed["executedByUsername"] != editorUsername ||
		executed["finishedAt"] == nil {
		t.Fatalf("successful execution = %v", executed)
	}
	verifyRollbackAndForwardFailures(t, harness, target, base, databaseID, migrationID, tableName, viewer, editor)

	failureSQL := "INSERT INTO " + quotedTable + " VALUES (2); INSERT INTO migration_table_that_does_not_exist VALUES (1)"
	failedDraft := harness.call(http.MethodPost, base,
		map[string]string{"name": "rollback integration table", "sql": failureSQL}, editor, http.StatusCreated)
	failedID, _ := failedDraft["id"].(string)
	harness.approveApply(base, failedID, viewer, editor)
	failedEnqueue := harness.do(http.MethodPost, base+"/"+failedID+"/execute", nil, &editor, true)
	if failedEnqueue.Code != http.StatusAccepted {
		t.Fatalf("enqueue failed execution = %d %s", failedEnqueue.Code, failedEnqueue.Body.String())
	}
	failedJob := harness.waitForJob(failedEnqueue.Header().Get("Location"), editor)
	if failedJob["status"] != "failed" {
		t.Fatalf("failed execution job = %v", failedJob)
	}
	failed := harness.call(http.MethodGet, base+"/"+failedID, nil, viewer, http.StatusOK)
	if failed["status"] != "failed" || failed["errorMessage"] == nil {
		t.Fatalf("failed execution = %v; job = %v", failed, failedJob)
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

	unresolved := harness.call(http.MethodPost, base,
		map[string]string{"name": "reconcile fixture", "sql": "SELECT 1"}, editor, http.StatusCreated)
	unresolvedID, _ := unresolved["id"].(string)
	if _, err := harness.database.Exec(context.Background(), `UPDATE migration_entries
		SET status='needs_reconciliation', started_at=CURRENT_TIMESTAMP, finished_at=CURRENT_TIMESTAMP,
			error_message='fixture requires target inspection'
		WHERE id=$1::uuid`, unresolvedID); err != nil {
		t.Fatalf("mark reconciliation fixture: %v", err)
	}
	reconcilePath := base + "/" + unresolvedID + "/reconcile"
	if response := harness.do(http.MethodPost, reconcilePath,
		map[string]any{"inspectedTarget": true, "note": "Verified target schema during integration test."}, &viewer, true); response.Code != http.StatusForbidden {
		t.Fatalf("view-only reconciliation = %d %s", response.Code, response.Body.String())
	}
	if response := harness.do(http.MethodPost, reconcilePath,
		map[string]any{"inspectedTarget": true, "note": "Verified target schema during integration test."}, &editor, false); response.Code != http.StatusForbidden || errorCode(response) != "INVALID_CSRF" {
		t.Fatalf("reconciliation without csrf = %d %s", response.Code, response.Body.String())
	}
	if response := harness.do(http.MethodPost, reconcilePath,
		map[string]any{"inspectedTarget": false, "note": "Verified target schema during integration test."}, &editor, true); response.Code != http.StatusBadRequest {
		t.Fatalf("unconfirmed reconciliation = %d %s", response.Code, response.Body.String())
	}
	if response := harness.do(http.MethodPost, reconcilePath,
		map[string]any{"inspectedTarget": true, "note": "Verified target schema during integration test."}, &editor, true); response.Code != http.StatusOK {
		t.Fatalf("reconciliation = %d %s", response.Code, response.Body.String())
	}
	reconciled := decodeMap(harness.do(http.MethodGet, base+"/"+unresolvedID, nil, &viewer, false).Body.Bytes())
	if reconciled["status"] != "reconciled" || reconciled["reconciledFrom"] != "needs_reconciliation" ||
		reconciled["reconciledBy"] != editorID || reconciled["reconciledByUsername"] != editorUsername ||
		reconciled["reconciliationNote"] != "Verified target schema during integration test." || reconciled["reconciledAt"] == nil {
		t.Fatalf("reconciled detail = %v", reconciled)
	}
	if reconciled["canReconcile"] != false || reconciled["canEdit"] != false {
		t.Fatalf("view-only reconciled capabilities = %v", reconciled)
	}

	nextDraft := harness.call(http.MethodPost, base,
		map[string]string{"name": "after reconciliation", "sql": "SELECT 1"}, editor, http.StatusCreated)
	nextID, _ := nextDraft["id"].(string)
	harness.approveApply(base, nextID, viewer, editor)
	nextEnqueue := harness.do(http.MethodPost, base+"/"+nextID+"/execute", nil, &editor, true)
	if nextEnqueue.Code != http.StatusAccepted {
		t.Fatalf("enqueue migration after reconciliation = %d %s", nextEnqueue.Code, nextEnqueue.Body.String())
	}
	if nextJob := harness.waitForJob(nextEnqueue.Header().Get("Location"), editor); nextJob["status"] != "succeeded" {
		t.Fatalf("migration after reconciliation job = %v", nextJob)
	}
	nextResult := harness.call(http.MethodGet, base+"/"+nextID, nil, editor, http.StatusOK)
	if nextResult["status"] != "succeeded" {
		t.Fatalf("migration after reconciliation = %v", nextResult)
	}

	verifyUncertainSingleCommitRecovery(t, harness, target, base, databaseID, editorID, viewer, editor)
	harness.stopWorker()
	recoveryTable := fmt.Sprintf("migration_expired_batch_%d", time.Now().UnixNano())
	quotedRecoveryTable := pgx.Identifier{recoveryTable}.Sanitize()
	if _, err := target.Exec(context.Background(), "CREATE TABLE "+quotedRecoveryTable+" (id bigint PRIMARY KEY, value bigint NOT NULL)"); err != nil {
		t.Fatalf("create expired batch recovery fixture: %v", err)
	}
	t.Cleanup(func() { _, _ = target.Exec(context.Background(), "DROP TABLE IF EXISTS "+quotedRecoveryTable) })
	if _, err := target.Exec(context.Background(), "INSERT INTO "+quotedRecoveryTable+" (id,value) VALUES (1,0),(2,0)"); err != nil {
		t.Fatalf("seed expired batch recovery fixture: %v", err)
	}
	batchSQL := "UPDATE " + quotedRecoveryTable + " SET value=value+10 WHERE id>$1 AND id<=$2"
	rollbackSQL := "UPDATE " + quotedRecoveryTable + " SET value=value-10 WHERE id>$1 AND id<=$2"
	selectorSQL := "SELECT id FROM " + quotedRecoveryTable + " WHERE id>$1 ORDER BY id LIMIT $2"
	recoveryBatchSize := 1
	recoveryDraft := harness.do(http.MethodPost, base, map[string]any{
		"name": "recover expired batch lease", "sql": batchSQL, "downSql": rollbackSQL,
		"executionMode": "batch", "batchSelectSql": selectorSQL,
		"batchSize": recoveryBatchSize, "batchCursorStart": 0,
	}, &editor, true)
	if recoveryDraft.Code != http.StatusCreated {
		t.Fatalf("create expired batch recovery migration: %d %s", recoveryDraft.Code, recoveryDraft.Body.String())
	}
	recoveryMigrationID, _ := decodeMap(recoveryDraft.Body.Bytes())["id"].(string)
	harness.approveApply(base, recoveryMigrationID, viewer, editor)
	recoveryEnqueue := harness.do(http.MethodPost, base+"/"+recoveryMigrationID+"/execute", nil, &editor, true)
	if recoveryEnqueue.Code != http.StatusAccepted {
		t.Fatalf("enqueue expired batch recovery migration: %d %s", recoveryEnqueue.Code, recoveryEnqueue.Body.String())
	}
	recoveryJob, _ := decodeMap(recoveryEnqueue.Body.Bytes())["jobId"].(string)
	var version, revision int64
	if err := harness.database.QueryRow(context.Background(), `SELECT version,revision FROM migration_entries WHERE id=$1::uuid`, recoveryMigrationID).
		Scan(&version, &revision); err != nil {
		t.Fatalf("load expired batch migration revision: %v", err)
	}
	var attemptID string
	if err := harness.database.QueryRow(context.Background(), `SELECT gen_random_uuid()::text`).Scan(&attemptID); err != nil {
		t.Fatalf("create expired batch attempt id: %v", err)
	}
	if _, err := harness.database.Exec(context.Background(), `INSERT INTO migration_execution_attempts
		(id,database_id,migration_id,version,revision,action,actor_id,requested_at,sql_snapshot,checkpoint_root_id)
		VALUES ($1::uuid,$2::uuid,$3::uuid,$4,$5,'apply',$6::uuid,CURRENT_TIMESTAMP,$7,$1::uuid)`,
		attemptID, databaseID, recoveryMigrationID, version, revision, editorID, batchSQL); err != nil {
		t.Fatalf("create expired batch attempt: %v", err)
	}
	if _, err := harness.database.Exec(context.Background(), `INSERT INTO migration_execution_events
		(execution_id,status,event,occurred_at) VALUES ($1::uuid,'running','started',CURRENT_TIMESTAMP)`, attemptID); err != nil {
		t.Fatalf("record expired batch attempt: %v", err)
	}
	if _, err := harness.database.Exec(context.Background(), `UPDATE migration_entries SET status='running',executed_by=$2::uuid,
		started_at=CURRENT_TIMESTAMP,finished_at=NULL,error_message=NULL,last_execution_id=$3::uuid WHERE id=$1::uuid`,
		recoveryMigrationID, editorID, attemptID); err != nil {
		t.Fatalf("mark expired batch migration running: %v", err)
	}
	if _, err := harness.database.Exec(context.Background(), `UPDATE migration_jobs SET status='running',
		worker_id='expired-during-batch',lease_token=gen_random_uuid(),lease_expires_at=CURRENT_TIMESTAMP-INTERVAL '1 second',
		started_at=CURRENT_TIMESTAMP,execution_id=$2::uuid WHERE id=$1::uuid AND status='queued'`,
		recoveryJob, attemptID); err != nil {
		t.Fatalf("expire batch job after execution started: %v", err)
	}
	if _, err := target.Exec(context.Background(), `CREATE TABLE IF NOT EXISTS dbmp_migration_batch_checkpoints (
		root_execution_id UUID NOT NULL,execution_id UUID NOT NULL,operation VARCHAR(8) NOT NULL CHECK (operation IN ('up','down')),
		batch_number BIGINT NOT NULL CHECK (batch_number>0),cursor_start BIGINT NOT NULL,cursor_end BIGINT NOT NULL CHECK (cursor_end>=cursor_start),
		rows_affected BIGINT NOT NULL CHECK (rows_affected>=0),committed_at TIMESTAMPTZ NOT NULL,
		PRIMARY KEY (root_execution_id,operation,batch_number))`); err != nil {
		t.Fatalf("prepare target batch checkpoint table: %v", err)
	}
	if _, err := target.Exec(context.Background(), "UPDATE "+quotedRecoveryTable+" SET value=10 WHERE id=1"); err != nil {
		t.Fatalf("seed committed batch state: %v", err)
	}
	if _, err := target.Exec(context.Background(), `INSERT INTO dbmp_migration_batch_checkpoints
		(root_execution_id,execution_id,operation,batch_number,cursor_start,cursor_end,rows_affected,committed_at)
		VALUES ($1::uuid,$1::uuid,'up',1,0,1,1,CURRENT_TIMESTAMP)`, attemptID); err != nil {
		t.Fatalf("seed committed target checkpoint: %v", err)
	}
	harness.startWorker()
	recoveredJob := harness.waitForJob(recoveryEnqueue.Header().Get("Location"), editor)
	if recoveredJob["status"] != "partial" || recoveredJob["executionId"] != attemptID {
		t.Fatalf("expired batch recovery job = %v", recoveredJob)
	}
	partialReview := objectField(harness.call(http.MethodGet, base+"/"+recoveryMigrationID+"/approvals?action=rollback", nil, viewer, http.StatusOK), "review")
	if partialReview["canDecide"] != true || partialReview["requiredCount"] != float64(2) {
		t.Fatalf("view-only leader cannot review rollback for a partial batch: %v", partialReview)
	}
	harness.approveMigrationAction(base, recoveryMigrationID, "rollback", viewer, editor)
	var recoveredRows, untouchedRows int64
	if err := target.QueryRow(context.Background(), "SELECT count(*) FILTER (WHERE value=10),count(*) FILTER (WHERE value=0) FROM "+quotedRecoveryTable).
		Scan(&recoveredRows, &untouchedRows); err != nil || recoveredRows != 1 || untouchedRows != 1 {
		t.Fatalf("recovered target state: committed=%d untouched=%d error=%v", recoveredRows, untouchedRows, err)
	}
}

func verifyRollbackAndForwardFailures(t *testing.T, harness *platformHarness, target *pgx.Conn,
	base, databaseID, migrationID, tableName string, viewer, editor platformLogin) {
	t.Helper()
	ctx := context.Background()
	table := pgx.Identifier{tableName}.Sanitize()
	moved := pgx.Identifier{tableName + "_moved"}.Sanitize()
	t.Cleanup(func() { _, _ = target.Exec(ctx, "DROP TABLE IF EXISTS "+moved) })
	runTarget := func(sql string) {
		t.Helper()
		if _, err := target.Exec(ctx, sql); err != nil {
			t.Fatalf("prepare target failure: %v", err)
		}
	}
	checkAction := func(action, wantJob, wantMigration string) {
		t.Helper()
		response := harness.do(http.MethodPost, base+"/"+migrationID+"/"+action, nil, &editor, true)
		if response.Code != http.StatusAccepted {
			t.Fatalf("enqueue %s: %d %s", action, response.Code, response.Body.String())
		}
		location := response.Header().Get("Location")
		job := harness.waitForJob(location, editor)
		if job["status"] != wantJob || (wantJob == "failed" && job["errorMessage"] == nil) {
			t.Fatalf("%s job = %v, want %s", action, job, wantJob)
		}
		entry := harness.call(http.MethodGet, base+"/"+migrationID, nil, viewer, http.StatusOK)
		if entry["status"] != wantMigration || entry["latestJobId"] != job["jobId"] {
			t.Fatalf("%s retained migration state = %v, want %s", action, entry, wantMigration)
		}
		executionID, _ := job["executionId"].(string)
		attempt := harness.call(http.MethodGet, "/api/v1/databases/"+databaseID+"/migration-executions/"+executionID, nil, viewer, http.StatusOK)
		if attempt["status"] != wantJob || attempt["migrationName"] == "" || attempt["sqlSnapshot"] == "" {
			t.Fatalf("%s execution detail = %v, want status %s and SQL snapshot", action, attempt, wantJob)
		}
	}

	runTarget("ALTER TABLE " + table + " RENAME TO " + moved)
	harness.approveMigrationAction(base, migrationID, "rollback", viewer, editor)
	checkAction("rollback", "failed", "succeeded")
	runTarget("ALTER TABLE " + moved + " RENAME TO " + table)
	checkAction("rollback", "succeeded", "rolled_back")

	runTarget("CREATE TABLE " + table + " (id bigint)")
	harness.approveMigrationAction(base, migrationID, "forward", viewer, editor)
	checkAction("forward", "failed", "rolled_back")
	runTarget("DROP TABLE " + table)
	checkAction("forward", "succeeded", "succeeded")
}

func verifyUncertainSingleCommitRecovery(t *testing.T, harness *platformHarness, target *pgx.Conn,
	base, databaseID, editorID string, viewer, editor platformLogin) {
	t.Helper()
	// A single-statement transaction can commit on the target before the worker
	// records its terminal platform state. Without a batch checkpoint, recovery
	// must require inspection and keep later jobs queued instead of replaying SQL.
	harness.stopWorker()
	uncertainTable := fmt.Sprintf("migration_uncertain_commit_%d", time.Now().UnixNano())
	quotedUncertainTable := pgx.Identifier{uncertainTable}.Sanitize()
	if _, err := target.Exec(context.Background(), "CREATE TABLE "+quotedUncertainTable+" (id bigint PRIMARY KEY, value bigint NOT NULL)"); err != nil {
		t.Fatalf("create uncertain commit fixture: %v", err)
	}
	t.Cleanup(func() { _, _ = target.Exec(context.Background(), "DROP TABLE IF EXISTS "+quotedUncertainTable) })
	if _, err := target.Exec(context.Background(), "INSERT INTO "+quotedUncertainTable+" (id,value) VALUES (1,0)"); err != nil {
		t.Fatalf("seed uncertain commit fixture: %v", err)
	}
	uncertainSQL := "UPDATE " + quotedUncertainTable + " SET value=value+1 WHERE id=1"
	uncertainDownSQL := "UPDATE " + quotedUncertainTable + " SET value=value-1 WHERE id=1"
	uncertainDraft := harness.do(http.MethodPost, base, map[string]any{
		"name": "recover uncertain single commit", "sql": uncertainSQL, "downSql": uncertainDownSQL,
	}, &editor, true)
	if uncertainDraft.Code != http.StatusCreated {
		t.Fatalf("create uncertain commit migration: %d %s", uncertainDraft.Code, uncertainDraft.Body.String())
	}
	uncertainMigrationID, _ := decodeMap(uncertainDraft.Body.Bytes())["id"].(string)
	harness.approveApply(base, uncertainMigrationID, viewer, editor)
	uncertainEnqueue := harness.do(http.MethodPost, base+"/"+uncertainMigrationID+"/execute", nil, &editor, true)
	if uncertainEnqueue.Code != http.StatusAccepted {
		t.Fatalf("enqueue uncertain commit migration: %d %s", uncertainEnqueue.Code, uncertainEnqueue.Body.String())
	}
	uncertainJob, _ := decodeMap(uncertainEnqueue.Body.Bytes())["jobId"].(string)
	var uncertainVersion, uncertainRevision int64
	if err := harness.database.QueryRow(context.Background(), `SELECT version,revision FROM migration_entries WHERE id=$1::uuid`, uncertainMigrationID).
		Scan(&uncertainVersion, &uncertainRevision); err != nil {
		t.Fatalf("load uncertain migration revision: %v", err)
	}
	var uncertainAttempt string
	if err := harness.database.QueryRow(context.Background(), `SELECT gen_random_uuid()::text`).Scan(&uncertainAttempt); err != nil {
		t.Fatalf("create uncertain attempt id: %v", err)
	}
	if _, err := harness.database.Exec(context.Background(), `INSERT INTO migration_execution_attempts
		(id,database_id,migration_id,version,revision,action,actor_id,requested_at,sql_snapshot,checkpoint_root_id)
		VALUES ($1::uuid,$2::uuid,$3::uuid,$4,$5,'apply',$6::uuid,CURRENT_TIMESTAMP,$7,$1::uuid)`,
		uncertainAttempt, databaseID, uncertainMigrationID, uncertainVersion, uncertainRevision, editorID, uncertainSQL); err != nil {
		t.Fatalf("create uncertain attempt: %v", err)
	}
	if _, err := harness.database.Exec(context.Background(), `INSERT INTO migration_execution_events
		(execution_id,status,event,occurred_at) VALUES ($1::uuid,'running','started',CURRENT_TIMESTAMP)`, uncertainAttempt); err != nil {
		t.Fatalf("record uncertain attempt: %v", err)
	}
	if _, err := harness.database.Exec(context.Background(), `UPDATE migration_entries SET status='running',executed_by=$2::uuid,
		started_at=CURRENT_TIMESTAMP,finished_at=NULL,error_message=NULL,last_execution_id=$3::uuid WHERE id=$1::uuid`,
		uncertainMigrationID, editorID, uncertainAttempt); err != nil {
		t.Fatalf("mark uncertain migration running: %v", err)
	}
	if _, err := harness.database.Exec(context.Background(), `UPDATE migration_jobs SET status='running',
		worker_id='expired-after-single-commit',lease_token=gen_random_uuid(),lease_expires_at=CURRENT_TIMESTAMP-INTERVAL '1 second',
		started_at=CURRENT_TIMESTAMP,execution_id=$2::uuid WHERE id=$1::uuid AND status='queued'`,
		uncertainJob, uncertainAttempt); err != nil {
		t.Fatalf("expire single job after execution started: %v", err)
	}
	// Model a target commit whose acknowledgement was lost to the worker.
	if _, err := target.Exec(context.Background(), "UPDATE "+quotedUncertainTable+" SET value=1 WHERE id=1"); err != nil {
		t.Fatalf("seed uncertain committed target state: %v", err)
	}
	harness.startWorker()
	recoveredUncertainJob := harness.waitForJob(uncertainEnqueue.Header().Get("Location"), editor)
	if recoveredUncertainJob["status"] != "needs_reconciliation" || recoveredUncertainJob["executionId"] != uncertainAttempt {
		t.Fatalf("uncertain single execution recovery = %v", recoveredUncertainJob)
	}
	var uncertainValue int64
	if err := target.QueryRow(context.Background(), "SELECT value FROM "+quotedUncertainTable+" WHERE id=1").Scan(&uncertainValue); err != nil || uncertainValue != 1 {
		t.Fatalf("recovery replayed uncertain SQL: value=%d error=%v", uncertainValue, err)
	}

	nextUncertainDraft := harness.do(http.MethodPost, base, map[string]any{
		"name": "blocked behind uncertain commit", "sql": "UPDATE " + quotedUncertainTable + " SET value=value+10 WHERE id=1",
	}, &editor, true)
	if nextUncertainDraft.Code != http.StatusCreated {
		t.Fatalf("create migration behind uncertain commit: %d %s", nextUncertainDraft.Code, nextUncertainDraft.Body.String())
	}
	nextUncertainID, _ := decodeMap(nextUncertainDraft.Body.Bytes())["id"].(string)
	harness.approveApply(base, nextUncertainID, viewer, editor)
	nextUncertainEnqueue := harness.do(http.MethodPost, base+"/"+nextUncertainID+"/execute", nil, &editor, true)
	if nextUncertainEnqueue.Code != http.StatusAccepted {
		t.Fatalf("enqueue migration behind uncertain commit: %d %s", nextUncertainEnqueue.Code, nextUncertainEnqueue.Body.String())
	}
	for attempt := 0; attempt < 5; attempt++ {
		time.Sleep(50 * time.Millisecond)
		queued := harness.call(http.MethodGet, nextUncertainEnqueue.Header().Get("Location"), nil, viewer, http.StatusOK)
		if queued["status"] != "queued" {
			t.Fatalf("migration ran before uncertain outcome was reconciled: %v", queued)
		}
	}
	if err := target.QueryRow(context.Background(), "SELECT value FROM "+quotedUncertainTable+" WHERE id=1").Scan(&uncertainValue); err != nil || uncertainValue != 1 {
		t.Fatalf("target changed while later job was blocked: value=%d error=%v", uncertainValue, err)
	}
	reconcilePath := base + "/" + uncertainMigrationID + "/reconcile"
	if response := harness.do(http.MethodPost, reconcilePath,
		map[string]any{"inspectedTarget": true, "note": "Verified the committed target value during recovery."}, &editor, true); response.Code != http.StatusOK {
		t.Fatalf("reconcile uncertain execution: %d %s", response.Code, response.Body.String())
	}
	if nextJob := harness.waitForJob(nextUncertainEnqueue.Header().Get("Location"), editor); nextJob["status"] != "succeeded" {
		t.Fatalf("job after uncertain outcome reconciliation = %v", nextJob)
	}
	if err := target.QueryRow(context.Background(), "SELECT value FROM "+quotedUncertainTable+" WHERE id=1").Scan(&uncertainValue); err != nil || uncertainValue != 11 {
		t.Fatalf("target after reconciliation and queued migration: value=%d error=%v", uncertainValue, err)
	}
}
