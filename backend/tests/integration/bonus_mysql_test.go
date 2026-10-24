package integration_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/migration"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/schemadiff"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb"
)

func TestMySQLExplainAnalyzeNeverPersists(t *testing.T) {
	config := mysqlTargetConfig(t)
	config.DatabaseID = bonusMySQLID
	provider := targetdb.NewProvider(mapResolver{config.DatabaseID: config}, 5*time.Second, nil)
	table := fmt.Sprintf("bonus_explain_%d", time.Now().UnixNano())
	child := table + "_child"
	execAll(t, provider, config.DatabaseID,
		"CREATE TABLE `"+table+"` (id BIGINT AUTO_INCREMENT PRIMARY KEY, code VARCHAR(32) NOT NULL, qty INT NOT NULL, INDEX qty_idx (qty)) ENGINE=InnoDB",
		"CREATE TABLE `"+child+"` (id BIGINT PRIMARY KEY, parent_id BIGINT NOT NULL) ENGINE=InnoDB",
		// 2000 baris dari cross join digit (CTE rekursif MySQL dibatasi 1000 iterasi).
		"INSERT INTO `"+table+"` (code, qty) SELECT CONCAT('c', n), n % 10 FROM ("+
			"SELECT a.d + b.d * 10 + c.d * 100 + e.d * 1000 + 1 AS n FROM "+
			"(SELECT 0 d UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4 UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9) a, "+
			"(SELECT 0 d UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4 UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9) b, "+
			"(SELECT 0 d UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4 UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9) c, "+
			"(SELECT 0 d UNION ALL SELECT 1) e) seq",
		"INSERT INTO `"+child+"` VALUES (1, 1), (2, 2)",
		"ANALYZE TABLE `"+table+"`",
	)
	t.Cleanup(func() { execAll(t, provider, config.DatabaseID, "DROP TABLE IF EXISTS `"+child+"`", "DROP TABLE IF EXISTS `"+table+"`") })

	users := newBonusUsers(config.DatabaseID)
	service, err := migration.NewExplainService(users.policy, provider, migration.ServiceConfig{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// MySQL target hanya menerima DML: DDL ditolak oleh kebijakan yang sama dengan migration.
	if _, err := service.Explain(ctx, users.editor, config.DatabaseID, "ALTER TABLE `"+table+"` ADD COLUMN x INT", false); !errors.Is(err, migration.ErrSQLPolicy) {
		t.Fatalf("MySQL DDL explain error = %v", err)
	}
	if _, err := service.Explain(ctx, users.editor, config.DatabaseID, "SELECT 1 /*!50000 , 2 */", false); !errors.Is(err, migration.ErrSQLPolicy) {
		t.Fatalf("MySQL executable comment error = %v", err)
	}

	script := strings.Join([]string{
		"SELECT * FROM `" + table + "` WHERE code = 'c5'",
		"SELECT * FROM `" + table + "` WHERE qty = 3",
		"UPDATE `" + table + "` SET qty = 999 WHERE id = 3",
		"SELECT p.code FROM `" + table + "` p JOIN `" + child + "` c ON c.parent_id = p.id",
		"INSERT INTO `" + child + "` VALUES (3, 3)",
		"DELETE FROM `" + table + "` WHERE id = 1",
	}, ";\n")
	for _, analyze := range []bool{false, true} {
		result, err := service.Explain(ctx, users.editor, config.DatabaseID, script, analyze)
		if err != nil {
			t.Fatalf("explain (analyze=%v): %v", analyze, err)
		}
		if result.Engine != targetdb.EngineMySQL || len(result.Statements) != 6 {
			t.Fatalf("result = %+v", result)
		}
		for _, statement := range result.Statements {
			if statement.Kind != migration.StatementPlanned || statement.Summary == nil || statement.Raw == "" {
				t.Fatalf("analyze=%v statement %d = %+v", analyze, statement.Index, statement)
			}
		}
		if scans := result.Statements[0].Summary.ScanTypes; len(scans) == 0 {
			t.Fatalf("full scan not detected: %+v", result.Statements[0].Summary)
		}
		if indexes := result.Statements[1].Summary.Indexes; !contains(indexes, "qty_idx") {
			t.Fatalf("qty_idx not used: %+v\n%s", result.Statements[1].Summary, result.Statements[1].Raw)
		}
		if joins := result.Statements[3].Summary.JoinTypes; len(joins) == 0 {
			t.Fatalf("join strategy missing: %+v\n%s", result.Statements[3].Summary, result.Statements[3].Raw)
		}
		if analyze {
			for _, index := range []int{0, 2, 4, 5} {
				summary := result.Statements[index].Summary
				if summary.ExecutionMs == nil || summary.ActualRows == nil {
					t.Fatalf("analyze statement %d missing actuals: %+v\n%s", index+1, summary, result.Statements[index].Raw)
				}
			}
		}
	}

	connection, err := provider.Open(ctx, config.DatabaseID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close(context.Background()) }()
	var qty, parents, children int
	if err := connection.QueryRow(ctx, "SELECT qty FROM `"+table+"` WHERE id = 3").Scan(&qty); err != nil || qty == 999 {
		t.Fatalf("UPDATE leaked from MySQL analysis: qty=%d err=%v", qty, err)
	}
	if err := connection.QueryRow(ctx, "SELECT COUNT(*) FROM `"+table+"`").Scan(&parents); err != nil || parents != 2000 {
		t.Fatalf("DELETE leaked from MySQL analysis: rows=%d err=%v", parents, err)
	}
	if err := connection.QueryRow(ctx, "SELECT COUNT(*) FROM `"+child+"`").Scan(&children); err != nil || children != 2 {
		t.Fatalf("INSERT leaked from MySQL analysis: rows=%d err=%v", children, err)
	}
}

func TestMySQLSchemaDiffRoundTrip(t *testing.T) {
	base := mysqlTargetConfig(t)
	rootPassword := os.Getenv("TEST_MYSQL_ROOT_PASSWORD")
	if rootPassword == "" {
		t.Skip("TEST_MYSQL_ROOT_PASSWORD is not set (dibutuhkan untuk membuat database kedua)")
	}
	suffix := time.Now().UnixNano()
	desiredDB := fmt.Sprintf("bonus_desired_%d", suffix)
	currentDB := fmt.Sprintf("bonus_current_%d", suffix)
	root := base
	root.Username, root.Password = "root", rootPassword
	desired, current := root, root
	desired.DatabaseID, desired.Database = "44444444-4444-4444-8444-444444444444", desiredDB
	current.DatabaseID, current.Database = "55555555-5555-4555-8555-555555555555", currentDB
	admin := root
	admin.DatabaseID = "66666666-6666-4666-8666-666666666666"
	provider := targetdb.NewProvider(mapResolver{desired.DatabaseID: desired, current.DatabaseID: current, admin.DatabaseID: admin}, 5*time.Second, nil)

	execAll(t, provider, admin.DatabaseID, "CREATE DATABASE `"+desiredDB+"`", "CREATE DATABASE `"+currentDB+"`")
	t.Cleanup(func() {
		execAll(t, provider, admin.DatabaseID, "DROP DATABASE IF EXISTS `"+desiredDB+"`", "DROP DATABASE IF EXISTS `"+currentDB+"`")
	})
	execAll(t, provider, current.DatabaseID,
		"CREATE TABLE customers (id BIGINT AUTO_INCREMENT PRIMARY KEY, email VARCHAR(100) NOT NULL, legacy TEXT, UNIQUE KEY customers_email (email))",
		"CREATE TABLE orders (id BIGINT PRIMARY KEY, customer_id BIGINT, amount DECIMAL(10,2), INDEX orders_amount_idx (amount))",
		"CREATE TABLE old_table (id INT)",
	)
	execAll(t, provider, desired.DatabaseID,
		"CREATE TABLE customers (id BIGINT AUTO_INCREMENT PRIMARY KEY, email VARCHAR(255) NOT NULL, name VARCHAR(80) NULL, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, status VARCHAR(10) NOT NULL DEFAULT 'active', UNIQUE KEY customers_email (email))",
		"CREATE TABLE orders (id BIGINT PRIMARY KEY, customer_id BIGINT NOT NULL, amount DECIMAL(10,2), INDEX orders_customer_idx (customer_id), CONSTRAINT orders_amount_check CHECK (amount >= 0), CONSTRAINT orders_customer_fk FOREIGN KEY (customer_id) REFERENCES customers (id) ON DELETE CASCADE)",
		"CREATE TABLE products (id INT AUTO_INCREMENT PRIMARY KEY, `order` INT NOT NULL DEFAULT 0, sku VARCHAR(40) NOT NULL)",
	)

	users := newBonusUsers(desired.DatabaseID, current.DatabaseID)
	service, err := schemadiff.NewService(users.policy, provider)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	request := schemadiff.Request{
		Source: schemadiff.Ref{DatabaseID: desired.DatabaseID, Schema: desiredDB},
		Target: schemadiff.Ref{DatabaseID: current.DatabaseID, Schema: currentDB},
	}
	result, err := service.Compare(ctx, users.viewer, request)
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if result.Identical || result.Executable || result.Engine != targetdb.EngineMySQL || result.Summary.Destructive != 2 {
		t.Fatalf("MySQL diff summary = %+v executable=%v", result.Summary, result.Executable)
	}
	t.Logf("generated MySQL SQL:\n%s", result.SQL)

	// SQL MySQL tidak bisa dijalankan sebagai migration atomik, jadi round trip
	// dijalankan langsung per statement untuk membuktikan SQL-nya valid.
	var statements []string
	for _, change := range result.Changes {
		statements = append(statements, change.SQL...)
	}
	execAll(t, provider, current.DatabaseID, statements...)
	again, err := service.Compare(ctx, users.editor, request)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Identical {
		t.Fatalf("MySQL schema still differs after applying generated SQL:\n%s", again.SQL)
	}

	// Engine berbeda tidak boleh dibandingkan.
	if pg := os.Getenv("TEST_TARGET_DB_HOST"); pg != "" {
		pgConfig := postgresTargetConfig(t)
		mixed := targetdb.NewProvider(mapResolver{pgConfig.DatabaseID: pgConfig, current.DatabaseID: current}, 5*time.Second, nil)
		mixedUsers := newBonusUsers(pgConfig.DatabaseID, current.DatabaseID)
		mixedService, _ := schemadiff.NewService(mixedUsers.policy, mixed)
		_, err := mixedService.Compare(ctx, mixedUsers.viewer, schemadiff.Request{
			Source: schemadiff.Ref{DatabaseID: pgConfig.DatabaseID, Schema: "public"},
			Target: schemadiff.Ref{DatabaseID: current.DatabaseID, Schema: currentDB},
		})
		if !errors.Is(err, schemadiff.ErrEngineMismatch) {
			t.Fatalf("engine mismatch error = %v", err)
		}
	}
}
