package integration_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
)

type targetSettings struct {
	host     string
	port     int
	database string
	user     string
	password string
}

func loadTargetSettings(t *testing.T) targetSettings {
	t.Helper()
	settings := targetSettings{
		host:     os.Getenv("TEST_TARGET_DB_HOST"),
		database: os.Getenv("TEST_TARGET_DB_NAME"),
		user:     os.Getenv("TEST_TARGET_DB_USER"),
		password: os.Getenv("TEST_TARGET_DB_PASSWORD"),
	}
	port, _ := strconv.Atoi(os.Getenv("TEST_TARGET_DB_PORT"))
	settings.port = port
	if settings.host == "" || settings.port == 0 || settings.database == "" || settings.user == "" || settings.password == "" {
		t.Skip("TEST_TARGET_DB_* is not set")
	}
	return settings
}

func (s targetSettings) url() string {
	return (&url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(s.user, s.password),
		Host:     net.JoinHostPort(s.host, strconv.Itoa(s.port)),
		Path:     s.database,
		RawQuery: "sslmode=disable",
	}).String()
}

// createTargetFixture membuat schema uji di target dengan semua jenis metadata
// wajib, termasuk table beridentifier "aneh".
func createTargetFixture(t *testing.T, settings targetSettings, schema string) *pgx.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	connection, err := pgx.Connect(ctx, settings.url())
	if err != nil {
		t.Fatalf("connect target fixture: %v", err)
	}
	quoted := pgx.Identifier{schema}.Sanitize()
	statements := []string{
		"CREATE SCHEMA " + quoted,
		"CREATE TABLE " + quoted + `.customers (
			id SERIAL PRIMARY KEY,
			email TEXT NOT NULL UNIQUE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		"CREATE TABLE " + quoted + `.orders (
			id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			customer_id INTEGER NOT NULL REFERENCES ` + quoted + `.customers(id),
			amount NUMERIC(12, 2) NOT NULL CHECK (amount >= 0),
			note TEXT,
			payload JSONB,
			ref UUID
		)`,
		"CREATE INDEX orders_customer_idx ON " + quoted + ".orders (customer_id)",
		"CREATE TABLE " + quoted + `."we""ird table" ("odd col" TEXT)`,
		"INSERT INTO " + quoted + `."we""ird table" VALUES ('x'), ('y')`,
		"INSERT INTO " + quoted + ".customers (email) SELECT 'c' || g || '@example.com' FROM generate_series(1, 120) g",
		"INSERT INTO " + quoted + `.orders (customer_id, amount, note, payload, ref)
			VALUES (1, 19.99, NULL, '{"tags":["a"],"n":1}', 'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11')`,
		"CREATE VIEW " + quoted + ".big_orders AS SELECT * FROM " + quoted + ".orders WHERE amount > 10",
	}
	for _, statement := range statements {
		if _, err := connection.Exec(ctx, statement); err != nil {
			_ = connection.Close(ctx)
			t.Fatalf("target fixture %q: %v", statement, err)
		}
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := connection.Exec(cleanupCtx, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Errorf("drop target fixture: %v", err)
		}
		_ = connection.Close(cleanupCtx)
	})
	return connection
}

func TestViewerAgainstRealTargetDatabase(t *testing.T) {
	settings := loadTargetSettings(t)
	harness := newPlatformHarness(t)
	schema := harness.name("viewer")
	target := createTargetFixture(t, settings, schema)

	harness.createUser("admin", auth.PlatformRoleAdmin)
	viewerID := harness.createUser("viewer", auth.PlatformRoleUser)
	editorID := harness.createUser("editor", auth.PlatformRoleUser)
	harness.createUser("outsider", auth.PlatformRoleUser)
	admin := harness.login("admin")

	// Test connection: draft valid & invalid tidak menyimpan apa pun.
	draft := map[string]any{
		"name": harness.name("draft"), "host": settings.host, "port": settings.port,
		"databaseName": settings.database, "username": settings.user, "password": settings.password, "sslMode": "disable",
	}
	if result := harness.call(http.MethodPost, "/api/v1/admin/databases/test-connection", draft, admin, http.StatusOK); result["success"] != true {
		t.Fatalf("valid draft test = %v", result)
	}
	draft["password"] = "definitely-wrong"
	result := harness.call(http.MethodPost, "/api/v1/admin/databases/test-connection", draft, admin, http.StatusOK)
	if result["success"] != false || result["reason"] != "authentication_failed" {
		t.Fatalf("invalid draft test = %v", result)
	}
	var drafts int
	_ = harness.database.QueryRow(context.Background(), `SELECT count(*) FROM managed_databases WHERE name = $1`, harness.name("draft")).Scan(&drafts)
	if drafts != 0 {
		t.Fatal("test-connection persisted a managed database")
	}

	created := harness.call(http.MethodPost, "/api/v1/admin/databases", map[string]any{
		"name": harness.name("target"), "host": settings.host, "port": settings.port,
		"databaseName": settings.database, "username": settings.user, "password": settings.password, "sslMode": "disable",
	}, admin, http.StatusCreated)
	databaseID, _ := objectField(created, "database")["id"].(string)
	if stored := harness.call(http.MethodPost, "/api/v1/admin/databases/"+databaseID+"/test", nil, admin, http.StatusOK); stored["success"] != true {
		t.Fatalf("stored connection test = %v", stored)
	}

	readers := harness.call(http.MethodPost, "/api/v1/admin/groups", map[string]any{"name": harness.name("readers")}, admin, http.StatusCreated)
	writers := harness.call(http.MethodPost, "/api/v1/admin/groups", map[string]any{"name": harness.name("writers")}, admin, http.StatusCreated)
	readersID, _ := objectField(readers, "group")["id"].(string)
	writersID, _ := objectField(writers, "group")["id"].(string)
	harness.call(http.MethodPost, "/api/v1/admin/groups/"+readersID+"/members/"+viewerID, nil, admin, http.StatusCreated)
	harness.call(http.MethodPost, "/api/v1/admin/groups/"+writersID+"/members/"+editorID, nil, admin, http.StatusCreated)
	harness.call(http.MethodPost, "/api/v1/admin/groups/"+readersID+"/members/"+editorID, nil, admin, http.StatusCreated)
	harness.call(http.MethodPut, "/api/v1/admin/groups/"+readersID+"/databases/"+databaseID, map[string]any{"accessLevel": "view_only"}, admin, http.StatusOK)
	harness.call(http.MethodPut, "/api/v1/admin/groups/"+writersID+"/databases/"+databaseID, map[string]any{"accessLevel": "view_edit"}, admin, http.StatusOK)

	viewer := harness.login("viewer")
	editor := harness.login("editor")
	outsider := harness.login("outsider")
	base := "/api/v1/databases/" + databaseID
	schemaPath := base + "/schemas/" + url.PathEscape(schema)

	// Effective access: editor (view_only + view_edit) -> view_edit.
	editorDetail := harness.call(http.MethodGet, base, nil, editor, http.StatusOK)
	if objectField(editorDetail, "database")["accessLevel"] != "view_edit" {
		t.Fatalf("editor effective access = %v", editorDetail)
	}
	viewerDetail := harness.call(http.MethodGet, base, nil, viewer, http.StatusOK)
	if objectField(viewerDetail, "database")["accessLevel"] != "view_only" {
		t.Fatalf("viewer effective access = %v", viewerDetail)
	}

	t.Run("no grant and admin without grant are denied", func(t *testing.T) {
		for name, login := range map[string]platformLogin{"outsider": outsider, "admin": admin} {
			for _, path := range []string{base, base + "/schemas", schemaPath + "/tables", schemaPath + "/tables/customers/rows"} {
				recorder := harness.do(http.MethodGet, path, nil, &login, false)
				if recorder.Code != http.StatusNotFound || errorCode(recorder) != "DATABASE_NOT_FOUND" {
					t.Errorf("%s GET %s = %d %s, want 404 DATABASE_NOT_FOUND", name, path, recorder.Code, errorCode(recorder))
				}
			}
		}
	})

	t.Run("schemas hide system schemas", func(t *testing.T) {
		for _, login := range []platformLogin{viewer, editor} {
			body := harness.do(http.MethodGet, base+"/schemas", nil, &login, false)
			if body.Code != http.StatusOK || !strings.Contains(body.Body.String(), `"`+schema+`"`) {
				t.Fatalf("schemas = %d %s", body.Code, body.Body.String())
			}
			for _, hidden := range []string{`"pg_catalog"`, `"information_schema"`, `"pg_toast"`} {
				if strings.Contains(body.Body.String(), hidden) {
					t.Fatalf("schemas include %s", hidden)
				}
			}
		}
		if hidden := harness.do(http.MethodGet, base+"/schemas/pg_catalog/tables", nil, &viewer, false); hidden.Code != http.StatusNotFound {
			t.Fatalf("pg_catalog tables = %d, want 404", hidden.Code)
		}
	})

	t.Run("tables include views and odd identifiers", func(t *testing.T) {
		body := harness.call(http.MethodGet, schemaPath+"/tables", nil, viewer, http.StatusOK)
		tables, _ := body["tables"].([]any)
		names := map[string]string{}
		for _, item := range tables {
			table, _ := item.(map[string]any)
			names[table["name"].(string)] = table["type"].(string)
		}
		if names["customers"] != "table" || names["orders"] != "table" || names["big_orders"] != "view" || names[`we"ird table`] != "table" {
			t.Fatalf("tables = %v", names)
		}
	})

	t.Run("table detail shows columns constraints indexes", func(t *testing.T) {
		detail := harness.call(http.MethodGet, schemaPath+"/tables/orders", nil, viewer, http.StatusOK)
		columns, _ := detail["columns"].([]any)
		if len(columns) != 6 {
			t.Fatalf("columns = %v", columns)
		}
		first, _ := columns[0].(map[string]any)
		if first["name"] != "id" || first["dataType"] != "bigint" || first["nullable"] != false || first["identity"] != "always" {
			t.Fatalf("id column = %v", first)
		}
		amount, _ := columns[2].(map[string]any)
		if amount["dataType"] != "numeric(12,2)" {
			t.Fatalf("amount column = %v", amount)
		}
		note, _ := columns[3].(map[string]any)
		if note["nullable"] != true {
			t.Fatalf("note column = %v", note)
		}

		types := map[string]bool{}
		for _, item := range detail["constraints"].([]any) {
			constraint, _ := item.(map[string]any)
			types[constraint["type"].(string)] = true
			if constraint["type"] == "foreign_key" {
				references, _ := constraint["references"].(map[string]any)
				if references["table"] != "customers" || references["schema"] != schema {
					t.Fatalf("foreign key references = %v", references)
				}
			}
		}
		for _, want := range []string{"primary_key", "foreign_key", "check"} {
			if !types[want] {
				t.Fatalf("constraints missing %s: %v", want, detail["constraints"])
			}
		}

		indexes := map[string]map[string]any{}
		for _, item := range detail["indexes"].([]any) {
			index, _ := item.(map[string]any)
			indexes[index["name"].(string)] = index
		}
		if idx := indexes["orders_customer_idx"]; idx == nil || idx["method"] != "btree" || idx["unique"] != false ||
			!strings.Contains(idx["definition"].(string), "CREATE INDEX") {
			t.Fatalf("indexes = %v", indexes)
		}

		customers := harness.call(http.MethodGet, schemaPath+"/tables/customers", nil, viewer, http.StatusOK)
		uniqueFound := false
		for _, item := range customers["constraints"].([]any) {
			if item.(map[string]any)["type"] == "unique" {
				uniqueFound = true
			}
		}
		createdAt := customers["columns"].([]any)[2].(map[string]any)
		if !uniqueFound || createdAt["default"] != "now()" {
			t.Fatalf("customers detail = %v", customers)
		}
	})

	t.Run("rows pagination default boundary invalid", func(t *testing.T) {
		page := harness.call(http.MethodGet, schemaPath+"/tables/customers/rows", nil, viewer, http.StatusOK)
		if rows := page["rows"].([]any); len(rows) != 50 || page["limit"] != float64(50) || page["hasMore"] != true {
			t.Fatalf("default page: rows=%d limit=%v hasMore=%v", len(rows), page["limit"], page["hasMore"])
		}
		firstRow := page["rows"].([]any)[0].([]any)
		if firstRow[0] != float64(1) || page["orderedBy"].([]any)[0] != "id" {
			t.Fatalf("rows are not ordered by primary key: %v", firstRow)
		}
		full := harness.call(http.MethodGet, schemaPath+"/tables/customers/rows?limit=100", nil, viewer, http.StatusOK)
		if len(full["rows"].([]any)) != 100 || full["hasMore"] != true {
			t.Fatalf("limit=100 page invalid")
		}
		last := harness.call(http.MethodGet, schemaPath+"/tables/customers/rows?limit=100&offset=100", nil, editor, http.StatusOK)
		if len(last["rows"].([]any)) != 20 || last["hasMore"] != false {
			t.Fatalf("last page rows=%d hasMore=%v", len(last["rows"].([]any)), last["hasMore"])
		}
		for _, query := range []string{"limit=0", "limit=101", "limit=-1", "offset=-5", "limit=abc"} {
			recorder := harness.do(http.MethodGet, schemaPath+"/tables/customers/rows?"+query, nil, &viewer, false)
			if recorder.Code != http.StatusBadRequest || errorCode(recorder) != "INVALID_PAGINATION" {
				t.Errorf("rows?%s = %d %s", query, recorder.Code, errorCode(recorder))
			}
		}

		orders := harness.call(http.MethodGet, schemaPath+"/tables/orders/rows", nil, viewer, http.StatusOK)
		row := orders["rows"].([]any)[0].([]any)
		if row[2] != "19.99" || row[3] != nil || row[5] != "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11" {
			t.Fatalf("orders row = %v", row)
		}
		if payload, _ := row[4].(map[string]any); payload["n"] != float64(1) {
			t.Fatalf("jsonb payload = %v", row[4])
		}
	})

	t.Run("odd identifiers and injection payloads are not executable", func(t *testing.T) {
		weird := harness.call(http.MethodGet, schemaPath+"/tables/"+url.PathEscape(`we"ird table`)+"/rows", nil, viewer, http.StatusOK)
		if len(weird["rows"].([]any)) != 2 {
			t.Fatalf("weird table rows = %v", weird)
		}
		payloads := []string{
			`customers; DROP TABLE customers; --`,
			`customers" ; DROP SCHEMA "` + schema + `" CASCADE; --`,
			`customers WHERE 1=1`,
			`../customers`,
		}
		for _, payload := range payloads {
			recorder := harness.do(http.MethodGet, schemaPath+"/tables/"+url.PathEscape(payload)+"/rows", nil, &viewer, false)
			if recorder.Code != http.StatusNotFound {
				t.Errorf("payload %q = %d, want 404", payload, recorder.Code)
			}
		}
		badSchema := harness.do(http.MethodGet, base+"/schemas/"+url.PathEscape(`public'; DROP TABLE x; --`)+"/tables", nil, &viewer, false)
		if badSchema.Code != http.StatusNotFound || errorCode(badSchema) != "SCHEMA_NOT_FOUND" {
			t.Errorf("bad schema = %d %s", badSchema.Code, errorCode(badSchema))
		}
		var count int
		if err := target.QueryRow(context.Background(), "SELECT count(*) FROM "+pgx.Identifier{schema, "customers"}.Sanitize()).Scan(&count); err != nil || count != 120 {
			t.Fatalf("fixture table damaged: count=%d err=%v", count, err)
		}
	})

	t.Run("viewer is read only", func(t *testing.T) {
		// Endpoint viewer tidak menerima mutation sama sekali.
		recorder := harness.do(http.MethodPost, schemaPath+"/tables/customers/rows", map[string]any{}, &editor, true)
		if recorder.Code == http.StatusOK || recorder.Code == http.StatusCreated {
			t.Fatalf("POST rows = %d", recorder.Code)
		}
	})

	t.Run("connections are closed after success and error", func(t *testing.T) {
		_ = harness.do(http.MethodGet, schemaPath+"/tables/missing_table", nil, &viewer, false)
		_ = harness.do(http.MethodGet, schemaPath+"/tables/customers/rows", nil, &viewer, false)
		deadline := time.Now().Add(3 * time.Second)
		for {
			var open int
			if err := target.QueryRow(context.Background(),
				`SELECT count(*) FROM pg_stat_activity WHERE application_name = 'dbmp-platform' AND pid <> pg_backend_pid()`).Scan(&open); err != nil {
				t.Fatalf("count target connections: %v", err)
			}
			if open == 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("%d platform connections still open on target", open)
			}
			time.Sleep(50 * time.Millisecond)
		}
	})

	t.Run("inactive database and connection failure return safe errors", func(t *testing.T) {
		harness.call(http.MethodPatch, "/api/v1/admin/databases/"+databaseID, map[string]any{"isActive": false}, admin, http.StatusOK)
		inactive := harness.do(http.MethodGet, base+"/schemas", nil, &viewer, false)
		if inactive.Code != http.StatusConflict || errorCode(inactive) != "DATABASE_INACTIVE" {
			t.Fatalf("inactive = %d %s", inactive.Code, inactive.Body.String())
		}
		listed := harness.call(http.MethodGet, "/api/v1/databases", nil, viewer, http.StatusOK)
		if strings.Contains(fmt.Sprint(listed), databaseID) {
			t.Fatal("inactive database is still listed")
		}
		if outsiderView := harness.do(http.MethodGet, base+"/schemas", nil, &outsider, false); outsiderView.Code != http.StatusNotFound {
			t.Fatalf("outsider on inactive database = %d, want 404", outsiderView.Code)
		}

		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		deadPort := listener.Addr().(*net.TCPAddr).Port
		_ = listener.Close()
		harness.call(http.MethodPatch, "/api/v1/admin/databases/"+databaseID,
			map[string]any{"isActive": true, "host": "127.0.0.1", "port": deadPort}, admin, http.StatusOK)
		down := harness.do(http.MethodGet, base+"/schemas", nil, &viewer, false)
		if down.Code != http.StatusBadGateway || errorCode(down) != "TARGET_UNAVAILABLE" {
			t.Fatalf("target down = %d %s", down.Code, down.Body.String())
		}
		for _, secret := range []string{settings.password, settings.user, strconv.Itoa(deadPort)} {
			if strings.Contains(down.Body.String(), secret) {
				t.Fatalf("error response leaked %q", secret)
			}
		}
		if health := harness.do(http.MethodGet, "/api/v1/health", nil, nil, false); health.Code != http.StatusOK {
			t.Fatal("platform became unhealthy after target failure")
		}
	})

	if strings.Contains(harness.logs.String(), settings.password) {
		t.Fatal("logs contain target password")
	}
}
