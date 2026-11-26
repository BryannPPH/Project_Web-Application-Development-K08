package integration_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/migration"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb"
)

type allowMigrationAccess struct{}

func (allowMigrationAccess) RequireDatabaseView(context.Context, auth.Principal, string) error {
	return nil
}
func (allowMigrationAccess) RequireDatabaseEdit(context.Context, auth.Principal, string) error {
	return nil
}

type integrationTargetProvider struct{ url string }
type integrationTargetConnection struct{ connection *pgx.Conn }
type integrationTargetTransaction struct{ transaction pgx.Tx }

func (p integrationTargetProvider) Open(ctx context.Context, _ string) (targetdb.Connection, error) {
	connection, err := pgx.Connect(ctx, p.url)
	if err != nil {
		return nil, err
	}
	return integrationTargetConnection{connection: connection}, nil
}
func (c integrationTargetConnection) Engine() targetdb.Engine { return targetdb.EnginePostgreSQL }
func (c integrationTargetConnection) Query(ctx context.Context, sql string, args ...any) (targetdb.Rows, error) {
	return c.connection.Query(ctx, sql, args...)
}
func (c integrationTargetConnection) QueryRow(ctx context.Context, sql string, args ...any) targetdb.Row {
	return c.connection.QueryRow(ctx, sql, args...)
}
func (c integrationTargetConnection) Exec(ctx context.Context, sql string, args ...any) (targetdb.CommandResult, error) {
	return c.connection.Exec(ctx, sql, args...)
}
func (c integrationTargetConnection) Begin(ctx context.Context, options ...targetdb.TxOptions) (targetdb.Transaction, error) {
	var pgOptions pgx.TxOptions
	if len(options) > 0 && options[0].ReadOnly {
		pgOptions.AccessMode = pgx.ReadOnly
	}
	tx, err := c.connection.BeginTx(ctx, pgOptions)
	if err != nil {
		return nil, err
	}
	return integrationTargetTransaction{transaction: tx}, nil
}
func (c integrationTargetConnection) Close(ctx context.Context) error { return c.connection.Close(ctx) }

func (t integrationTargetTransaction) Query(ctx context.Context, sql string, args ...any) (targetdb.Rows, error) {
	return t.transaction.Query(ctx, sql, args...)
}
func (t integrationTargetTransaction) QueryRow(ctx context.Context, sql string, args ...any) targetdb.Row {
	return t.transaction.QueryRow(ctx, sql, args...)
}
func (t integrationTargetTransaction) Exec(ctx context.Context, sql string, args ...any) (targetdb.CommandResult, error) {
	return t.transaction.Exec(ctx, sql, args...)
}
func (t integrationTargetTransaction) ExecScript(ctx context.Context, sql string) (targetdb.CommandResult, error) {
	return t.transaction.Exec(ctx, sql, pgx.QueryExecModeSimpleProtocol)
}
func (t integrationTargetTransaction) Commit(ctx context.Context) error {
	return t.transaction.Commit(ctx)
}
func (t integrationTargetTransaction) Rollback(ctx context.Context) error {
	return t.transaction.Rollback(ctx)
}

func TestMigrationExecutionCommitAndRollback(t *testing.T) {
	platformURL := os.Getenv("TEST_PLATFORM_DATABASE_URL")
	targetURL := os.Getenv("TEST_TARGET_DATABASE_URL")
	if platformURL == "" {
		t.Skip("platform test database URL is required")
	}
	if targetURL == "" {
		targetURL = loadTargetSettings(t).url()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	t.Cleanup(func() {
		if err := target.Close(context.Background()); err != nil {
			t.Errorf("close target database: %v", err)
		}
	})

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	table := "migration_atomic_" + suffix
	username := "migration_user_" + suffix
	databaseName := "migration_database_" + suffix
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := target.Exec(cleanupCtx, `DROP TABLE IF EXISTS `+table); err != nil {
			t.Errorf("cleanup target table: %v", err)
		}
	})

	var userID, databaseID string
	if err := platform.QueryRow(ctx, `INSERT INTO users (username,password_hash) VALUES ($1,'test-only') RETURNING id::text`, username).Scan(&userID); err != nil {
		t.Fatalf("create migration user: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := platform.Exec(cleanupCtx, `DELETE FROM users WHERE id=$1`, userID); err != nil {
			t.Errorf("cleanup migration user: %v", err)
		}
	})

	if err := platform.QueryRow(ctx, `INSERT INTO managed_databases
		(name,host,database_name,username,credential_ciphertext,ssl_mode)
		VALUES ($1,'localhost','target_db','target_owner',$2,'disable') RETURNING id::text`, databaseName, []byte("test-only")).Scan(&databaseID); err != nil {
		t.Fatalf("create managed database: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		cleanupTx, err := platform.Begin(cleanupCtx)
		if err != nil {
			t.Errorf("begin migration fixture cleanup: %v", err)
			return
		}
		defer func() { _ = cleanupTx.Rollback(context.Background()) }()
		for _, statement := range []string{
			`ALTER TABLE migration_execution_events DISABLE TRIGGER migration_execution_events_append_only`,
			`ALTER TABLE migration_execution_attempts DISABLE TRIGGER migration_execution_attempts_append_only`,
		} {
			if _, err := cleanupTx.Exec(cleanupCtx, statement); err != nil {
				t.Errorf("cleanup trigger setup %q: %v", statement, err)
				return
			}
		}
		if _, err := cleanupTx.Exec(cleanupCtx, `DELETE FROM migration_execution_events WHERE execution_id IN (
			SELECT id FROM migration_execution_attempts WHERE database_id=$1)`, databaseID); err != nil {
			t.Errorf("cleanup migration execution events: %v", err)
			return
		}
		for {
			command, err := cleanupTx.Exec(cleanupCtx, `DELETE FROM migration_execution_attempts a
				WHERE a.database_id=$1 AND NOT EXISTS (
					SELECT 1 FROM migration_execution_attempts child WHERE child.related_execution_id=a.id)`, databaseID)
			if err != nil {
				t.Errorf("cleanup migration execution attempts: %v", err)
				return
			}
			if command.RowsAffected() == 0 {
				break
			}
		}
		if _, err := cleanupTx.Exec(cleanupCtx, `DELETE FROM migration_entries WHERE database_id=$1`, databaseID); err != nil {
			t.Errorf("cleanup migration entries: %v", err)
			return
		}
		for _, statement := range []string{
			`ALTER TABLE migration_execution_events ENABLE TRIGGER migration_execution_events_append_only`,
			`ALTER TABLE migration_execution_attempts ENABLE TRIGGER migration_execution_attempts_append_only`,
		} {
			if _, err := cleanupTx.Exec(cleanupCtx, statement); err != nil {
				t.Errorf("cleanup trigger restore %q: %v", statement, err)
				return
			}
		}
		if err := cleanupTx.Commit(cleanupCtx); err != nil {
			t.Errorf("commit migration fixture cleanup: %v", err)
			return
		}
		if _, err := platform.Exec(cleanupCtx, `DELETE FROM managed_databases WHERE id=$1`, databaseID); err != nil {
			t.Errorf("cleanup managed database: %v", err)
		}
	})

	repository := migration.NewPGRepository(platform)
	service, err := migration.NewService(
		repository,
		allowMigrationAccess{},
		migration.NewPGAdvisoryLocker(platform),
		migration.NewPGExecutor(integrationTargetProvider{url: targetURL}),
		approveAll{},
		migration.RealClock{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		migration.ServiceConfig{ExecutionTimeout: 10 * time.Second},
	)
	if err != nil {
		t.Fatal(err)
	}
	principal := auth.Principal{UserID: userID, Username: username}

	successSQL := fmt.Sprintf(`CREATE TABLE %s(id bigint PRIMARY KEY); INSERT INTO %s VALUES (1)`, table, table)
	success, err := service.CreateDraft(ctx, principal, databaseID, "create atomic fixture", successSQL)
	if err != nil {
		t.Fatalf("create successful draft: %v", err)
	}
	success, err = service.Execute(ctx, principal, databaseID, success.ID)
	if err != nil || success.Status != migration.StatusSucceeded {
		t.Fatalf("execute successful migration = status %s, error %v", success.Status, err)
	}

	failureSQL := fmt.Sprintf(`INSERT INTO %s VALUES (2); INSERT INTO table_that_does_not_exist VALUES (1)`, table)
	failure, err := service.CreateDraft(ctx, principal, databaseID, "rollback fixture", failureSQL)
	if err != nil {
		t.Fatalf("create failing draft: %v", err)
	}
	failure, err = service.Execute(ctx, principal, databaseID, failure.ID)
	if err != nil || failure.Status != migration.StatusFailed {
		t.Fatalf("execute failing migration = status %s, error %v", failure.Status, err)
	}

	var rows int
	if err := target.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&rows); err != nil {
		t.Fatalf("inspect target: %v", err)
	}
	if rows != 1 {
		t.Fatalf("target row count = %d, want 1 after rollback", rows)
	}

	page, err := service.List(ctx, principal, databaseID, migration.ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	if page.Total != 2 || page.Items[0].Version != 2 || page.Items[1].Version != 1 {
		t.Fatalf("unexpected linear history: %#v", page)
	}

	orphaned, err := service.CreateDraft(ctx, principal, databaseID, "interrupted fixture", "SELECT 1")
	if err != nil {
		t.Fatalf("create interrupted draft: %v", err)
	}
	if _, err := repository.MarkRunning(ctx, databaseID, orphaned.ID, userID, orphaned.Revision, time.Now().UTC()); err != nil {
		t.Fatalf("mark interrupted fixture running: %v", err)
	}
	if _, err := migration.NewModule(
		ctx,
		platform,
		allowMigrationAccess{},
		approveAll{},
		integrationTargetProvider{url: targetURL},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		migration.ServiceConfig{ExecutionTimeout: 10 * time.Second},
	); err != nil {
		t.Fatalf("initialize migration module recovery: %v", err)
	}
	stillRunning, err := repository.Get(ctx, databaseID, orphaned.ID)
	if err != nil || stillRunning.Status != migration.StatusRunning {
		t.Fatalf("API module startup changed a live migration lease: %#v, error %v", stillRunning, err)
	}
	if recoveredCount, err := repository.RecoverRunning(ctx, time.Now().Add(time.Second),
		"migration execution was interrupted; manual reconciliation is required"); err != nil || recoveredCount != 1 {
		t.Fatalf("explicit stale migration recovery count = %d, error %v; want 1", recoveredCount, err)
	}
	recovered, err := repository.Get(ctx, databaseID, orphaned.ID)
	if err != nil || recovered.Status != migration.StatusInterrupted || recovered.ErrorMessage == nil {
		t.Fatalf("recovered migration = %#v, error %v", recovered, err)
	}

	blocked, err := service.CreateDraft(ctx, principal, databaseID, "blocked by interrupted fixture", "SELECT 2")
	if err != nil {
		t.Fatalf("create draft after interrupted migration: %v", err)
	}
	if _, err := service.Execute(ctx, principal, databaseID, blocked.ID); !errors.Is(err, migration.ErrWrongOrder) {
		t.Fatalf("execute migration after interrupted migration error = %v, want ErrWrongOrder", err)
	}
	stillDraft, err := repository.Get(ctx, databaseID, blocked.ID)
	if err != nil || stillDraft.Status != migration.StatusDraft {
		t.Fatalf("blocked migration status = %s, error %v; want draft", stillDraft.Status, err)
	}

	reconciled, err := service.Reconcile(ctx, principal, databaseID, orphaned.ID,
		"Verified target state after the interrupted migration; no partial SQL was applied.")
	if err != nil || reconciled.Status != migration.StatusReconciled || reconciled.ReconciledFrom == nil ||
		*reconciled.ReconciledFrom != migration.StatusInterrupted || reconciled.ReconciledBy == nil ||
		*reconciled.ReconciledBy != userID || reconciled.ReconciledAt == nil || reconciled.ReconciliationNote == nil {
		t.Fatalf("reconciled migration = %#v, error %v", reconciled, err)
	}
	if reconciled.ReconciledByUsername == nil || *reconciled.ReconciledByUsername != username {
		t.Fatalf("reconciled by username = %v, want %q", reconciled.ReconciledByUsername, username)
	}
	if next, executeErr := service.Execute(ctx, principal, databaseID, blocked.ID); executeErr != nil || next.Status != migration.StatusSucceeded {
		t.Fatalf("execute after reconciliation = status %s, error %v", next.Status, executeErr)
	}

	const concurrentDrafts = 8
	versions := make(chan int64, concurrentDrafts)
	errorsFound := make(chan error, concurrentDrafts)
	var wait sync.WaitGroup
	for index := 0; index < concurrentDrafts; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			entry, createErr := repository.CreateDraft(ctx, databaseID, fmt.Sprintf("concurrent %d", index), "SELECT 1", userID, time.Now().UTC())
			if createErr != nil {
				errorsFound <- createErr
				return
			}
			versions <- entry.Version
		}(index)
	}
	wait.Wait()
	close(versions)
	close(errorsFound)
	for createErr := range errorsFound {
		t.Fatalf("concurrent create: %v", createErr)
	}
	seen := make(map[int64]bool, concurrentDrafts)
	for version := range versions {
		if seen[version] {
			t.Fatalf("duplicate concurrent version %d", version)
		}
		seen[version] = true
	}
	if len(seen) != concurrentDrafts {
		t.Fatalf("allocated %d concurrent versions, want %d", len(seen), concurrentDrafts)
	}
}

func TestMigrationHistoryRollbackForwardAndImmutableSQL(t *testing.T) {
	platformURL := os.Getenv("TEST_PLATFORM_DATABASE_URL")
	targetURL := os.Getenv("TEST_TARGET_DATABASE_URL")
	if platformURL == "" {
		t.Skip("platform test database URL is required")
	}
	if targetURL == "" {
		targetURL = loadTargetSettings(t).url()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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
	table := "migration_reversible_" + suffix
	username, databaseName := "history_user_"+suffix, "history_db_"+suffix
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
		_, _ = target.Exec(cleanupCtx, `DROP TABLE IF EXISTS `+table+`_next`)
		_, _ = target.Exec(cleanupCtx, `DROP TABLE IF EXISTS `+table)
		cleanupTx, beginErr := platform.Begin(cleanupCtx)
		if beginErr != nil {
			t.Errorf("begin execution ledger cleanup: %v", beginErr)
			return
		}
		defer func() { _ = cleanupTx.Rollback(context.Background()) }()
		for _, statement := range []string{
			`ALTER TABLE migration_execution_events DISABLE TRIGGER migration_execution_events_append_only`,
			`ALTER TABLE migration_execution_attempts DISABLE TRIGGER migration_execution_attempts_append_only`,
		} {
			if _, err := cleanupTx.Exec(cleanupCtx, statement); err != nil {
				t.Errorf("disable history trigger: %v", err)
				return
			}
		}
		if _, err := cleanupTx.Exec(cleanupCtx, `DELETE FROM migration_execution_events WHERE execution_id IN (
			SELECT id FROM migration_execution_attempts WHERE database_id=$1)`, databaseID); err != nil {
			t.Errorf("delete execution events: %v", err)
			return
		}
		for {
			command, err := cleanupTx.Exec(cleanupCtx, `DELETE FROM migration_execution_attempts a
				WHERE a.database_id=$1 AND NOT EXISTS (SELECT 1 FROM migration_execution_attempts child WHERE child.related_execution_id=a.id)`, databaseID)
			if err != nil {
				t.Errorf("delete execution attempts: %v", err)
				return
			}
			if command.RowsAffected() == 0 {
				break
			}
		}
		if _, err := cleanupTx.Exec(cleanupCtx, `DELETE FROM migration_entries WHERE database_id=$1`, databaseID); err != nil {
			t.Errorf("delete migration definitions: %v", err)
			return
		}
		for _, statement := range []string{
			`ALTER TABLE migration_execution_events ENABLE TRIGGER migration_execution_events_append_only`,
			`ALTER TABLE migration_execution_attempts ENABLE TRIGGER migration_execution_attempts_append_only`,
		} {
			if _, err := cleanupTx.Exec(cleanupCtx, statement); err != nil {
				t.Errorf("enable history trigger: %v", err)
				return
			}
		}
		if err := cleanupTx.Commit(cleanupCtx); err != nil {
			t.Errorf("commit execution ledger cleanup: %v", err)
			return
		}
		if _, err := platform.Exec(cleanupCtx, `DELETE FROM managed_databases WHERE id=$1`, databaseID); err != nil {
			t.Errorf("delete managed database: %v", err)
		}
		if _, err := platform.Exec(cleanupCtx, `DELETE FROM users WHERE id=$1`, userID); err != nil {
			t.Errorf("delete migration user: %v", err)
		}
	})

	repository := migration.NewPGRepository(platform)
	service, err := migration.NewService(repository, allowMigrationAccess{}, migration.NewPGAdvisoryLocker(platform),
		migration.NewPGExecutor(integrationTargetProvider{url: targetURL}), approveAll{}, migration.RealClock{},
		slog.New(slog.NewTextHandler(io.Discard, nil)), migration.ServiceConfig{ExecutionTimeout: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	principal := auth.Principal{UserID: userID, Username: username}
	up := fmt.Sprintf(`CREATE TABLE %s(id bigint PRIMARY KEY)`, table)
	down := fmt.Sprintf(`DROP TABLE %s`, table)
	entry, err := service.CreateDraftWithDefinition(ctx, principal, databaseID, migration.Definition{
		Name: "reversible table", SQL: up, DownSQL: &down, ExecutionMode: migration.ExecutionModeSingle,
	})
	if err != nil {
		t.Fatalf("create reversible definition: %v", err)
	}
	entry, err = service.Execute(ctx, principal, databaseID, entry.ID)
	if err != nil || entry.Status != migration.StatusSucceeded || !entry.IsActive || entry.LatestExecutionID == nil {
		t.Fatalf("apply migration = %#v, error %v", entry, err)
	}
	applyID := *entry.LatestExecutionID
	rolledBack, err := service.Rollback(ctx, principal, databaseID, entry.ID)
	if err != nil || rolledBack.Status != migration.StatusRolledBack || rolledBack.IsActive {
		t.Fatalf("rollback migration = %#v, error %v", rolledBack, err)
	}
	rollbackID := *rolledBack.LatestExecutionID
	forwarded, err := service.Forward(ctx, principal, databaseID, entry.ID)
	if err != nil || forwarded.Status != migration.StatusSucceeded || !forwarded.IsActive {
		t.Fatalf("forward migration = %#v, error %v", forwarded, err)
	}
	forwardID := *forwarded.LatestExecutionID
	for executionID, want := range map[string]struct {
		action  migration.ExecutionAction
		sql     string
		related string
	}{
		applyID:    {migration.ActionApply, up, ""},
		rollbackID: {migration.ActionRollback, down, applyID},
		forwardID:  {migration.ActionForward, up, rollbackID},
	} {
		attempt, err := repository.GetHistory(ctx, databaseID, executionID)
		if err != nil {
			t.Fatalf("get execution %s: %v", executionID, err)
		}
		if attempt.Action != want.action || attempt.SQLSnapshot != want.sql || attempt.Status != string(migration.StatusSucceeded) {
			t.Fatalf("execution %s = %#v, want action=%s SQL=%q status=succeeded", executionID, attempt, want.action, want.sql)
		}
		if want.related == "" && attempt.RelatedExecutionID != nil ||
			want.related != "" && (attempt.RelatedExecutionID == nil || *attempt.RelatedExecutionID != want.related) {
			t.Fatalf("execution %s related ID=%v, want %q", executionID, attempt.RelatedExecutionID, want.related)
		}
	}
	filtered, err := repository.ListHistory(ctx, databaseID, migration.HistoryOptions{Limit: 10, ExecutionID: rollbackID})
	if err != nil || filtered.Total != 1 || len(filtered.Items) != 1 || filtered.Items[0].ID != rollbackID {
		t.Fatalf("execution ID filter = %#v, error %v", filtered, err)
	}
	allHistory, err := repository.ListHistory(ctx, databaseID, migration.HistoryOptions{Limit: 1})
	if err != nil || allHistory.Total != 3 || len(allHistory.Items) != 1 || allHistory.Items[0].ID != forwardID {
		t.Fatalf("first history page = %#v, error %v", allHistory, err)
	}
	hiddenHistory, err := repository.ListHistory(ctx, databaseID, migration.HistoryOptions{Limit: 1, HideRollback: true})
	if err != nil || hiddenHistory.Total != 1 || len(hiddenHistory.Items) != 1 || hiddenHistory.Items[0].ID != forwardID {
		t.Fatalf("server-side Hide rollback across pages = %#v, error %v", hiddenHistory, err)
	}

	laterTable := table + "_later"
	laterUp, laterDown := fmt.Sprintf(`CREATE TABLE %s(id bigint PRIMARY KEY)`, laterTable), fmt.Sprintf(`DROP TABLE %s`, laterTable)
	laterEntry, err := service.CreateDraftWithDefinition(ctx, principal, databaseID, migration.Definition{
		Name: "later reversible table", SQL: laterUp, DownSQL: &laterDown, ExecutionMode: migration.ExecutionModeSingle,
	})
	if err != nil {
		t.Fatalf("create later migration: %v", err)
	}
	if laterEntry, err = service.Execute(ctx, principal, databaseID, laterEntry.ID); err != nil || !laterEntry.IsActive {
		t.Fatalf("apply later migration = %#v, error %v", laterEntry, err)
	}
	if _, err := service.Rollback(ctx, principal, databaseID, entry.ID); !errors.Is(err, migration.ErrWrongOrder) {
		t.Fatalf("rollback of earlier migration error=%v, want order conflict", err)
	}
	if laterEntry, err = service.Rollback(ctx, principal, databaseID, laterEntry.ID); err != nil || laterEntry.Status != migration.StatusRolledBack {
		t.Fatalf("rollback later migration = %#v, error %v", laterEntry, err)
	}
	rolledBackForward, err := service.Rollback(ctx, principal, databaseID, entry.ID)
	if err != nil || rolledBackForward.Status != migration.StatusRolledBack || rolledBackForward.IsActive {
		t.Fatalf("rollback forwarded migration = %#v, error %v", rolledBackForward, err)
	}
	allCyclesHistory, err := repository.ListHistory(ctx, databaseID, migration.HistoryOptions{Limit: 10})
	if err != nil || allCyclesHistory.Total != 6 || len(allCyclesHistory.Items) != 6 {
		t.Fatalf("history after repeated forward/rollback cycles = %#v, error %v", allCyclesHistory, err)
	}
	hiddenCyclesHistory, err := repository.ListHistory(ctx, databaseID, migration.HistoryOptions{Limit: 10, HideRollback: true})
	if err != nil || hiddenCyclesHistory.Total != 0 || len(hiddenCyclesHistory.Items) != 0 {
		t.Fatalf("Hide rollback after repeated forward/rollback cycles = %#v, error %v", hiddenCyclesHistory, err)
	}
	forwardedAgain, err := service.Forward(ctx, principal, databaseID, entry.ID)
	if err != nil || forwardedAgain.Status != migration.StatusSucceeded || !forwardedAgain.IsActive {
		t.Fatalf("forward migration after repeated rollback = %#v, error %v", forwardedAgain, err)
	}
	forwardedLater, err := service.Forward(ctx, principal, databaseID, laterEntry.ID)
	if err != nil || forwardedLater.Status != migration.StatusSucceeded || !forwardedLater.IsActive {
		t.Fatalf("forward later migration = %#v, error %v", forwardedLater, err)
	}
	if _, err := platform.Exec(ctx, `UPDATE migration_entries SET sql_text='SELECT 42' WHERE id=$1`, entry.ID); err == nil {
		t.Fatal("executed migration definition was mutable")
	}

	nextDown := "DROP TABLE does_not_exist"
	next, err := service.CreateDraftWithDefinition(ctx, principal, databaseID, migration.Definition{
		Name: "failed rollback", SQL: fmt.Sprintf(`CREATE TABLE %s_next(id bigint)`, table),
		DownSQL: &nextDown, ExecutionMode: migration.ExecutionModeSingle,
	})
	if err != nil {
		t.Fatalf("create second migration: %v", err)
	}
	if next, err = service.Execute(ctx, principal, databaseID, next.ID); err != nil || !next.IsActive {
		t.Fatalf("apply second migration = %#v, error %v", next, err)
	}
	failedRollback, err := service.Rollback(ctx, principal, databaseID, next.ID)
	if err != nil || failedRollback.Status != migration.StatusSucceeded || !failedRollback.IsActive {
		t.Fatalf("failed rollback changed active position: entry=%#v error=%v", failedRollback, err)
	}
}

// approveAll: gerbang approval yang selalu mengizinkan (test ini fokus ke eksekusi).
type approveAll struct{}

func (approveAll) RequireApproved(context.Context, string, string, string, int64) error { return nil }
