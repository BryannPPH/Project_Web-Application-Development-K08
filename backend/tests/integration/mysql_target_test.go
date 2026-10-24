package integration_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/authz/authztest"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/migration"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/viewer"
)

type fixedTargetResolver struct{ config targetdb.ConnectionConfig }

func (r fixedTargetResolver) ResolveConnection(context.Context, string) (targetdb.ConnectionConfig, error) {
	return r.config, nil
}

func mysqlTargetConfig(t *testing.T) targetdb.ConnectionConfig {
	t.Helper()
	port, _ := strconv.Atoi(os.Getenv("TEST_MYSQL_TARGET_DB_PORT"))
	config := targetdb.ConnectionConfig{
		DatabaseID: "11111111-1111-4111-8111-111111111111",
		Engine:     targetdb.EngineMySQL,
		Host:       os.Getenv("TEST_MYSQL_TARGET_DB_HOST"),
		Port:       port,
		Database:   os.Getenv("TEST_MYSQL_TARGET_DB_NAME"),
		Username:   os.Getenv("TEST_MYSQL_TARGET_DB_USER"),
		Password:   os.Getenv("TEST_MYSQL_TARGET_DB_PASSWORD"),
		SSLMode:    "disable",
	}
	if config.Host == "" || config.Port == 0 || config.Database == "" || config.Username == "" || config.Password == "" {
		t.Skip("TEST_MYSQL_TARGET_DB_* is not set")
	}
	return config
}

func TestMySQLViewerAndTransactionalMigration(t *testing.T) {
	config := mysqlTargetConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	provider := targetdb.NewProvider(fixedTargetResolver{config: config}, 5*time.Second, nil)

	connection, err := provider.Open(ctx, config.DatabaseID)
	if err != nil {
		t.Fatalf("open MySQL target: %v", err)
	}
	table := fmt.Sprintf("bonus_mysql_items_%d", time.Now().UnixNano())
	if _, err := connection.Exec(ctx, "CREATE TABLE "+"`"+table+"`"+" (id BIGINT PRIMARY KEY, code VARCHAR(32) NOT NULL UNIQUE, quantity INT NOT NULL, CHECK (quantity >= 0)) ENGINE=InnoDB"); err != nil {
		_ = connection.Close(context.Background())
		t.Fatalf("create MySQL fixture: %v", err)
	}
	_ = connection.Close(context.Background())
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		cleanupConnection, openErr := provider.Open(cleanupCtx, config.DatabaseID)
		if openErr == nil {
			_, _ = cleanupConnection.Exec(cleanupCtx, "DROP TABLE IF EXISTS "+"`"+table+"`")
			_ = cleanupConnection.Close(cleanupCtx)
		}
	})

	store := authztest.NewStore()
	store.AddDatabase(config.DatabaseID, true)
	principal := store.AddUser("mysql-viewer", auth.PlatformRoleUser)
	store.Grant("mysql-editors", config.DatabaseID, authz.AccessViewEdit, principal.UserID)
	viewerService, err := viewer.NewService(store.Policy(), provider, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	schemas, err := viewerService.Schemas(ctx, principal, config.DatabaseID)
	if err != nil {
		t.Fatalf("list MySQL schemas: %v", err)
	}
	foundSchema := false
	for _, schema := range schemas {
		if schema.Name == "mysql" || schema.Name == "information_schema" || schema.Name == "performance_schema" || schema.Name == "sys" {
			t.Fatalf("MySQL system schema leaked into viewer: %q", schema.Name)
		}
		foundSchema = foundSchema || schema.Name == config.Database
	}
	if !foundSchema {
		t.Fatalf("schema %q not found in %+v", config.Database, schemas)
	}
	tables, err := viewerService.Tables(ctx, principal, config.DatabaseID, config.Database)
	if err != nil {
		t.Fatalf("list MySQL tables: %v", err)
	}
	foundTable := false
	for _, candidate := range tables {
		foundTable = foundTable || candidate.Name == table
	}
	if !foundTable {
		t.Fatalf("table %q not found in %+v", table, tables)
	}
	detail, err := viewerService.TableDetail(ctx, principal, config.DatabaseID, config.Database, table)
	if err != nil || len(detail.Columns) != 3 || len(detail.Constraints) < 2 || len(detail.Indexes) < 2 {
		t.Fatalf("MySQL table detail = %+v, error %v", detail, err)
	}

	executor := migration.NewPGExecutor(provider)
	if err := executor.Validate(ctx, config.DatabaseID, "ALTER TABLE `"+table+"` ADD COLUMN unsafe INT"); !errors.Is(err, migration.ErrSQLPolicy) {
		t.Fatalf("MySQL DDL validation error = %v", err)
	}
	insert := "INSERT INTO `" + table + "` (id, code, quantity) VALUES (1, 'first', 1)"
	if result := executor.Execute(ctx, config.DatabaseID, insert); result.Status != migration.StatusSucceeded {
		t.Fatalf("MySQL successful migration = %+v", result)
	}
	failing := "INSERT INTO `" + table + "` (id, code, quantity) VALUES (2, 'second', 2); INSERT INTO `" + table + "` (id, code, quantity) VALUES (1, 'duplicate', 3)"
	if result := executor.Execute(ctx, config.DatabaseID, failing); result.Status != migration.StatusFailed {
		t.Fatalf("MySQL failing migration = %+v", result)
	}
	page, err := viewerService.Rows(ctx, principal, config.DatabaseID, config.Database, table, viewer.Page{Limit: 10})
	if err != nil || len(page.Rows) != 1 || page.Rows[0][0] != int64(1) {
		t.Fatalf("MySQL rows after rollback = %+v, error %v", page, err)
	}
}

func TestAdminCreatesAndTestsMySQLTarget(t *testing.T) {
	config := mysqlTargetConfig(t)
	harness := newPlatformHarness(t)
	harness.createUser("mysql-admin", auth.PlatformRoleAdmin)
	admin := harness.login("mysql-admin")
	draft := map[string]any{
		"engine": "mysql", "name": harness.name("mysql-target"),
		"host": config.Host, "port": config.Port, "databaseName": config.Database,
		"username": config.Username, "password": config.Password, "sslMode": "disable", "isActive": true,
	}
	if result := harness.call(http.MethodPost, "/api/v1/admin/databases/test-connection", draft, admin, http.StatusOK); result["success"] != true {
		t.Fatalf("MySQL draft connection test = %v", result)
	}
	created := objectField(harness.call(http.MethodPost, "/api/v1/admin/databases", draft, admin, http.StatusCreated), "database")
	databaseID, _ := created["id"].(string)
	if databaseID == "" || created["engine"] != "mysql" || created["hasCredential"] != true {
		t.Fatalf("created MySQL database = %v", created)
	}
	if _, exposed := created["password"]; exposed {
		t.Fatal("created MySQL response exposed password")
	}
	if result := harness.call(http.MethodPost, "/api/v1/admin/databases/"+databaseID+"/test", nil, admin, http.StatusOK); result["success"] != true {
		t.Fatalf("stored MySQL connection test = %v", result)
	}
	var engine string
	if err := harness.database.QueryRow(context.Background(), `SELECT engine FROM managed_databases WHERE id = $1`, databaseID).Scan(&engine); err != nil || engine != "mysql" {
		t.Fatalf("stored engine = %q, error %v", engine, err)
	}
}
