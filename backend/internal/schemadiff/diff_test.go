package schemadiff

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb"
	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb/targetdbtest"
)

func snapshot(engine targetdb.Engine, schema string, tables ...*Table) *Snapshot {
	result := &Snapshot{Engine: engine, Schema: schema, Tables: map[string]*Table{}}
	for _, table := range tables {
		result.Tables[table.Name] = table
	}
	return result
}

func TestDiffIdenticalSchemasProduceNoChanges(t *testing.T) {
	table := func() *Table {
		return &Table{Name: "t", Columns: []Column{{Name: "id", Type: "integer", AutoIncrement: true}},
			Constraints: []Constraint{{Name: "t_pkey", Type: PrimaryKey, Definition: "PRIMARY KEY (id)"}}}
	}
	result := Diff(snapshot(targetdb.EnginePostgreSQL, "a", table()), snapshot(targetdb.EnginePostgreSQL, "b", table()))
	if !result.Identical || len(result.Changes) != 0 || result.SQL != "" {
		t.Fatalf("result = %+v", result)
	}
}

func TestDiffMatchesConstraintsByDefinitionNotName(t *testing.T) {
	desired := snapshot(targetdb.EnginePostgreSQL, "a", &Table{Name: "t",
		Constraints: []Constraint{{Name: "pk_one", Type: PrimaryKey, Definition: "PRIMARY KEY (id)"}}})
	current := snapshot(targetdb.EnginePostgreSQL, "b", &Table{Name: "t",
		Constraints: []Constraint{{Name: "pk_two", Type: PrimaryKey, Definition: "PRIMARY KEY (id)"}}})
	if result := Diff(desired, current); !result.Identical {
		t.Fatalf("renamed but equal constraint must not differ: %+v", result.Changes)
	}
}

func TestDiffOrdersPhasesSoDependenciesAreSafe(t *testing.T) {
	desired := snapshot(targetdb.EnginePostgreSQL, "dev",
		&Table{Name: "parent", Columns: []Column{{Name: "id", Type: "integer"}},
			Constraints: []Constraint{{Name: "parent_pkey", Type: PrimaryKey, Definition: "PRIMARY KEY (id)"}}},
		&Table{Name: "child", Columns: []Column{{Name: "id", Type: "integer"}, {Name: "parent_id", Type: "integer", Nullable: true}},
			Constraints: []Constraint{{Name: "child_fk", Type: ForeignKey, Definition: "FOREIGN KEY (parent_id) REFERENCES {{schema}}.parent(id)"}},
			Indexes:     []Index{{Name: "child_parent_idx", Definition: "ON {{schema}}.child USING btree (parent_id)", Create: "CREATE INDEX child_parent_idx ON {{schema}}.child USING btree (parent_id)"}}},
	)
	current := snapshot(targetdb.EnginePostgreSQL, "prod",
		&Table{Name: "legacy", Columns: []Column{{Name: "id", Type: "integer"}},
			Constraints: []Constraint{{Name: "legacy_fk", Type: ForeignKey, Definition: "FOREIGN KEY (id) REFERENCES {{schema}}.other(id)"}}},
	)
	result := Diff(desired, current)
	var kinds []ChangeKind
	for _, change := range result.Changes {
		kinds = append(kinds, change.Kind)
	}
	// FK milik table yang dihapus ikut hilang bersama DROP TABLE; table baru
	// dibuat sebelum index dan FK yang merujuknya.
	want := []ChangeKind{DropTable, CreateTable, CreateTable, CreateIndex, AddConstraint}
	if strings.Join(toStrings(kinds), ",") != strings.Join(toStrings(want), ",") {
		t.Fatalf("change order = %v, want %v", kinds, want)
	}
	if !strings.Contains(result.SQL, "REFERENCES prod.parent(id)") || strings.Contains(result.SQL, "{{schema}}") || strings.Contains(result.SQL, "dev.") {
		t.Fatalf("placeholder not resolved to target schema:\n%s", result.SQL)
	}
	for _, change := range result.Changes {
		if strings.Contains(change.Before+change.After, "{{schema}}") {
			t.Fatalf("placeholder leaked into display text: %+v", change)
		}
	}
	if result.Summary.Destructive != 1 || result.Summary.Total != 5 || result.Changes[0].ID != "c1" {
		t.Fatalf("summary = %+v", result.Summary)
	}
}

func TestPostgresColumnAlterations(t *testing.T) {
	d := postgresDialect{}
	before := Column{Name: "email", Type: "text", Nullable: true}
	after := Column{Name: "email", Type: "character varying(255)", Nullable: false, Default: "'x'::text"}
	statements, warning := d.alterColumn("public", "users", before, after)
	joined := strings.Join(statements, "\n")
	for _, want := range []string{"TYPE character varying(255) USING email::character varying(255)", "SET DEFAULT 'x'::text", "SET NOT NULL"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in\n%s", want, joined)
		}
	}
	if warning == "" {
		t.Fatal("type change must warn")
	}
	drop, _ := d.alterColumn("public", "users", Column{Name: "n", Type: "int", Default: "0"}, Column{Name: "n", Type: "int", Nullable: true})
	if !strings.Contains(strings.Join(drop, " "), "DROP DEFAULT") {
		t.Fatalf("drop default missing: %v", drop)
	}
}

func TestPostgresIdentifierQuoting(t *testing.T) {
	d := postgresDialect{}
	for input, want := range map[string]string{
		"orders": "orders", "Orders": `"Orders"`, "order": `"order"`, "user": `"user"`,
		"my table": `"my table"`, `a"b`: `"a""b"`, "_x1": "_x1", "1abc": `"1abc"`,
	} {
		if got := d.ident(input); got != want {
			t.Errorf("ident(%q) = %s, want %s", input, got, want)
		}
	}
	if got := d.columnDefinition(Column{Name: "id", Type: "bigint", AutoIncrement: true}); got != "id bigserial NOT NULL" {
		t.Errorf("serial column = %s", got)
	}
	if got := d.columnDefinition(Column{Name: "id", Type: "bigint", Identity: "ALWAYS"}); got != "id bigint GENERATED ALWAYS AS IDENTITY NOT NULL" {
		t.Errorf("identity column = %s", got)
	}
}

func TestAddNotNullColumnWithoutDefaultWarns(t *testing.T) {
	desired := snapshot(targetdb.EnginePostgreSQL, "a", &Table{Name: "t", Columns: []Column{{Name: "id", Type: "int"}, {Name: "code", Type: "text"}}})
	current := snapshot(targetdb.EnginePostgreSQL, "b", &Table{Name: "t", Columns: []Column{{Name: "id", Type: "int"}}})
	result := Diff(desired, current)
	if len(result.Changes) != 1 || result.Changes[0].Warning == "" {
		t.Fatalf("changes = %+v", result.Changes)
	}
}

func TestMySQLDialect(t *testing.T) {
	d := mysqlDialect{}
	if got := d.ident("we`ird"); got != "`we``ird`" {
		t.Fatalf("ident = %s", got)
	}
	column := Column{Name: "id", Type: "bigint", AutoIncrement: true}
	if got := d.columnDefinition(column); got != "`id` bigint NOT NULL AUTO_INCREMENT" {
		t.Fatalf("column = %s", got)
	}
	for kind, want := range map[ConstraintType]string{
		PrimaryKey: "DROP PRIMARY KEY", ForeignKey: "DROP FOREIGN KEY `c`", Unique: "DROP INDEX `c`", Check: "DROP CHECK `c`",
	} {
		if got := d.dropConstraint("db", "t", Constraint{Name: "c", Type: kind}); !strings.Contains(got, want) {
			t.Errorf("drop %s = %s", kind, got)
		}
	}
	statements, _ := d.alterColumn("db", "t", Column{Name: "x", Type: "int"}, Column{Name: "x", Type: "bigint", Nullable: true})
	if len(statements) != 1 || !strings.Contains(statements[0], "MODIFY COLUMN `x` bigint NULL") {
		t.Fatalf("modify = %v", statements)
	}
	for value, want := range map[[3]string]string{
		{"abc", "varchar", ""}: "'abc'", {"o'k", "text", ""}: "'o''k'", {"5", "int", ""}: "5",
		{"CURRENT_TIMESTAMP", "datetime", "1"}: "CURRENT_TIMESTAMP", {"(rand())", "double", "1"}: "((rand()))",
	} {
		if got := mysqlDefault(value[0], value[1], value[2] == "1"); got != want {
			t.Errorf("mysqlDefault(%v) = %s, want %s", value, got, want)
		}
	}
}

type fakeViewAuthorizer struct{ denied map[string]bool }

func (a fakeViewAuthorizer) RequireDatabaseView(_ context.Context, _ auth.Principal, databaseID string) error {
	if a.denied[databaseID] {
		return errors.New("forbidden")
	}
	return nil
}

func TestServiceValidatesAndAuthorizesBothSidesBeforeOpening(t *testing.T) {
	const source = "11111111-1111-4111-8111-111111111111"
	const target = "22222222-2222-4222-8222-222222222222"
	provider := &targetdbtest.Provider{}
	service, err := NewService(fakeViewAuthorizer{denied: map[string]bool{target: true}}, provider)
	if err != nil {
		t.Fatal(err)
	}
	principal := auth.Principal{UserID: "u"}
	for _, request := range []Request{
		{},
		{Source: Ref{DatabaseID: "not-a-uuid", Schema: "public"}, Target: Ref{DatabaseID: target, Schema: "public"}},
		{Source: Ref{DatabaseID: source, Schema: " "}, Target: Ref{DatabaseID: target, Schema: "public"}},
		{Source: Ref{DatabaseID: source, Schema: strings.Repeat("s", 200)}, Target: Ref{DatabaseID: target, Schema: "public"}},
	} {
		if _, err := service.Compare(context.Background(), principal, request); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("request %+v error = %v", request, err)
		}
	}
	request := Request{Source: Ref{DatabaseID: source, Schema: "public"}, Target: Ref{DatabaseID: target, Schema: "public"}}
	if _, err := service.Compare(context.Background(), principal, request); err == nil || err.Error() != "forbidden" {
		t.Fatalf("unauthorized target error = %v", err)
	}
	if provider.OpenCount() != 0 {
		t.Fatal("no connection may be opened before both sides are authorized")
	}
}

func toStrings(kinds []ChangeKind) []string {
	result := make([]string, len(kinds))
	for index, kind := range kinds {
		result[index] = string(kind)
	}
	return result
}
