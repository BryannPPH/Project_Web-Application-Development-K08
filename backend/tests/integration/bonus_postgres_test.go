package integration_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz/authztest"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/migration"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/schemadiff"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb"
)

const (
	bonusPostgresID = "22222222-2222-4222-8222-222222222222"
	bonusMySQLID    = "33333333-3333-4333-8333-333333333333"
)

// mapResolver mengembalikan konfigurasi berbeda per databaseID.
type mapResolver map[string]targetdb.ConnectionConfig

func (r mapResolver) ResolveConnection(_ context.Context, databaseID string) (targetdb.ConnectionConfig, error) {
	config, ok := r[databaseID]
	if !ok {
		return targetdb.ConnectionConfig{}, targetdb.ErrDatabaseNotFound
	}
	return config, nil
}

func postgresTargetConfig(t *testing.T) targetdb.ConnectionConfig {
	t.Helper()
	port, _ := strconv.Atoi(os.Getenv("TEST_TARGET_DB_PORT"))
	config := targetdb.ConnectionConfig{
		DatabaseID: bonusPostgresID,
		Engine:     targetdb.EnginePostgreSQL,
		Host:       os.Getenv("TEST_TARGET_DB_HOST"),
		Port:       port,
		Database:   os.Getenv("TEST_TARGET_DB_NAME"),
		Username:   os.Getenv("TEST_TARGET_DB_USER"),
		Password:   os.Getenv("TEST_TARGET_DB_PASSWORD"),
		SSLMode:    "disable",
	}
	if config.Host == "" || config.Port == 0 || config.Database == "" || config.Username == "" || config.Password == "" {
		t.Skip("TEST_TARGET_DB_* is not set")
	}
	return config
}

// execAll menjalankan beberapa statement langsung pada target (fixture).
func execAll(t *testing.T, provider targetdb.Provider, databaseID string, statements ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	connection, err := provider.Open(ctx, databaseID)
	if err != nil {
		t.Fatalf("open target: %v", err)
	}
	defer func() { _ = connection.Close(context.Background()) }()
	for _, statement := range statements {
		if _, err := connection.Exec(ctx, statement); err != nil {
			t.Fatalf("fixture %q: %v", statement, err)
		}
	}
}

type bonusUsers struct {
	editor, viewer, outsider auth.Principal
	policy                   *authz.Policy
}

func newBonusUsers(databaseIDs ...string) bonusUsers {
	store := authztest.NewStore()
	users := bonusUsers{
		editor:   store.AddUser("bonus-editor", auth.PlatformRoleUser),
		viewer:   store.AddUser("bonus-viewer", auth.PlatformRoleUser),
		outsider: store.AddUser("bonus-outsider", auth.PlatformRoleUser),
	}
	for _, databaseID := range databaseIDs {
		store.AddDatabase(databaseID, true)
		store.Grant("editors-"+databaseID, databaseID, authz.AccessViewEdit, users.editor.UserID)
		store.Grant("viewers-"+databaseID, databaseID, authz.AccessViewOnly, users.viewer.UserID)
	}
	users.policy = store.Policy()
	return users
}

func TestPostgresExplainAnalyzeNeverPersists(t *testing.T) {
	config := postgresTargetConfig(t)
	provider := targetdb.NewProvider(mapResolver{config.DatabaseID: config}, 5*time.Second, nil)
	schema := fmt.Sprintf("bonus_explain_%d", time.Now().UnixNano())
	execAll(t, provider, config.DatabaseID,
		"CREATE SCHEMA "+schema,
		"CREATE TABLE "+schema+".items (id serial PRIMARY KEY, code text NOT NULL, qty int NOT NULL)",
		"INSERT INTO "+schema+".items (code, qty) SELECT 'c' || g, g % 10 FROM generate_series(1, 5000) g",
		"ANALYZE "+schema+".items",
	)
	t.Cleanup(func() { execAll(t, provider, config.DatabaseID, "DROP SCHEMA IF EXISTS "+schema+" CASCADE") })

	users := newBonusUsers(config.DatabaseID)
	service, err := migration.NewExplainService(users.policy, provider, migration.ServiceConfig{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	script := strings.Join([]string{
		"SELECT * FROM " + schema + ".items WHERE qty > 5",
		"UPDATE " + schema + ".items SET qty = 999 WHERE id = 3",
		"CREATE TABLE " + schema + ".tmp (id int)",
		"INSERT INTO " + schema + ".tmp VALUES (1)",
		"SELECT * FROM " + schema + ".does_not_exist",
		"SELECT i.code FROM " + schema + ".items i JOIN " + schema + ".tmp t ON t.id = i.id",
	}, ";\n") + ";"

	// Edge case: user View Only dan user tanpa akses ditolak sebelum koneksi dibuka.
	for _, principal := range []auth.Principal{users.viewer, users.outsider} {
		if _, err := service.Explain(ctx, principal, config.DatabaseID, script, false); err == nil {
			t.Fatalf("%s must not be allowed to explain", principal.Username)
		}
	}
	// Edge case: input kosong, kontrol transaksi, dan string tak tertutup.
	for _, bad := range []struct {
		sql  string
		want error
	}{
		{"   ", migration.ErrInvalidInput},
		{"COMMIT", migration.ErrSQLPolicy},
		{"SELECT 1; BEGIN; SELECT 2", migration.ErrSQLPolicy},
		{"SELECT 'unterminated", migration.ErrSQLPolicy},
	} {
		if _, err := service.Explain(ctx, users.editor, config.DatabaseID, bad.sql, false); !errors.Is(err, bad.want) {
			t.Fatalf("explain %q error = %v, want %v", bad.sql, err, bad.want)
		}
	}

	for _, analyze := range []bool{false, true} {
		result, err := service.Explain(ctx, users.editor, config.DatabaseID, script, analyze)
		if err != nil {
			t.Fatalf("explain (analyze=%v): %v", analyze, err)
		}
		if !result.RolledBack || result.Engine != targetdb.EnginePostgreSQL || len(result.Statements) != 6 {
			t.Fatalf("result = %+v", result)
		}
		kinds := []migration.StatementKind{migration.StatementPlanned, migration.StatementPlanned, migration.StatementExecuted,
			migration.StatementPlanned, migration.StatementFailed, migration.StatementPlanned}
		for index, statement := range result.Statements {
			if statement.Kind != kinds[index] {
				t.Fatalf("analyze=%v statement %d kind = %s (%s), want %s", analyze, index+1, statement.Kind, statement.Error, kinds[index])
			}
		}
		scan := result.Statements[0].Summary
		if !contains(scan.ScanTypes, "Seq Scan") || scan.TotalCost == nil || len(scan.Warnings) == 0 {
			t.Fatalf("seq scan summary = %+v", scan)
		}
		update := result.Statements[1].Summary
		if !contains(update.Indexes, "items_pkey") {
			t.Fatalf("update should use primary key index: %+v", update)
		}
		if join := result.Statements[5].Summary; len(join.JoinTypes) == 0 {
			t.Fatalf("join strategy missing: %+v", join)
		}
		if !strings.Contains(result.Statements[4].Error, "does_not_exist") {
			t.Fatalf("missing relation error = %q", result.Statements[4].Error)
		}
		if analyze {
			if update.ActualRows == nil || update.ExecutionMs == nil || scan.PlanningMs == nil {
				t.Fatalf("analyze must report actual rows/time: %+v", update)
			}
		} else if update.ActualRows != nil {
			t.Fatalf("plain EXPLAIN must not report actual rows: %+v", update)
		}
	}

	// Inti keamanan: EXPLAIN ANALYZE tidak boleh meninggalkan perubahan apa pun.
	connection, err := provider.Open(ctx, config.DatabaseID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close(context.Background()) }()
	var qty int
	if err := connection.QueryRow(ctx, "SELECT qty FROM "+schema+".items WHERE id = 3").Scan(&qty); err != nil || qty == 999 {
		t.Fatalf("UPDATE leaked from EXPLAIN ANALYZE: qty=%d err=%v", qty, err)
	}
	var tmp *string
	if err := connection.QueryRow(ctx, "SELECT to_regclass($1)::text", schema+".tmp").Scan(&tmp); err != nil || tmp != nil {
		t.Fatalf("CREATE TABLE leaked from analysis: %v %v", tmp, err)
	}
}

func TestPostgresSchemaDiffRoundTrip(t *testing.T) {
	config := postgresTargetConfig(t)
	provider := targetdb.NewProvider(mapResolver{config.DatabaseID: config}, 5*time.Second, nil)
	suffix := time.Now().UnixNano()
	desired := fmt.Sprintf("bonus_desired_%d", suffix)
	current := fmt.Sprintf("bonus_current_%d", suffix)
	execAll(t, provider, config.DatabaseID,
		"CREATE SCHEMA "+current,
		"CREATE TABLE "+current+".customers (id serial PRIMARY KEY, email text NOT NULL UNIQUE, legacy text)",
		"CREATE TABLE "+current+".orders (id serial PRIMARY KEY, customer_id int REFERENCES "+current+".customers(id), amount numeric(10,2))",
		"CREATE TABLE "+current+".old_table (id int)",
		"CREATE INDEX orders_amount_idx ON "+current+".orders (amount)",
		"CREATE SCHEMA "+desired,
		"CREATE TABLE "+desired+".customers (id serial PRIMARY KEY, email varchar(255) NOT NULL UNIQUE, name text, created_at timestamptz DEFAULT now() NOT NULL)",
		"CREATE TABLE "+desired+".orders (id serial PRIMARY KEY, customer_id int NOT NULL REFERENCES "+desired+".customers(id) ON DELETE CASCADE, amount numeric(10,2) CHECK (amount >= 0))",
		"CREATE INDEX orders_customer_idx ON "+desired+".orders (customer_id)",
		`CREATE TABLE `+desired+`."Products" (id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, sku text NOT NULL, "order" int DEFAULT 0)`,
	)
	t.Cleanup(func() {
		execAll(t, provider, config.DatabaseID, "DROP SCHEMA IF EXISTS "+desired+" CASCADE", "DROP SCHEMA IF EXISTS "+current+" CASCADE")
	})

	users := newBonusUsers(config.DatabaseID)
	service, err := schemadiff.NewService(users.policy, provider)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	request := schemadiff.Request{
		Source: schemadiff.Ref{DatabaseID: config.DatabaseID, Schema: desired},
		Target: schemadiff.Ref{DatabaseID: config.DatabaseID, Schema: current},
	}

	// Edge case otorisasi & input.
	if _, err := service.Compare(ctx, users.outsider, request); err == nil {
		t.Fatal("user without access must be rejected")
	}
	if _, err := service.Compare(ctx, users.editor, schemadiff.Request{Source: request.Source}); !errors.Is(err, schemadiff.ErrInvalidInput) {
		t.Fatalf("missing target error = %v", err)
	}
	missing := request
	missing.Target.Schema = "schema_yang_tidak_ada"
	if _, err := service.Compare(ctx, users.editor, missing); !errors.Is(err, schemadiff.ErrSchemaNotFound) {
		t.Fatalf("missing schema error = %v", err)
	}
	// Schema dibandingkan dengan dirinya sendiri harus identik.
	self := schemadiff.Request{Source: request.Target, Target: request.Target}
	if result, err := service.Compare(ctx, users.viewer, self); err != nil || !result.Identical || result.SQL != "" {
		t.Fatalf("self diff = %+v, %v", result, err)
	}

	// View Only cukup untuk melihat diff.
	result, err := service.Compare(ctx, users.viewer, request)
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if result.Identical || !result.Executable || result.Summary.Destructive != 2 {
		t.Fatalf("summary = %+v", result.Summary)
	}
	wantKinds := map[schemadiff.ChangeKind]int{}
	for _, change := range result.Changes {
		wantKinds[change.Kind]++
		if len(change.SQL) == 0 {
			t.Fatalf("change without SQL: %+v", change)
		}
	}
	for kind, minimum := range map[schemadiff.ChangeKind]int{
		schemadiff.CreateTable: 1, schemadiff.DropTable: 1, schemadiff.AddColumn: 2, schemadiff.DropColumn: 1,
		schemadiff.AlterColumn: 2, schemadiff.AddConstraint: 2, schemadiff.DropConstraint: 1,
		schemadiff.CreateIndex: 1, schemadiff.DropIndex: 1,
	} {
		if wantKinds[kind] < minimum {
			t.Fatalf("expected at least %d %s, got %d\n%s", minimum, kind, wantKinds[kind], result.SQL)
		}
	}
	if !strings.Contains(result.SQL, `"Products"`) || !strings.Contains(result.SQL, `"order"`) || strings.Contains(result.SQL, desired) {
		t.Fatalf("generated SQL must quote identifiers and target the current schema:\n%s", result.SQL)
	}

	t.Logf("generated SQL:\n%s", result.SQL)

	// Round trip: SQL hasil generate dijalankan lewat migration executor
	// (satu transaksi), lalu diff ulang harus identik.
	executor := migration.NewPGExecutor(provider)
	if outcome := executor.Execute(ctx, config.DatabaseID, result.SQL); outcome.Status != migration.StatusSucceeded {
		t.Fatalf("generated SQL failed: %+v\n%s", outcome, result.SQL)
	}
	again, err := service.Compare(ctx, users.editor, request)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Identical {
		t.Fatalf("schema still differs after applying generated SQL:\n%s", again.SQL)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
