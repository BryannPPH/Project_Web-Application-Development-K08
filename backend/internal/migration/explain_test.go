package migration

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb/targetdbtest"
)

func TestSplitStatementsRespectsEngineLexicalRules(t *testing.T) {
	cases := []struct {
		name   string
		engine targetdb.Engine
		input  string
		want   []string
	}{
		{"basic", targetdb.EnginePostgreSQL, "SELECT 1; SELECT 2;", []string{"SELECT 1", "SELECT 2"}},
		{"no trailing semicolon", targetdb.EnginePostgreSQL, "SELECT 1;\nSELECT 2", []string{"SELECT 1", "SELECT 2"}},
		{"semicolon in string", targetdb.EnginePostgreSQL, "SELECT ';'; SELECT 'a''b;'", []string{"SELECT ';'", "SELECT 'a''b;'"}},
		{"dollar quote", targetdb.EnginePostgreSQL,
			"CREATE FUNCTION f() RETURNS int AS $$ SELECT 1; $$ LANGUAGE sql; SELECT 2",
			[]string{"CREATE FUNCTION f() RETURNS int AS $$ SELECT 1; $$ LANGUAGE sql", "SELECT 2"}},
		{"comments only dropped", targetdb.EnginePostgreSQL, "-- note;\nSELECT 1; /* x; */", []string{"-- note;\nSELECT 1"}},
		{"postgres backslash is literal", targetdb.EnginePostgreSQL, `SELECT 'C:\'; SELECT 2`, []string{`SELECT 'C:\'`, "SELECT 2"}},
		{"mysql backslash escape", targetdb.EngineMySQL, `SELECT 'a\';b'; SELECT 2`, []string{`SELECT 'a\';b'`, "SELECT 2"}},
		{"mysql hash comment", targetdb.EngineMySQL, "SELECT 1 # ;\n; SELECT 2", []string{"SELECT 1 # ;", "SELECT 2"}},
		{"empty", targetdb.EnginePostgreSQL, " ;; ", nil},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, err := splitStatements(test.input, test.engine)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("got %#v, want %#v", got, test.want)
			}
		})
	}
	if _, err := splitStatements("SELECT 'open", targetdb.EnginePostgreSQL); err == nil {
		t.Fatal("unterminated string must fail")
	}
	if _, err := splitStatements("SELECT $tag$ open", targetdb.EnginePostgreSQL); err == nil {
		t.Fatal("unterminated dollar quote must fail")
	}
}

func TestFirstKeywordSkipsCommentsAndParentheses(t *testing.T) {
	for input, want := range map[string]string{
		"select 1":                      "SELECT",
		"  -- c\n/* b */ (SELECT 1)":    "SELECT",
		"with x as (select 1) select 1": "WITH",
		"-- only comment":               "",
		"123":                           "",
	} {
		if got := firstKeyword(input); got != want {
			t.Errorf("firstKeyword(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestParsePostgresPlanSummarizesScansIndexesJoins(t *testing.T) {
	raw := `[{"Plan":{"Node Type":"Hash Join","Join Type":"Inner","Startup Cost":1.5,"Total Cost":250.75,"Plan Rows":5000,
	  "Actual Total Time":3.2,"Actual Rows":120,"Actual Loops":1,"Hash Cond":"(o.cid = c.id)",
	  "Plans":[{"Node Type":"Seq Scan","Relation Name":"orders","Alias":"o","Total Cost":200,"Plan Rows":5000,"Actual Rows":5000,"Actual Loops":1},
	           {"Node Type":"Index Scan","Relation Name":"customers","Alias":"customers","Index Name":"customers_pkey","Plan Rows":2,"Actual Rows":400,"Actual Loops":1}]},
	  "Planning Time":0.4,"Execution Time":3.9}]`
	root, summary, err := parsePostgresPlan([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if root.NodeType != "Hash Join" || root.Condition != "(o.cid = c.id)" || len(root.Children) != 2 || root.Children[1].Alias != "" {
		t.Fatalf("root = %+v", root)
	}
	if !reflect.DeepEqual(summary.ScanTypes, []string{"Index Scan", "Seq Scan"}) ||
		!reflect.DeepEqual(summary.Indexes, []string{"customers_pkey"}) ||
		!reflect.DeepEqual(summary.JoinTypes, []string{"Hash Join (Inner)"}) {
		t.Fatalf("summary = %+v", summary)
	}
	if *summary.PlanningMs != 0.4 || *summary.ExecutionMs != 3.9 || *summary.TotalCost != 250.75 || *summary.ActualRows != 120 {
		t.Fatalf("metrics = %+v", summary)
	}
	// Seq scan besar + estimasi meleset (2 vs 400) menghasilkan dua peringatan.
	if len(summary.Warnings) != 3 {
		t.Fatalf("warnings = %v", summary.Warnings)
	}
	if _, _, err := parsePostgresPlan([]byte(`[]`)); err == nil {
		t.Fatal("empty plan must fail")
	}
	if _, _, err := parsePostgresPlan([]byte(`not json`)); err == nil {
		t.Fatal("invalid json must fail")
	}
}

func TestParsePostgresModifyTableUsesOperationName(t *testing.T) {
	raw := `[{"Plan":{"Node Type":"ModifyTable","Operation":"Update","Relation Name":"items","Plans":[{"Node Type":"Index Scan","Index Name":"items_pkey"}]}}]`
	root, _, err := parsePostgresPlan([]byte(raw))
	if err != nil || root.NodeType != "Update" {
		t.Fatalf("root = %+v, err %v", root, err)
	}
}

func TestParseMySQLTreeBuildsNestedPlan(t *testing.T) {
	text := "-> Nested loop inner join  (cost=1.15 rows=2) (actual time=0.05..0.07 rows=2 loops=1)\n" +
		"    -> Filter: (c.parent_id is not null)  (cost=0.45 rows=2) (actual time=0.02..0.03 rows=2 loops=1)\n" +
		"        -> Table scan on c  (cost=0.45 rows=2) (actual time=0.02..0.03 rows=2 loops=1)\n" +
		"    -> Single-row index lookup on p using PRIMARY (id=c.parent_id)  (cost=0.3 rows=1) (actual time=0.01..0.01 rows=1 loops=2)\n"
	root, summary, err := parseMySQLTree(text)
	if err != nil {
		t.Fatal(err)
	}
	if root.NodeType != "Nested loop inner join" || len(root.Children) != 2 || len(root.Children[0].Children) != 1 {
		t.Fatalf("tree = %+v", root)
	}
	lookup := root.Children[1]
	if lookup.Relation != "p" || lookup.Index != "PRIMARY" || lookup.Condition != "id=c.parent_id" || *lookup.ActualLoops != 2 {
		t.Fatalf("lookup = %+v", lookup)
	}
	if root.Children[0].NodeType != "Filter" || root.Children[0].Filter != "(c.parent_id is not null)" {
		t.Fatalf("filter = %+v", root.Children[0])
	}
	if !reflect.DeepEqual(summary.JoinTypes, []string{"Nested loop inner join"}) || !reflect.DeepEqual(summary.Indexes, []string{"PRIMARY"}) {
		t.Fatalf("summary = %+v", summary)
	}
	if *summary.ExecutionMs != 0.07 || *summary.TotalCost != 1.15 {
		t.Fatalf("metrics = %+v", summary)
	}
	if _, _, err := parseMySQLTree("nothing useful"); err == nil {
		t.Fatal("empty tree must fail")
	}
	if usableMySQLTree("-> <not executable by iterator executor>") {
		t.Fatal("placeholder output must be treated as unsupported")
	}
}

func TestParseMySQLTabularHandlesDMLAndJoins(t *testing.T) {
	row := func(selectType, table, access, key, rows, extra string) []any {
		return []any{int64(1), selectType, table, nil, access, nil, key, nil, nil, rows, "100.00", extra}
	}
	root, summary, err := parseMySQLTabular([][]any{row("UPDATE", "items", "range", "PRIMARY", "1", "Using where")})
	if err != nil || root.NodeType != "Update items" || len(root.Children) != 1 || root.Children[0].NodeType != "Index range scan" {
		t.Fatalf("update = %+v, %v", root, err)
	}
	if !reflect.DeepEqual(summary.Indexes, []string{"PRIMARY"}) {
		t.Fatalf("summary = %+v", summary)
	}
	root, summary, err = parseMySQLTabular([][]any{
		row("SIMPLE", "c", "ALL", "", "5000", ""),
		row("SIMPLE", "p", "ALL", "", "10", "Using where; Using join buffer (hash join)"),
	})
	if err != nil || root.NodeType != "Hash join" || len(summary.Warnings) != 1 {
		t.Fatalf("join = %+v %+v %v", root, summary, err)
	}
	if _, _, err := parseMySQLTabular([][]any{{"short"}}); err == nil {
		t.Fatal("unexpected shape must fail")
	}
}

// Fake authorizer untuk unit test service.
type fakeExplainAuthorizer struct{ editErr error }

func (fakeExplainAuthorizer) RequireDatabaseView(context.Context, auth.Principal, string) error {
	return nil
}
func (a fakeExplainAuthorizer) RequireDatabaseEdit(context.Context, auth.Principal, string) error {
	return a.editErr
}

func TestExplainServiceAuthorizesBeforeOpeningTarget(t *testing.T) {
	denied := errors.New("forbidden")
	provider := &targetdbtest.Provider{}
	service, err := NewExplainService(fakeExplainAuthorizer{editErr: denied}, provider, ServiceConfig{})
	if err != nil {
		t.Fatal(err)
	}
	principal := auth.Principal{UserID: "u"}
	if _, err := service.Explain(context.Background(), principal, "db", "SELECT 1", false); !errors.Is(err, denied) {
		t.Fatalf("error = %v", err)
	}
	if provider.OpenCount() != 0 {
		t.Fatal("target must not be opened before authorization succeeds")
	}
	allowed, _ := NewExplainService(fakeExplainAuthorizer{}, provider, ServiceConfig{MaxSQLBytes: 10})
	if _, err := allowed.Explain(context.Background(), principal, "db", "SELECT 1234567890", false); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("oversized SQL error = %v", err)
	}
	if _, err := allowed.Explain(context.Background(), principal, "db", "ROLLBACK", false); !errors.Is(err, ErrSQLPolicy) {
		t.Fatalf("transaction control error = %v", err)
	}
	if provider.OpenCount() != 0 {
		t.Fatal("invalid SQL must be rejected before opening the target")
	}
	failing := &targetdbtest.Provider{OpenErr: targetdb.ErrTargetUnavailable}
	unavailable, _ := NewExplainService(fakeExplainAuthorizer{}, failing, ServiceConfig{})
	if _, err := unavailable.Explain(context.Background(), principal, "db", "SELECT 1", true); !errors.Is(err, targetdb.ErrTargetUnavailable) {
		t.Fatalf("unavailable target error = %v", err)
	}
}
