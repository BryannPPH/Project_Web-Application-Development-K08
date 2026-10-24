package viewer

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb"
)

// querier adalah subset pgx.Tx yang dipakai query katalog.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (targetdb.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) targetdb.Row
}

// relkind yang dianggap "table" oleh viewer.
const tableKinds = `('r', 'p', 'v', 'm', 'f')`

// hiddenSchemaFilter menyembunyikan schema sistem: pg_catalog, information_schema,
// pg_toast, serta schema temporary/toast per-session (pg_temp_N, pg_toast_temp_N).
const hiddenSchemaFilter = `
	n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
	AND n.nspname !~ '^pg_(toast_)?temp_'`

// IsHiddenSchema menerapkan aturan yang sama di sisi Go.
func IsHiddenSchema(name string) bool {
	switch name {
	case "pg_catalog", "information_schema", "pg_toast":
		return true
	}
	return strings.HasPrefix(name, "pg_temp_") || strings.HasPrefix(name, "pg_toast_temp_")
}

func tableType(kind string) string {
	switch kind {
	case "r":
		return "table"
	case "p":
		return "partitioned_table"
	case "v":
		return "view"
	case "m":
		return "materialized_view"
	case "f":
		return "foreign_table"
	default:
		return "unknown"
	}
}

func listSchemas(ctx context.Context, q querier) ([]Schema, error) {
	rows, err := q.Query(ctx, `
		SELECT n.nspname::text, pg_get_userbyid(n.nspowner)::text
		FROM pg_namespace n
		WHERE `+hiddenSchemaFilter+`
		ORDER BY n.nspname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	schemas := make([]Schema, 0)
	for rows.Next() {
		var schema Schema
		if err := rows.Scan(&schema.Name, &schema.Owner); err != nil {
			return nil, err
		}
		schemas = append(schemas, schema)
	}
	return schemas, rows.Err()
}

func schemaExists(ctx context.Context, q querier, schema string) error {
	if IsHiddenSchema(schema) {
		return ErrSchemaNotFound
	}
	var exists bool
	if err := q.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_namespace n WHERE n.nspname = $1 AND `+hiddenSchemaFilter+`)`,
		schema).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrSchemaNotFound
	}
	return nil
}

func listTables(ctx context.Context, q querier, schema string) ([]Table, error) {
	if err := schemaExists(ctx, q, schema); err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `
		SELECT c.relname::text, c.relkind::text, GREATEST(c.reltuples, 0)::bigint
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relkind IN `+tableKinds+`
		ORDER BY c.relname`, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tables := make([]Table, 0)
	for rows.Next() {
		table := Table{Schema: schema}
		var kind string
		if err := rows.Scan(&table.Name, &kind, &table.EstimatedRows); err != nil {
			return nil, err
		}
		table.Type = tableType(kind)
		tables = append(tables, table)
	}
	return tables, rows.Err()
}

// resolveTable memverifikasi keberadaan table melalui katalog dan mengembalikan OID-nya.
// Identifier dari URL tidak pernah masuk SQL sebelum lolos langkah ini.
func resolveTable(ctx context.Context, q querier, schema, name string) (uint32, Table, error) {
	if err := schemaExists(ctx, q, schema); err != nil {
		return 0, Table{}, err
	}
	var oid uint32
	var kind string
	table := Table{Schema: schema, Name: name}
	err := q.QueryRow(ctx, `
		SELECT c.oid, c.relkind::text, GREATEST(c.reltuples, 0)::bigint
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2 AND c.relkind IN `+tableKinds,
		schema, name).Scan(&oid, &kind, &table.EstimatedRows)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, Table{}, ErrTableNotFound
	}
	if err != nil {
		return 0, Table{}, err
	}
	table.Type = tableType(kind)
	return oid, table, nil
}

func listColumns(ctx context.Context, q querier, oid uint32) ([]Column, error) {
	rows, err := q.Query(ctx, `
		SELECT
			a.attname::text,
			a.attnum::int,
			format_type(a.atttypid, a.atttypmod),
			NOT a.attnotnull,
			pg_get_expr(d.adbin, d.adrelid),
			a.attidentity::text,
			a.attgenerated::text
		FROM pg_attribute a
		LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
		WHERE a.attrelid = $1::oid AND a.attnum > 0 AND NOT a.attisdropped
		ORDER BY a.attnum`, oid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := make([]Column, 0)
	for rows.Next() {
		var column Column
		var identity, generated string
		if err := rows.Scan(&column.Name, &column.Position, &column.DataType, &column.Nullable,
			&column.Default, &identity, &generated); err != nil {
			return nil, err
		}
		switch identity {
		case "a":
			column.Identity = "always"
		case "d":
			column.Identity = "by_default"
		}
		switch generated {
		case "s":
			column.Generated = "stored"
		case "v":
			column.Generated = "virtual"
		}
		columns = append(columns, column)
	}
	return columns, rows.Err()
}

func constraintType(kind string) string {
	switch kind {
	case "p":
		return "primary_key"
	case "f":
		return "foreign_key"
	case "u":
		return "unique"
	case "c":
		return "check"
	case "x":
		return "exclusion"
	default:
		return "other"
	}
}

func listConstraints(ctx context.Context, q querier, oid uint32) ([]Constraint, error) {
	rows, err := q.Query(ctx, `
		SELECT
			con.conname::text,
			con.contype::text,
			pg_get_constraintdef(con.oid, true),
			COALESCE(ARRAY(
				SELECT a.attname::text
				FROM unnest(con.conkey) WITH ORDINALITY AS k(attnum, ord)
				JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.attnum
				ORDER BY k.ord
			), '{}'::text[]),
			fn.nspname::text,
			fc.relname::text,
			COALESCE(ARRAY(
				SELECT a.attname::text
				FROM unnest(con.confkey) WITH ORDINALITY AS k(attnum, ord)
				JOIN pg_attribute a ON a.attrelid = con.confrelid AND a.attnum = k.attnum
				ORDER BY k.ord
			), '{}'::text[])
		FROM pg_constraint con
		LEFT JOIN pg_class fc ON fc.oid = con.confrelid
		LEFT JOIN pg_namespace fn ON fn.oid = fc.relnamespace
		WHERE con.conrelid = $1::oid AND con.contype IN ('p', 'u', 'f', 'c', 'x')
		ORDER BY
			CASE con.contype WHEN 'p' THEN 0 WHEN 'u' THEN 1 WHEN 'f' THEN 2 WHEN 'c' THEN 3 ELSE 4 END,
			con.conname`, oid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	constraints := make([]Constraint, 0)
	for rows.Next() {
		var constraint Constraint
		var kind string
		var refSchema, refTable *string
		var refColumns []string
		if err := rows.Scan(&constraint.Name, &kind, &constraint.Definition, &constraint.Columns,
			&refSchema, &refTable, &refColumns); err != nil {
			return nil, err
		}
		constraint.Type = constraintType(kind)
		if constraint.Columns == nil {
			constraint.Columns = []string{}
		}
		if kind == "f" && refSchema != nil && refTable != nil {
			constraint.References = &ForeignKeyReference{Schema: *refSchema, Table: *refTable, Columns: refColumns}
		}
		constraints = append(constraints, constraint)
	}
	return constraints, rows.Err()
}

func listIndexes(ctx context.Context, q querier, oid uint32) ([]Index, error) {
	rows, err := q.Query(ctx, `
		SELECT
			ic.relname::text,
			i.indisunique,
			i.indisprimary,
			am.amname::text,
			COALESCE(ARRAY(
				SELECT a.attname::text
				FROM unnest(i.indkey::int2[]) WITH ORDINALITY AS k(attnum, ord)
				JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = k.attnum
				ORDER BY k.ord
			), '{}'::text[]),
			pg_get_indexdef(i.indexrelid)
		FROM pg_index i
		JOIN pg_class ic ON ic.oid = i.indexrelid
		JOIN pg_am am ON am.oid = ic.relam
		WHERE i.indrelid = $1::oid
		ORDER BY i.indisprimary DESC, ic.relname`, oid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	indexes := make([]Index, 0)
	for rows.Next() {
		var index Index
		if err := rows.Scan(&index.Name, &index.Unique, &index.Primary, &index.Method, &index.Columns, &index.Definition); err != nil {
			return nil, err
		}
		if index.Columns == nil {
			index.Columns = []string{}
		}
		indexes = append(indexes, index)
	}
	return indexes, rows.Err()
}

func primaryKeyColumns(ctx context.Context, q querier, oid uint32) ([]string, error) {
	rows, err := q.Query(ctx, `
		SELECT a.attname::text
		FROM pg_index i
		CROSS JOIN LATERAL unnest(i.indkey::int2[]) WITH ORDINALITY AS k(attnum, ord)
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = k.attnum
		WHERE i.indrelid = $1::oid AND i.indisprimary
		ORDER BY k.ord`, oid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := make([]string, 0)
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			return nil, err
		}
		columns = append(columns, column)
	}
	return columns, rows.Err()
}

// buildRowsQuery membentuk SELECT data. Schema/table/kolom ORDER BY berasal dari
// katalog (sudah diverifikasi) dan tetap di-quote dengan pgx.Identifier.Sanitize.
// LIMIT/OFFSET tetap bind parameter. Tidak ada filter/ORDER BY dari user.
func buildRowsQuery(schema, table string, orderBy []string) string {
	var builder strings.Builder
	builder.WriteString("SELECT * FROM ")
	builder.WriteString(pgx.Identifier{schema, table}.Sanitize())
	if len(orderBy) > 0 {
		quoted := make([]string, len(orderBy))
		for index, column := range orderBy {
			quoted[index] = pgx.Identifier{column}.Sanitize()
		}
		builder.WriteString(" ORDER BY ")
		builder.WriteString(strings.Join(quoted, ", "))
	}
	builder.WriteString(" LIMIT $1 OFFSET $2")
	return builder.String()
}

func fetchRows(ctx context.Context, q querier, table Table, columns []Column, orderBy []string, page Page) (RowsPage, error) {
	rows, err := q.Query(ctx, buildRowsQuery(table.Schema, table.Name, orderBy), page.Limit+1, page.Offset)
	if err != nil {
		return RowsPage{}, err
	}
	defer rows.Close()

	result := RowsPage{
		Columns:   make([]RowColumn, 0, len(columns)),
		Rows:      make([][]any, 0, page.Limit),
		Limit:     page.Limit,
		Offset:    page.Offset,
		OrderedBy: orderBy,
	}
	for _, column := range columns {
		result.Columns = append(result.Columns, RowColumn{Name: column.Name, DataType: column.DataType})
	}

	for rows.Next() {
		if len(result.Rows) == page.Limit {
			result.HasMore = true
			break
		}
		values, err := rows.Values()
		if err != nil {
			return RowsPage{}, fmt.Errorf("decode row: %w", err)
		}
		row := make([]any, len(values))
		for index, value := range values {
			normalized, truncated := normalizeValue(value)
			row[index] = normalized
			result.Truncated = result.Truncated || truncated
		}
		result.Rows = append(result.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return RowsPage{}, err
	}
	return result, nil
}
