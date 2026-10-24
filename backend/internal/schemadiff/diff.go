package schemadiff

import (
	"fmt"
	"sort"
	"strings"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb"
)

// Urutan fase SQL agar dependensi aman: FK dilepas paling awal dan ditambahkan
// paling akhir, table baru dibuat sebelum kolom/constraint yang merujuknya.
const (
	phaseDropForeignKey = iota + 1
	phaseDropConstraintOrIndex
	phaseDropTable
	phaseCreateTable
	phaseColumns
	phaseAddConstraint
	phaseCreateIndex
	phaseAddForeignKey
)

// dialect menghasilkan SQL sesuai engine target.
type dialect interface {
	table(schema, name string) string
	ident(name string) string
	resolve(definition, schema string) string
	createTable(schema string, table *Table) string
	columnDefinition(column Column) string
	alterColumn(schema, table string, before, after Column) ([]string, string)
	addConstraint(schema, table string, constraint Constraint) string
	dropConstraint(schema, table string, constraint Constraint) string
	createIndex(schema, table string, index Index) string
	dropIndex(schema, table string, index Index) string
}

func dialectFor(engine targetdb.Engine) dialect {
	if engine == targetdb.EngineMySQL {
		return mysqlDialect{}
	}
	return postgresDialect{}
}

// Diff membandingkan desired (sumber) dengan current (target) dan menghasilkan
// perubahan yang membuat current menjadi seperti desired.
func Diff(desired, current *Snapshot) Result {
	d := dialectFor(current.Engine)
	schema := current.Schema
	var changes []Change
	add := func(change Change) { changes = append(changes, change) }

	for _, name := range sortedTableNames(current.Tables) {
		if _, ok := desired.Tables[name]; !ok {
			add(Change{Kind: DropTable, Table: name, Destructive: true, phase: phaseDropTable,
				Description: fmt.Sprintf("Hapus table %s", name),
				Warning:     "Seluruh data pada table ini akan hilang.",
				SQL:         []string{fmt.Sprintf("DROP TABLE %s;", d.table(schema, name))}})
		}
	}

	for _, name := range sortedTableNames(desired.Tables) {
		want := desired.Tables[name]
		have, exists := current.Tables[name]
		if !exists {
			add(Change{Kind: CreateTable, Table: name, phase: phaseCreateTable,
				Description: fmt.Sprintf("Buat table %s (%d kolom)", name, len(want.Columns)),
				After:       columnList(want.Columns),
				SQL:         []string{d.createTable(schema, want)}})
			// FK dan index table baru dibuat setelah semua table tersedia.
			for _, constraint := range want.Constraints {
				if constraint.Type == ForeignKey {
					add(addConstraintChange(d, schema, name, constraint))
				}
			}
			for _, index := range want.Indexes {
				add(createIndexChange(d, schema, name, index))
			}
			continue
		}
		changes = append(changes, diffColumns(d, schema, name, want, have)...)
		changes = append(changes, diffConstraints(d, schema, name, want, have)...)
		changes = append(changes, diffIndexes(d, schema, name, want, have)...)
	}

	sort.SliceStable(changes, func(i, j int) bool { return changes[i].phase < changes[j].phase })
	tables := map[string]bool{}
	summary := Summary{Total: len(changes)}
	var script strings.Builder
	for index := range changes {
		changes[index].ID = fmt.Sprintf("c%d", index+1)
		// Teks before/after untuk tampilan memakai nama schema target yang nyata.
		changes[index].Before = d.resolve(changes[index].Before, schema)
		changes[index].After = d.resolve(changes[index].After, schema)
		tables[changes[index].Table] = true
		if changes[index].Destructive {
			summary.Destructive++
		}
		if index > 0 {
			script.WriteString("\n")
		}
		script.WriteString("-- " + changes[index].Description + "\n")
		script.WriteString(strings.Join(changes[index].SQL, "\n") + "\n")
	}
	summary.Tables = len(tables)
	if changes == nil {
		changes = []Change{}
	}
	return Result{Engine: current.Engine, Identical: len(changes) == 0, Changes: changes, SQL: script.String(), Summary: summary}
}

func diffColumns(d dialect, schema, table string, want, have *Table) []Change {
	var changes []Change
	existing := map[string]Column{}
	for _, column := range have.Columns {
		existing[column.Name] = column
	}
	wanted := map[string]bool{}
	for _, column := range want.Columns {
		wanted[column.Name] = true
		before, ok := existing[column.Name]
		if !ok {
			change := Change{Kind: AddColumn, Table: table, Object: column.Name, phase: phaseColumns,
				Description: fmt.Sprintf("Tambah kolom %s.%s", table, column.Name),
				After:       d.columnDefinition(column),
				SQL:         []string{fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s;", d.table(schema, table), d.resolve(d.columnDefinition(column), schema))}}
			if !column.Nullable && column.Default == "" && !column.AutoIncrement && column.Identity == "" && column.Generated == "" {
				change.Warning = "Kolom NOT NULL tanpa DEFAULT akan gagal bila table sudah berisi data."
			}
			changes = append(changes, change)
			continue
		}
		if sameColumn(before, column) {
			continue
		}
		statements, warning := d.alterColumn(schema, table, before, column)
		changes = append(changes, Change{Kind: AlterColumn, Table: table, Object: column.Name, phase: phaseColumns,
			Description: fmt.Sprintf("Ubah kolom %s.%s", table, column.Name),
			Before:      d.columnDefinition(before), After: d.columnDefinition(column),
			Warning: warning, SQL: statements})
	}
	for _, column := range have.Columns {
		if !wanted[column.Name] {
			changes = append(changes, Change{Kind: DropColumn, Table: table, Object: column.Name, Destructive: true, phase: phaseColumns,
				Description: fmt.Sprintf("Hapus kolom %s.%s", table, column.Name),
				Before:      d.columnDefinition(column),
				Warning:     "Data pada kolom ini akan hilang.",
				SQL:         []string{fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;", d.table(schema, table), d.ident(column.Name))}})
		}
	}
	return changes
}

func sameColumn(a, b Column) bool {
	return a.Type == b.Type && a.Nullable == b.Nullable && a.Default == b.Default &&
		a.AutoIncrement == b.AutoIncrement && a.Identity == b.Identity && a.Generated == b.Generated && a.Extra == b.Extra
}

// constraintKey membandingkan constraint berdasarkan isi, bukan nama, karena
// nama otomatis dapat berbeda antar schema/database.
func constraintKey(constraint Constraint) string {
	return string(constraint.Type) + "|" + constraint.Definition
}

func diffConstraints(d dialect, schema, table string, want, have *Table) []Change {
	var changes []Change
	wanted := map[string]bool{}
	for _, constraint := range want.Constraints {
		wanted[constraintKey(constraint)] = true
	}
	existing := map[string]bool{}
	for _, constraint := range have.Constraints {
		existing[constraintKey(constraint)] = true
		if wanted[constraintKey(constraint)] {
			continue
		}
		phase := phaseDropConstraintOrIndex
		if constraint.Type == ForeignKey {
			phase = phaseDropForeignKey
		}
		changes = append(changes, Change{Kind: DropConstraint, Table: table, Object: constraint.Name, phase: phase,
			Description: fmt.Sprintf("Hapus %s %s pada %s", constraintLabel(constraint.Type), constraint.Name, table),
			Before:      constraint.Definition,
			SQL:         []string{d.dropConstraint(schema, table, constraint)}})
	}
	for _, constraint := range want.Constraints {
		if !existing[constraintKey(constraint)] {
			changes = append(changes, addConstraintChange(d, schema, table, constraint))
		}
	}
	return changes
}

func addConstraintChange(d dialect, schema, table string, constraint Constraint) Change {
	phase := phaseAddConstraint
	warning := ""
	switch constraint.Type {
	case ForeignKey:
		phase = phaseAddForeignKey
		warning = "Akan gagal bila ada baris yang tidak memiliki pasangan di table rujukan."
	case Unique, PrimaryKey:
		warning = "Akan gagal bila data saat ini mengandung nilai duplikat."
	case Check:
		warning = "Akan gagal bila data saat ini melanggar kondisi CHECK."
	}
	return Change{Kind: AddConstraint, Table: table, Object: constraint.Name, phase: phase,
		Description: fmt.Sprintf("Tambah %s %s pada %s", constraintLabel(constraint.Type), constraint.Name, table),
		After:       constraint.Definition, Warning: warning,
		SQL: []string{d.addConstraint(schema, table, constraint)}}
}

func diffIndexes(d dialect, schema, table string, want, have *Table) []Change {
	var changes []Change
	wanted := map[string]bool{}
	for _, index := range want.Indexes {
		wanted[index.Definition] = true
	}
	existing := map[string]bool{}
	for _, index := range have.Indexes {
		existing[index.Definition] = true
		if !wanted[index.Definition] {
			changes = append(changes, Change{Kind: DropIndex, Table: table, Object: index.Name, phase: phaseDropConstraintOrIndex,
				Description: fmt.Sprintf("Hapus index %s pada %s", index.Name, table),
				Before:      index.Definition,
				SQL:         []string{d.dropIndex(schema, table, index)}})
		}
	}
	for _, index := range want.Indexes {
		if !existing[index.Definition] {
			changes = append(changes, createIndexChange(d, schema, table, index))
		}
	}
	return changes
}

func createIndexChange(d dialect, schema, table string, index Index) Change {
	change := Change{Kind: CreateIndex, Table: table, Object: index.Name, phase: phaseCreateIndex,
		Description: fmt.Sprintf("Buat index %s pada %s", index.Name, table),
		After:       index.Definition,
		SQL:         []string{d.createIndex(schema, table, index)}}
	if index.Unique {
		change.Warning = "Index unik akan gagal bila data saat ini mengandung nilai duplikat."
	}
	return change
}

func constraintLabel(kind ConstraintType) string {
	switch kind {
	case PrimaryKey:
		return "primary key"
	case Unique:
		return "unique constraint"
	case ForeignKey:
		return "foreign key"
	default:
		return "check constraint"
	}
}

func columnList(columns []Column) string {
	names := make([]string, len(columns))
	for index, column := range columns {
		names[index] = column.Name + " " + column.Type
	}
	return strings.Join(names, ", ")
}

func sortedTableNames(tables map[string]*Table) []string {
	names := make([]string, 0, len(tables))
	for name := range tables {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ---------- PostgreSQL ----------

type postgresDialect struct{}

// postgresReserved: kata yang wajib di-quote bila dipakai sebagai identifier.
var postgresReserved = map[string]bool{
	"all": true, "analyse": true, "analyze": true, "and": true, "any": true, "array": true, "as": true, "asc": true,
	"both": true, "case": true, "cast": true, "check": true, "collate": true, "column": true, "constraint": true,
	"create": true, "default": true, "desc": true, "distinct": true, "do": true, "else": true, "end": true,
	"except": true, "false": true, "for": true, "foreign": true, "from": true, "grant": true, "group": true,
	"having": true, "in": true, "into": true, "is": true, "join": true, "limit": true, "not": true, "null": true,
	"offset": true, "on": true, "or": true, "order": true, "primary": true, "references": true, "select": true,
	"table": true, "then": true, "to": true, "true": true, "union": true, "unique": true, "user": true,
	"using": true, "when": true, "where": true, "with": true,
}

func (postgresDialect) ident(name string) string {
	plain := name != ""
	for index, r := range name {
		if !(r == '_' || (r >= 'a' && r <= 'z') || (index > 0 && (r >= '0' && r <= '9' || r == '$'))) {
			plain = false
			break
		}
	}
	if plain && !postgresReserved[name] {
		return name
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func (d postgresDialect) table(schema, name string) string {
	return d.ident(schema) + "." + d.ident(name)
}

func (d postgresDialect) resolve(definition, schema string) string {
	return strings.ReplaceAll(definition, schemaPlaceholder, d.ident(schema)+".")
}

var postgresSerial = map[string]string{"integer": "serial", "bigint": "bigserial", "smallint": "smallserial"}

func (d postgresDialect) columnDefinition(column Column) string {
	columnType := column.Type
	if serial, ok := postgresSerial[column.Type]; ok && column.AutoIncrement {
		columnType = serial
	}
	parts := []string{d.ident(column.Name), columnType}
	switch {
	case column.Identity != "":
		parts = append(parts, "GENERATED "+column.Identity+" AS IDENTITY")
	case column.Generated != "":
		parts = append(parts, "GENERATED ALWAYS AS ("+column.Generated+") STORED")
	case column.Default != "":
		parts = append(parts, "DEFAULT "+column.Default)
	}
	if !column.Nullable {
		parts = append(parts, "NOT NULL")
	}
	return strings.Join(parts, " ")
}

func (d postgresDialect) createTable(schema string, table *Table) string {
	lines := make([]string, 0, len(table.Columns)+len(table.Constraints))
	for _, column := range table.Columns {
		lines = append(lines, "  "+d.resolve(d.columnDefinition(column), schema))
	}
	for _, constraint := range table.Constraints {
		if constraint.Type != ForeignKey {
			lines = append(lines, "  CONSTRAINT "+d.ident(constraint.Name)+" "+d.resolve(constraint.Definition, schema))
		}
	}
	return fmt.Sprintf("CREATE TABLE %s (\n%s\n);", d.table(schema, table.Name), strings.Join(lines, ",\n"))
}

func (d postgresDialect) alterColumn(schema, table string, before, after Column) ([]string, string) {
	prefix := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s", d.table(schema, table), d.ident(after.Name))
	var statements []string
	warning := ""
	if before.Type != after.Type {
		statements = append(statements, fmt.Sprintf("%s TYPE %s USING %s::%s;", prefix, after.Type, d.ident(after.Name), after.Type))
		warning = "Perubahan tipe dapat gagal atau mengubah nilai bila data tidak kompatibel."
	}
	if before.Identity != after.Identity {
		if before.Identity != "" {
			statements = append(statements, prefix+" DROP IDENTITY;")
		}
		if after.Identity != "" {
			statements = append(statements, fmt.Sprintf("%s ADD GENERATED %s AS IDENTITY;", prefix, after.Identity))
		}
	}
	if before.Default != after.Default || before.AutoIncrement != after.AutoIncrement {
		if after.Default != "" {
			statements = append(statements, fmt.Sprintf("%s SET DEFAULT %s;", prefix, d.resolve(after.Default, schema)))
		} else if before.Default != "" || before.AutoIncrement {
			statements = append(statements, prefix+" DROP DEFAULT;")
		}
		if after.AutoIncrement && !before.AutoIncrement {
			warning = "Kolom serial butuh sequence; tinjau SQL ini dan buat sequence secara manual bila perlu."
		}
	}
	if before.Nullable != after.Nullable {
		if after.Nullable {
			statements = append(statements, prefix+" DROP NOT NULL;")
		} else {
			statements = append(statements, prefix+" SET NOT NULL;")
			if warning == "" {
				warning = "SET NOT NULL akan gagal bila kolom masih berisi NULL."
			}
		}
	}
	if before.Generated != after.Generated {
		statements = append(statements, fmt.Sprintf("-- Kolom generated tidak dapat diubah langsung; hapus lalu buat ulang %s.", after.Name))
		warning = "Ekspresi kolom generated berubah; perlu ditangani manual."
	}
	return statements, warning
}

func (d postgresDialect) addConstraint(schema, table string, constraint Constraint) string {
	return fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s %s;", d.table(schema, table), d.ident(constraint.Name), d.resolve(constraint.Definition, schema))
}

func (d postgresDialect) dropConstraint(schema, table string, constraint Constraint) string {
	return fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s;", d.table(schema, table), d.ident(constraint.Name))
}

func (d postgresDialect) createIndex(schema, _ string, index Index) string {
	return d.resolve(index.Create, schema) + ";"
}

func (d postgresDialect) dropIndex(schema, _ string, index Index) string {
	return fmt.Sprintf("DROP INDEX %s;", d.table(schema, index.Name))
}

// ---------- MySQL ----------

type mysqlDialect struct{}

func (mysqlDialect) ident(name string) string { return "`" + strings.ReplaceAll(name, "`", "``") + "`" }

func (d mysqlDialect) table(schema, name string) string { return d.ident(schema) + "." + d.ident(name) }

func (d mysqlDialect) resolve(definition, schema string) string {
	return strings.ReplaceAll(definition, schemaPlaceholder, d.ident(schema)+".")
}

func (d mysqlDialect) columnDefinition(column Column) string {
	parts := []string{d.ident(column.Name), column.Type}
	if column.Generated != "" {
		parts = append(parts, "GENERATED ALWAYS AS ("+column.Generated+") STORED")
	}
	if column.Nullable {
		parts = append(parts, "NULL")
	} else {
		parts = append(parts, "NOT NULL")
	}
	if column.Default != "" && column.Generated == "" {
		parts = append(parts, "DEFAULT "+column.Default)
	}
	if column.AutoIncrement {
		parts = append(parts, "AUTO_INCREMENT")
	}
	if column.Extra != "" {
		parts = append(parts, column.Extra)
	}
	return strings.Join(parts, " ")
}

func (d mysqlDialect) createTable(schema string, table *Table) string {
	lines := make([]string, 0, len(table.Columns)+len(table.Constraints))
	for _, column := range table.Columns {
		lines = append(lines, "  "+d.columnDefinition(column))
	}
	for _, constraint := range table.Constraints {
		switch constraint.Type {
		case PrimaryKey:
			lines = append(lines, "  "+constraint.Definition)
		case Unique, Check:
			lines = append(lines, "  CONSTRAINT "+d.ident(constraint.Name)+" "+d.resolve(constraint.Definition, schema))
		}
	}
	return fmt.Sprintf("CREATE TABLE %s (\n%s\n);", d.table(schema, table.Name), strings.Join(lines, ",\n"))
}

func (d mysqlDialect) alterColumn(schema, table string, before, after Column) ([]string, string) {
	warning := ""
	if before.Type != after.Type {
		warning = "Perubahan tipe dapat gagal atau memotong data bila tidak kompatibel."
	} else if before.Nullable && !after.Nullable {
		warning = "NOT NULL akan gagal bila kolom masih berisi NULL."
	}
	// MODIFY COLUMN menulis ulang seluruh definisi kolom sekaligus.
	return []string{fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s;", d.table(schema, table), d.columnDefinition(after))}, warning
}

func (d mysqlDialect) addConstraint(schema, table string, constraint Constraint) string {
	if constraint.Type == PrimaryKey {
		return fmt.Sprintf("ALTER TABLE %s ADD %s;", d.table(schema, table), constraint.Definition)
	}
	return fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s %s;", d.table(schema, table), d.ident(constraint.Name), d.resolve(constraint.Definition, schema))
}

func (d mysqlDialect) dropConstraint(schema, table string, constraint Constraint) string {
	target := d.table(schema, table)
	switch constraint.Type {
	case PrimaryKey:
		return fmt.Sprintf("ALTER TABLE %s DROP PRIMARY KEY;", target)
	case ForeignKey:
		return fmt.Sprintf("ALTER TABLE %s DROP FOREIGN KEY %s;", target, d.ident(constraint.Name))
	case Unique:
		return fmt.Sprintf("ALTER TABLE %s DROP INDEX %s;", target, d.ident(constraint.Name))
	default:
		return fmt.Sprintf("ALTER TABLE %s DROP CHECK %s;", target, d.ident(constraint.Name))
	}
}

func (d mysqlDialect) createIndex(schema, table string, index Index) string {
	unique := ""
	if index.Unique {
		unique = "UNIQUE "
	}
	return fmt.Sprintf("CREATE %sINDEX %s ON %s %s;", unique, d.ident(index.Name), d.table(schema, table), index.Create)
}

func (d mysqlDialect) dropIndex(schema, table string, index Index) string {
	return fmt.Sprintf("DROP INDEX %s ON %s;", d.ident(index.Name), d.table(schema, table))
}
