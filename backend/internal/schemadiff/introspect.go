package schemadiff

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb"
)

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (targetdb.Rows, error)
}

// ---------- PostgreSQL ----------

// introspectPostgres membaca struktur satu schema. Pemanggil wajib sudah
// memasang search_path = pg_catalog di dalam transaksi READ ONLY sehingga
// pg_get_*def dan format_type selalu menulis nama schema secara eksplisit.
func introspectPostgres(ctx context.Context, q querier, schema string) (*Snapshot, error) {
	var exists bool
	if err := scanOne(ctx, q, "SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = $1)", []any{schema}, &exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrSchemaNotFound
	}
	snapshot := &Snapshot{Engine: targetdb.EnginePostgreSQL, Schema: schema, Tables: map[string]*Table{}}
	var prefix string
	if err := scanOne(ctx, q, "SELECT quote_ident($1) || '.'", []any{schema}, &prefix); err != nil {
		return nil, err
	}
	normalize := func(text string) string { return strings.ReplaceAll(text, prefix, schemaPlaceholder) }

	err := eachRow(ctx, q, `
		SELECT c.relname
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relkind IN ('r', 'p') AND NOT c.relispartition
		ORDER BY c.relname`, []any{schema}, func(scan func(...any) error) error {
		var name string
		if err := scan(&name); err != nil {
			return err
		}
		snapshot.Tables[name] = &Table{Name: name}
		return nil
	})
	if err != nil {
		return nil, err
	}

	err = eachRow(ctx, q, `
		SELECT c.relname, a.attname, format_type(a.atttypid, a.atttypmod), NOT a.attnotnull,
		       COALESCE(pg_get_expr(d.adbin, d.adrelid), ''), a.attidentity::text, a.attgenerated::text
		FROM pg_attribute a
		JOIN pg_class c ON c.oid = a.attrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
		WHERE n.nspname = $1 AND c.relkind IN ('r', 'p') AND NOT c.relispartition
		  AND a.attnum > 0 AND NOT a.attisdropped
		ORDER BY c.relname, a.attnum`, []any{schema}, func(scan func(...any) error) error {
		var tableName, identity, generated string
		var column Column
		if err := scan(&tableName, &column.Name, &column.Type, &column.Nullable, &column.Default, &identity, &generated); err != nil {
			return err
		}
		column.Default = normalize(column.Default)
		switch identity {
		case "a":
			column.Identity = "ALWAYS"
		case "d":
			column.Identity = "BY DEFAULT"
		}
		if generated == "s" {
			column.Generated, column.Default = column.Default, ""
		}
		// Default nextval(...) = kolom serial; nama sequence tidak dibandingkan.
		if strings.HasPrefix(column.Default, "nextval(") {
			column.AutoIncrement, column.Default = true, ""
		}
		if table := snapshot.Tables[tableName]; table != nil {
			table.Columns = append(table.Columns, column)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	err = eachRow(ctx, q, `
		SELECT c.relname, con.conname, con.contype::text, pg_get_constraintdef(con.oid)
		FROM pg_constraint con
		JOIN pg_class c ON c.oid = con.conrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND con.contype IN ('p', 'u', 'f', 'c')
		ORDER BY c.relname, con.contype, con.conname`, []any{schema}, func(scan func(...any) error) error {
		var tableName, kind string
		var constraint Constraint
		if err := scan(&tableName, &constraint.Name, &kind, &constraint.Definition); err != nil {
			return err
		}
		constraint.Type = map[string]ConstraintType{"p": PrimaryKey, "u": Unique, "f": ForeignKey, "c": Check}[kind]
		constraint.Definition = normalize(constraint.Definition)
		if table := snapshot.Tables[tableName]; table != nil {
			table.Constraints = append(table.Constraints, constraint)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Index yang menopang constraint (PK/UNIQUE/EXCLUDE) sudah diwakili constraint.
	err = eachRow(ctx, q, `
		SELECT t.relname, i.relname, x.indisunique, pg_get_indexdef(x.indexrelid)
		FROM pg_index x
		JOIN pg_class i ON i.oid = x.indexrelid
		JOIN pg_class t ON t.oid = x.indrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		WHERE n.nspname = $1 AND t.relkind IN ('r', 'p')
		  AND NOT EXISTS (SELECT 1 FROM pg_constraint con WHERE con.conindid = x.indexrelid AND con.contype IN ('p', 'u', 'x'))
		ORDER BY t.relname, i.relname`, []any{schema}, func(scan func(...any) error) error {
		var tableName, create string
		var index Index
		if err := scan(&tableName, &index.Name, &index.Unique, &create); err != nil {
			return err
		}
		index.Create = normalize(create)
		index.Definition = postgresIndexBody(index.Create)
		if table := snapshot.Tables[tableName]; table != nil {
			table.Indexes = append(table.Indexes, index)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return snapshot, nil
}

// "CREATE UNIQUE INDEX nama ON {{schema}}.t USING btree (a)" -> "UNIQUE ON {{schema}}.t USING btree (a)"
var postgresIndexName = regexp.MustCompile(`^CREATE (UNIQUE )?INDEX \S+ `)

func postgresIndexBody(create string) string {
	return postgresIndexName.ReplaceAllString(create, "$1")
}

// ---------- MySQL ----------

// introspectMySQL membaca struktur satu database MySQL (setara schema).
func introspectMySQL(ctx context.Context, q querier, schema string) (*Snapshot, error) {
	var count int
	if err := scanOne(ctx, q, "SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = ?", []any{schema}, &count); err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, ErrSchemaNotFound
	}
	snapshot := &Snapshot{Engine: targetdb.EngineMySQL, Schema: schema, Tables: map[string]*Table{}}
	ident := mysqlDialect{}.ident

	err := eachRow(ctx, q, `
		SELECT TABLE_NAME FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = ? AND TABLE_TYPE = 'BASE TABLE' ORDER BY TABLE_NAME`, []any{schema}, func(scan func(...any) error) error {
		var name string
		if err := scan(&name); err != nil {
			return err
		}
		snapshot.Tables[name] = &Table{Name: name}
		return nil
	})
	if err != nil {
		return nil, err
	}

	err = eachRow(ctx, q, `
		SELECT TABLE_NAME, COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_DEFAULT, EXTRA, DATA_TYPE,
		       COALESCE(GENERATION_EXPRESSION, '')
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = ? ORDER BY TABLE_NAME, ORDINAL_POSITION`, []any{schema}, func(scan func(...any) error) error {
		var tableName, nullable, extra, dataType, generated string
		var defaultValue *string
		var column Column
		if err := scan(&tableName, &column.Name, &column.Type, &nullable, &defaultValue, &extra, &dataType, &generated); err != nil {
			return err
		}
		column.Nullable = nullable == "YES"
		column.Generated = generated
		lowerExtra := strings.ToLower(extra)
		column.AutoIncrement = strings.Contains(lowerExtra, "auto_increment")
		if index := strings.Index(lowerExtra, "on update "); index >= 0 {
			column.Extra = strings.ToUpper(extra[index:])
		}
		if defaultValue != nil && generated == "" {
			column.Default = mysqlDefault(*defaultValue, dataType, strings.Contains(lowerExtra, "default_generated"))
		}
		if table := snapshot.Tables[tableName]; table != nil {
			table.Columns = append(table.Columns, column)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// PK/UNIQUE/FK beserta kolomnya (urutan kolom dijaga ORDINAL_POSITION).
	type keyInfo struct {
		table, kind, refTable  string
		columns, refColumns    []string
		updateRule, deleteRule string
	}
	keys := map[string]*keyInfo{}
	var keyOrder []string
	err = eachRow(ctx, q, `
		SELECT tc.TABLE_NAME, tc.CONSTRAINT_NAME, tc.CONSTRAINT_TYPE, k.COLUMN_NAME,
		       COALESCE(k.REFERENCED_TABLE_NAME, ''), COALESCE(k.REFERENCED_COLUMN_NAME, ''),
		       COALESCE(r.UPDATE_RULE, ''), COALESCE(r.DELETE_RULE, '')
		FROM information_schema.TABLE_CONSTRAINTS tc
		JOIN information_schema.KEY_COLUMN_USAGE k
		  ON k.CONSTRAINT_SCHEMA = tc.CONSTRAINT_SCHEMA AND k.CONSTRAINT_NAME = tc.CONSTRAINT_NAME
		 AND k.TABLE_NAME = tc.TABLE_NAME
		LEFT JOIN information_schema.REFERENTIAL_CONSTRAINTS r
		  ON r.CONSTRAINT_SCHEMA = tc.CONSTRAINT_SCHEMA AND r.CONSTRAINT_NAME = tc.CONSTRAINT_NAME
		 AND r.TABLE_NAME = tc.TABLE_NAME
		WHERE tc.TABLE_SCHEMA = ? AND tc.CONSTRAINT_TYPE IN ('PRIMARY KEY', 'UNIQUE', 'FOREIGN KEY')
		ORDER BY tc.TABLE_NAME, tc.CONSTRAINT_NAME, k.ORDINAL_POSITION`, []any{schema}, func(scan func(...any) error) error {
		var tableName, name, kind, column, refTable, refColumn, updateRule, deleteRule string
		if err := scan(&tableName, &name, &kind, &column, &refTable, &refColumn, &updateRule, &deleteRule); err != nil {
			return err
		}
		key := tableName + "\x00" + name
		info := keys[key]
		if info == nil {
			info = &keyInfo{table: tableName, kind: kind, refTable: refTable, updateRule: updateRule, deleteRule: deleteRule}
			keys[key] = info
			keyOrder = append(keyOrder, key)
		}
		info.columns = append(info.columns, ident(column))
		if refColumn != "" {
			info.refColumns = append(info.refColumns, ident(refColumn))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	foreignKeyNames := map[string]bool{}
	uniqueNames := map[string]bool{}
	for _, key := range keyOrder {
		info := keys[key]
		name := key[strings.IndexByte(key, 0)+1:]
		constraint := Constraint{Name: name}
		columns := "(" + strings.Join(info.columns, ",") + ")"
		switch info.kind {
		case "PRIMARY KEY":
			constraint.Type, constraint.Definition = PrimaryKey, "PRIMARY KEY "+columns
		case "UNIQUE":
			constraint.Type, constraint.Definition = Unique, "UNIQUE "+columns
			uniqueNames[info.table+"\x00"+name] = true
		case "FOREIGN KEY":
			constraint.Type = ForeignKey
			constraint.Definition = fmt.Sprintf("FOREIGN KEY %s REFERENCES %s%s (%s) ON DELETE %s ON UPDATE %s",
				columns, schemaPlaceholder, ident(info.refTable), strings.Join(info.refColumns, ","), info.deleteRule, info.updateRule)
			foreignKeyNames[info.table+"\x00"+name] = true
		}
		if table := snapshot.Tables[info.table]; table != nil {
			table.Constraints = append(table.Constraints, constraint)
		}
	}

	err = eachRow(ctx, q, `
		SELECT tc.TABLE_NAME, cc.CONSTRAINT_NAME, cc.CHECK_CLAUSE
		FROM information_schema.CHECK_CONSTRAINTS cc
		JOIN information_schema.TABLE_CONSTRAINTS tc
		  ON tc.CONSTRAINT_SCHEMA = cc.CONSTRAINT_SCHEMA AND tc.CONSTRAINT_NAME = cc.CONSTRAINT_NAME
		WHERE cc.CONSTRAINT_SCHEMA = ? AND tc.CONSTRAINT_TYPE = 'CHECK'
		ORDER BY tc.TABLE_NAME, cc.CONSTRAINT_NAME`, []any{schema}, func(scan func(...any) error) error {
		var tableName, name, clause string
		if err := scan(&tableName, &name, &clause); err != nil {
			return err
		}
		if table := snapshot.Tables[tableName]; table != nil {
			table.Constraints = append(table.Constraints, Constraint{Name: name, Type: Check, Definition: "CHECK (" + clause + ")"})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Index biasa: PRIMARY, index UNIQUE constraint, dan index otomatis FK
	// sudah diwakili oleh constraint sehingga dilewati.
	type indexInfo struct {
		table, name, method string
		unique              bool
		columns             []string
	}
	indexes := map[string]*indexInfo{}
	var indexOrder []string
	err = eachRow(ctx, q, `
		SELECT TABLE_NAME, INDEX_NAME, NON_UNIQUE, COALESCE(COLUMN_NAME, ''), COALESCE(SUB_PART, 0),
		       COALESCE(INDEX_TYPE, ''), COALESCE(EXPRESSION, '')
		FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = ? AND INDEX_NAME <> 'PRIMARY'
		ORDER BY TABLE_NAME, INDEX_NAME, SEQ_IN_INDEX`, []any{schema}, func(scan func(...any) error) error {
		var tableName, name, column, method, expression string
		var nonUnique, subPart int64
		if err := scan(&tableName, &name, &nonUnique, &column, &subPart, &method, &expression); err != nil {
			return err
		}
		key := tableName + "\x00" + name
		if uniqueNames[key] || foreignKeyNames[key] {
			return nil
		}
		info := indexes[key]
		if info == nil {
			info = &indexInfo{table: tableName, name: name, method: method, unique: nonUnique == 0}
			indexes[key] = info
			indexOrder = append(indexOrder, key)
		}
		part := ident(column)
		if column == "" {
			part = "(" + expression + ")"
		} else if subPart > 0 {
			part = fmt.Sprintf("%s(%d)", part, subPart)
		}
		info.columns = append(info.columns, part)
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, key := range indexOrder {
		info := indexes[key]
		columns := "(" + strings.Join(info.columns, ",") + ")"
		definition := columns + " USING " + info.method
		if info.unique {
			definition = "UNIQUE " + definition
		}
		if table := snapshot.Tables[info.table]; table != nil {
			table.Indexes = append(table.Indexes, Index{Name: info.name, Unique: info.unique, Definition: definition, Create: columns})
		}
	}
	return snapshot, nil
}

var mysqlNumericTypes = map[string]bool{
	"tinyint": true, "smallint": true, "mediumint": true, "int": true, "bigint": true,
	"decimal": true, "float": true, "double": true, "bit": true, "year": true,
}

// mysqlDefault mengubah COLUMN_DEFAULT menjadi ekspresi SQL siap pakai.
func mysqlDefault(value, dataType string, generated bool) string {
	upper := strings.ToUpper(value)
	switch {
	case strings.HasPrefix(upper, "CURRENT_TIMESTAMP") || upper == "NULL":
		return upper
	case generated:
		return "(" + value + ")"
	case mysqlNumericTypes[strings.ToLower(dataType)]:
		return value
	default:
		return "'" + strings.ReplaceAll(value, "'", "''") + "'"
	}
}

// ---------- helper query ----------

func scanOne(ctx context.Context, q querier, query string, args []any, dest ...any) error {
	found := false
	err := eachRow(ctx, q, query, args, func(scan func(...any) error) error {
		found = true
		return scan(dest...)
	})
	if err == nil && !found {
		return fmt.Errorf("query returned no rows")
	}
	return err
}

func eachRow(ctx context.Context, q querier, query string, args []any, handle func(scan func(...any) error) error) error {
	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := handle(rows.Scan); err != nil {
			return err
		}
	}
	return rows.Err()
}
