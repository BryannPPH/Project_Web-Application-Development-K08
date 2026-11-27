package targetdb

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestRedactSQLStatement(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		engine Engine
		query  string
		want   string
	}{
		{"adjacent literals", EnginePostgreSQL, "SELECT 123,456,-789,+.25e-3,3_500,0xFE,0o71,0b10",
			"SELECT [redacted],[redacted],-[redacted],+[redacted],[redacted],[redacted],[redacted],[redacted]"},
		{"identifiers and parameters", EnginePostgreSQL, `SELECT "column_42",item_17,$1,$2 FROM "schema"."table_2025"`,
			`SELECT "column_42",item_17,$1,$2 FROM "schema"."table_2025"`},
		{"tagged literal", EnginePostgreSQL, "SELECT $outer$ before $inner$ after $outer$,17",
			"SELECT '[redacted]',[redacted]"},
		{"SQL comments", EnginePostgreSQL, "SELECT 5 /* outer /* nested */ private */,15 -- private text\n",
			"SELECT [redacted] [comment],[redacted] [comment]\n"},
		{"quoted string", EnginePostgreSQL, "SELECT 'fake''private',42", "SELECT '[redacted]',[redacted]"},
		{"ambiguous escape", EnginePostgreSQL, `SELECT 'a\'sensitive'`, "[redacted]"},
		{"unterminated quote", EnginePostgreSQL, "SELECT 'private", "[redacted]"},
		{"unterminated dollar quote", EnginePostgreSQL, "SELECT $record$ private $$ trailing", "[redacted]"},
		{"MySQL quoted value and bind", EngineMySQL, "SELECT \"private\",? FROM `table_7` # private",
			"SELECT '[redacted]',? FROM `table_7` [comment]"},
		{"unclassified double quote", "", `SELECT "possibly sensitive"`, "SELECT '[redacted]'"},
		{"bare credential and placeholder", EnginePostgreSQL, "UPDATE app SET password=private WHERE token=$1",
			"UPDATE app SET password=[redacted] WHERE token=$1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := redactSQLStatement(tt.query, tt.engine); got != tt.want {
				t.Fatalf("redacted SQL = %q, want %q", got, tt.want)
			}
		})
	}
}

type redactionConnection struct {
	Connection
	executed string
}

func (*redactionConnection) Engine() Engine { return EnginePostgreSQL }

func (c *redactionConnection) Exec(_ context.Context, query string, _ ...any) (CommandResult, error) {
	c.executed = query
	return nil, &pgconn.PgError{Code: "22012", Message: "synthetic error containing private_marker"}
}

func TestQueryLogsUseRedactedSQLWithoutChangingExecution(t *testing.T) {
	t.Parallel()
	rawQuery := "SELECT 81233,96754,$1"
	var output bytes.Buffer
	target := &redactionConnection{}
	connection := loggingConnection{Connection: target, databaseID: "test-db", logger: slog.New(slog.NewJSONHandler(&output, nil))}
	ctx := WithActivity(context.Background(), Activity{JobID: "test-job", Attempt: 2, BatchNumber: 3, CorrelationID: "test-request"})
	if _, err := connection.Exec(ctx, rawQuery, "private_marker"); err == nil {
		t.Fatal("target error was lost")
	}
	if target.executed != rawQuery {
		t.Fatal("redaction changed the SQL sent to the target")
	}
	if strings.Contains(output.String(), "private_marker") || strings.Contains(output.String(), "81233") || strings.Contains(output.String(), "96754") {
		t.Fatal("log contains an unredacted fixture value")
	}
	decoder := json.NewDecoder(&output)
	for _, want := range []struct{ event, status string }{
		{"database_query_started", "started"}, {"database_query_failed", "failed"},
	} {
		var event map[string]any
		if err := decoder.Decode(&event); err != nil {
			t.Fatal(err)
		}
		if event["event"] != want.event || event["status"] != want.status ||
			event["query"] != "SELECT [redacted],[redacted],$1" || event["correlation_id"] != "test-request" ||
			event["job_id"] != "test-job" || event["attempt"] != float64(2) || event["batch_number"] != float64(3) {
			t.Fatalf("unexpected log fields: %v", event)
		}
		if want.status == "failed" && (event["sqlstate"] != "22012" || event["duration_ms"] == nil) {
			t.Fatalf("failed SQL log lacks error metadata: %v", event)
		}
	}
}
