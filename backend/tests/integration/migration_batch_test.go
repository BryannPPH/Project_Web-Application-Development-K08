package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/migration"
)

func TestBatchMigrationPartialFailureResumeAndReverseRollback(t *testing.T) {
	platformURL := os.Getenv("TEST_PLATFORM_DATABASE_URL")
	targetURL := os.Getenv("TEST_TARGET_DATABASE_URL")
	if platformURL == "" {
		t.Skip("platform test database URL is required")
	}
	if targetURL == "" {
		targetURL = loadTargetSettings(t).url()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	platform, err := pgxpool.New(ctx, platformURL)
	if err != nil {
		t.Fatalf("connect platform database: %v", err)
	}
	t.Cleanup(platform.Close)
	target, err := pgx.Connect(ctx, targetURL)
	if err != nil {
		t.Fatalf("connect target database: %v", err)
	}
	t.Cleanup(func() { _ = target.Close(context.Background()) })

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	table := "migration_batch_" + suffix
	function := "migration_batch_fail_" + suffix
	trigger := "migration_batch_fail_trigger_" + suffix
	username, databaseName := "batch_user_"+suffix, "batch_db_"+suffix
	var userID, databaseID string
	if err := platform.QueryRow(ctx, `INSERT INTO users (username,password_hash) VALUES ($1,'test-only') RETURNING id::text`, username).Scan(&userID); err != nil {
		t.Fatalf("create migration user: %v", err)
	}
	if err := platform.QueryRow(ctx, `INSERT INTO managed_databases
		(name,host,database_name,username,credential_ciphertext,ssl_mode)
		VALUES ($1,'localhost','target_db','target_owner',$2,'disable') RETURNING id::text`,
		databaseName, []byte("test-only")).Scan(&databaseID); err != nil {
		t.Fatalf("create managed database: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		_, _ = target.Exec(cleanupCtx, `DROP TRIGGER IF EXISTS `+trigger+` ON `+table)
		_, _ = target.Exec(cleanupCtx, `DROP FUNCTION IF EXISTS `+function+`() CASCADE`)
		_, _ = target.Exec(cleanupCtx, `DROP TABLE IF EXISTS `+table)
		cleanupTx, beginErr := platform.Begin(cleanupCtx)
		if beginErr != nil {
			t.Errorf("begin batch fixture cleanup: %v", beginErr)
			return
		}
		defer func() { _ = cleanupTx.Rollback(context.Background()) }()
		for _, statement := range []string{
			`ALTER TABLE migration_execution_events DISABLE TRIGGER migration_execution_events_append_only`,
			`ALTER TABLE migration_execution_attempts DISABLE TRIGGER migration_execution_attempts_append_only`,
		} {
			if _, err := cleanupTx.Exec(cleanupCtx, statement); err != nil {
				t.Errorf("disable append-only trigger: %v", err)
				return
			}
		}
		if _, err := cleanupTx.Exec(cleanupCtx, `DELETE FROM migration_execution_events WHERE execution_id IN (
			SELECT id FROM migration_execution_attempts WHERE database_id=$1)`, databaseID); err != nil {
			t.Errorf("delete batch execution events: %v", err)
			return
		}
		for {
			command, err := cleanupTx.Exec(cleanupCtx, `DELETE FROM migration_execution_attempts a
				WHERE a.database_id=$1 AND NOT EXISTS (SELECT 1 FROM migration_execution_attempts child WHERE child.related_execution_id=a.id)`, databaseID)
			if err != nil {
				t.Errorf("delete batch execution attempts: %v", err)
				return
			}
			if command.RowsAffected() == 0 {
				break
			}
		}
		if _, err := cleanupTx.Exec(cleanupCtx, `DELETE FROM migration_entries WHERE database_id=$1`, databaseID); err != nil {
			t.Errorf("delete batch migration definitions: %v", err)
			return
		}
		for _, statement := range []string{
			`ALTER TABLE migration_execution_events ENABLE TRIGGER migration_execution_events_append_only`,
			`ALTER TABLE migration_execution_attempts ENABLE TRIGGER migration_execution_attempts_append_only`,
		} {
			if _, err := cleanupTx.Exec(cleanupCtx, statement); err != nil {
				t.Errorf("enable append-only trigger: %v", err)
				return
			}
		}
		if err := cleanupTx.Commit(cleanupCtx); err != nil {
			t.Errorf("commit batch fixture cleanup: %v", err)
			return
		}
		if _, err := platform.Exec(cleanupCtx, `DELETE FROM managed_databases WHERE id=$1`, databaseID); err != nil {
			t.Errorf("delete batch managed database: %v", err)
		}
		if _, err := platform.Exec(cleanupCtx, `DELETE FROM users WHERE id=$1`, userID); err != nil {
			t.Errorf("delete batch user: %v", err)
		}
	})

	if _, err := target.Exec(ctx, `CREATE TABLE `+table+` (id BIGINT PRIMARY KEY, value BIGINT NOT NULL)`); err != nil {
		t.Fatalf("create batch target table: %v", err)
	}
	if _, err := target.Exec(ctx, `INSERT INTO `+table+` (id,value) SELECT id,0 FROM generate_series(1,7) id`); err != nil {
		t.Fatalf("seed batch target table: %v", err)
	}
	if _, err := target.Exec(ctx, `CREATE FUNCTION `+function+`() RETURNS trigger LANGUAGE plpgsql AS $body$
		BEGIN IF NEW.id=7 THEN RAISE EXCEPTION 'fixture batch failure'; END IF; RETURN NEW; END
		$body$`); err != nil {
		t.Fatalf("create failing trigger function: %v", err)
	}
	if _, err := target.Exec(ctx, `CREATE TRIGGER `+trigger+` BEFORE UPDATE ON `+table+` FOR EACH ROW EXECUTE FUNCTION `+function+`() `); err != nil {
		t.Fatalf("create failing trigger: %v", err)
	}

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	service, err := migration.NewService(migration.NewPGRepository(platform), allowMigrationAccess{}, migration.NewPGAdvisoryLocker(platform),
		migration.NewPGExecutorWithLogger(integrationTargetProvider{url: targetURL}, logger), approveAll{}, migration.RealClock{},
		logger, migration.ServiceConfig{ExecutionTimeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	selector := `SELECT id FROM ` + table + ` WHERE id>$1 ORDER BY id LIMIT $2`
	up := `UPDATE ` + table + ` SET value=value+10 WHERE id>$1 AND id<=$2`
	down := `UPDATE ` + table + ` SET value=value-10 WHERE id>$1 AND id<=$2`
	batchSize := 3
	principal := auth.Principal{UserID: userID, Username: username}
	entry, err := service.CreateDraftWithDefinition(ctx, principal, databaseID, migration.Definition{
		Name: "increment rows in batches", SQL: up, DownSQL: &down,
		ExecutionMode: migration.ExecutionModeBatch, BatchSelectSQL: &selector,
		BatchSize: &batchSize, BatchCursorStart: 0,
	})
	if err != nil {
		t.Fatalf("create batch definition: %v", err)
	}

	partial, err := service.Execute(ctx, principal, databaseID, entry.ID)
	if err != nil || partial.Status != migration.StatusPartial || partial.IsActive || partial.LatestExecutionID == nil {
		t.Fatalf("partial batch result = %#v, error %v", partial, err)
	}
	progress, err := service.GetBatchProgress(ctx, principal, databaseID, *partial.LatestExecutionID)
	if err != nil || progress.CommittedBatches != 2 || progress.CommittedRows != 6 || progress.LastCursor != 6 || progress.Status != "partial" {
		t.Fatalf("partial progress = %#v, error %v", progress, err)
	}
	failedAttempt, err := service.GetExecution(ctx, principal, databaseID, *partial.LatestExecutionID)
	if err != nil || len(failedAttempt.Events) == 0 {
		t.Fatalf("load failed batch history = %#v, error %v", failedAttempt, err)
	}
	var failure *migration.ExecutionEvent
	for index := range failedAttempt.Events {
		if failedAttempt.Events[index].Event == migration.EventBatchFailed {
			failure = &failedAttempt.Events[index]
		}
	}
	if failure == nil || failure.QuerySQL != up || failure.BatchNumber == nil ||
		*failure.BatchNumber != 3 || failure.CursorStart == nil || *failure.CursorStart != 6 ||
		failure.CursorEnd == nil || *failure.CursorEnd != 7 || len(failure.QueryParameters) != 2 ||
		failure.QueryParameters[0] != 6 || failure.QueryParameters[1] != 7 {
		t.Fatalf("failed batch event missing SQL/range details: %#v", failure)
	}
	for _, field := range []string{"event=migration_batch_failed", `query="UPDATE ` + table,
		`parameters="[6 7]"`, "batch_number=3", "cursor_start=6", "cursor_end=7"} {
		if !strings.Contains(logs.String(), field) {
			t.Fatalf("failed batch log is missing %q: %s", field, logs.String())
		}
	}
	var committedRows, doubleApplied int
	if err := target.QueryRow(ctx, `SELECT count(*) FILTER (WHERE value=10),count(*) FILTER (WHERE value<>0 AND value<>10) FROM `+table).Scan(&committedRows, &doubleApplied); err != nil {
		t.Fatalf("inspect partial target: %v", err)
	}
	if committedRows != 6 || doubleApplied != 0 {
		t.Fatalf("partial target values: committed=%d invalid=%d", committedRows, doubleApplied)
	}
	if _, err := target.Exec(ctx, `DROP TRIGGER `+trigger+` ON `+table); err != nil {
		t.Fatalf("remove temporary failure trigger: %v", err)
	}
	if _, err := target.Exec(ctx, `DROP FUNCTION `+function+`() `); err != nil {
		t.Fatalf("remove temporary failure function: %v", err)
	}

	resumed, err := service.Retry(ctx, principal, databaseID, entry.ID)
	if err != nil || resumed.Status != migration.StatusSucceeded || !resumed.IsActive {
		t.Fatalf("resume batch execution = %#v, error %v", resumed, err)
	}
	progress, err = service.GetBatchProgress(ctx, principal, databaseID, *resumed.LatestExecutionID)
	if err != nil || progress.CommittedBatches != 3 || progress.CommittedRows != 7 || progress.LastCursor != 7 {
		t.Fatalf("resumed progress = %#v, error %v", progress, err)
	}
	if err := target.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE value=10`).Scan(&committedRows); err != nil || committedRows != 7 {
		t.Fatalf("resumed rows = %d, error %v", committedRows, err)
	}

	reverted, err := service.Rollback(ctx, principal, databaseID, entry.ID)
	if err != nil || reverted.Status != migration.StatusRolledBack || reverted.IsActive {
		t.Fatalf("batch rollback = %#v, error %v", reverted, err)
	}
	progress, err = service.GetBatchProgress(ctx, principal, databaseID, *reverted.LatestExecutionID)
	if err != nil || progress.CommittedBatches != 3 || progress.CommittedRows != 7 {
		t.Fatalf("revert progress = %#v, error %v", progress, err)
	}
	if err := target.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE value<>0`).Scan(&committedRows); err != nil || committedRows != 0 {
		t.Fatalf("rows remaining after reverse batch rollback = %d, error %v", committedRows, err)
	}
	forwarded, err := service.Forward(ctx, principal, databaseID, entry.ID)
	if err != nil || forwarded.Status != migration.StatusSucceeded || !forwarded.IsActive {
		t.Fatalf("forward original batch migration = %#v, error %v", forwarded, err)
	}

	if _, err := target.Exec(ctx, `CREATE FUNCTION `+function+`() RETURNS trigger LANGUAGE plpgsql AS $body$
		BEGIN IF NEW.id=7 THEN RAISE EXCEPTION 'fixture batch failure'; END IF; RETURN NEW; END
		$body$`); err != nil {
		t.Fatalf("create second partial batch function: %v", err)
	}
	if _, err := target.Exec(ctx, `CREATE TRIGGER `+trigger+` BEFORE UPDATE ON `+table+` FOR EACH ROW EXECUTE FUNCTION `+function+`() `); err != nil {
		t.Fatalf("create second partial batch trigger: %v", err)
	}
	partialEntry, err := service.CreateDraftWithDefinition(ctx, principal, databaseID, migration.Definition{
		Name: "rollback directly from partial", SQL: up, DownSQL: &down,
		ExecutionMode: migration.ExecutionModeBatch, BatchSelectSQL: &selector,
		BatchSize: &batchSize, BatchCursorStart: 0,
	})
	if err != nil {
		t.Fatalf("create partial cancellation migration: %v", err)
	}
	partialApply, err := service.Execute(ctx, principal, databaseID, partialEntry.ID)
	if err != nil || partialApply.Status != migration.StatusPartial {
		t.Fatalf("prepare partial cancellation = %#v, error %v", partialApply, err)
	}
	if _, err := target.Exec(ctx, `DROP TRIGGER `+trigger+` ON `+table); err != nil {
		t.Fatalf("remove second partial batch trigger: %v", err)
	}
	if _, err := target.Exec(ctx, `DROP FUNCTION `+function+`() `); err != nil {
		t.Fatalf("remove second partial batch function: %v", err)
	}
	if _, err := target.Exec(ctx, `CREATE FUNCTION `+function+`() RETURNS trigger LANGUAGE plpgsql AS $body$
		BEGIN IF NEW.id=2 THEN RAISE EXCEPTION 'fixture rollback failure'; END IF; RETURN NEW; END
		$body$`); err != nil {
		t.Fatalf("create rollback failure function: %v", err)
	}
	if _, err := target.Exec(ctx, `CREATE TRIGGER `+trigger+` BEFORE UPDATE ON `+table+` FOR EACH ROW EXECUTE FUNCTION `+function+`() `); err != nil {
		t.Fatalf("create rollback failure trigger: %v", err)
	}
	partialRollback, err := service.Rollback(ctx, principal, databaseID, partialEntry.ID)
	if err != nil || partialRollback.Status != migration.StatusPartiallyReverted || !partialRollback.IsActive {
		t.Fatalf("rollback from partial apply = %#v, error %v", partialRollback, err)
	}
	rollbackProgress, err := service.GetBatchProgress(ctx, principal, databaseID, *partialRollback.LatestExecutionID)
	if err != nil || rollbackProgress.CommittedBatches != 1 || rollbackProgress.CommittedRows != 3 {
		t.Fatalf("partial rollback progress = %#v, error %v", rollbackProgress, err)
	}
	if _, err := target.Exec(ctx, `DROP TRIGGER `+trigger+` ON `+table); err != nil {
		t.Fatalf("remove rollback failure trigger: %v", err)
	}
	if _, err := target.Exec(ctx, `DROP FUNCTION `+function+`() `); err != nil {
		t.Fatalf("remove rollback failure function: %v", err)
	}
	rolledBack, err := service.Rollback(ctx, principal, databaseID, partialEntry.ID)
	if err != nil || rolledBack.Status != migration.StatusRolledBack || rolledBack.IsActive {
		t.Fatalf("resume rollback from partial apply = %#v, error %v", rolledBack, err)
	}
	var restoredRows int
	if err := target.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE value=10`).Scan(&restoredRows); err != nil || restoredRows != 7 {
		t.Fatalf("rows preserved from earlier forward migration = %d, error %v", restoredRows, err)
	}
	if err := target.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE value<>10`).Scan(&committedRows); err != nil || committedRows != 0 {
		t.Fatalf("rows remaining after resumed partial rollback = %d, error %v", committedRows, err)
	}
}
