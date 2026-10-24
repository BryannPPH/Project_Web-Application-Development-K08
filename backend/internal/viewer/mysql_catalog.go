package viewer

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type mysqlCatalog struct{}

var mysqlSystemSchemas = map[string]struct{}{
	"information_schema": {},
	"mysql":              {},
	"performance_schema": {},
	"sys":                {},
}

func (mysqlCatalog) Schemas(ctx context.Context, q querier) ([]Schema, error) {
	rows, err := q.Query(ctx, `
		SELECT SCHEMA_NAME, ''
		FROM information_schema.SCHEMATA
		WHERE SCHEMA_NAME NOT IN ('information_schema','mysql','performance_schema','sys')
		ORDER BY SCHEMA_NAME`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Schema, 0)
	for rows.Next() {
		var schema Schema
		if err := rows.Scan(&schema.Name, &schema.Owner); err != nil {
			return nil, err
		}
		result = append(result, schema)
	}
	return result, rows.Err()
}

func mysqlSchemaExists(ctx context.Context, q querier, schema string) error {
	if _, hidden := mysqlSystemSchemas[strings.ToLower(schema)]; hidden {
		return ErrSchemaNotFound
	}
	var exists bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = ?)`, schema).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrSchemaNotFound
	}
	return nil
}

func (mysqlCatalog) Tables(ctx context.Context, q querier, schema string) ([]Table, error) {
	if err := mysqlSchemaExists(ctx, q, schema); err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `
		SELECT TABLE_NAME, TABLE_TYPE, COALESCE(TABLE_ROWS, 0)
		FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = ?
		ORDER BY TABLE_NAME`, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Table, 0)
	for rows.Next() {
		var table Table
		var tableType string
		table.Schema = schema
		if err := rows.Scan(&table.Name, &tableType, &table.EstimatedRows); err != nil {
			return nil, err
		}
		if tableType == "BASE TABLE" {
			table.Type = "table"
		} else if tableType == "VIEW" {
			table.Type = "view"
		} else {
			table.Type = "unknown"
		}
		result = append(result, table)
	}
	return result, rows.Err()
}

func mysqlResolveTable(ctx context.Context, q querier, schema, name string) (Table, error) {
	if err := mysqlSchemaExists(ctx, q, schema); err != nil {
		return Table{}, err
	}
	table := Table{Schema: schema, Name: name}
	var tableType string
	err := q.QueryRow(ctx, `SELECT TABLE_TYPE, COALESCE(TABLE_ROWS, 0)
		FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?`, schema, name).
		Scan(&tableType, &table.EstimatedRows)
	if err == sql.ErrNoRows {
		return Table{}, ErrTableNotFound
	}
	if err != nil {
		return Table{}, err
	}
	if tableType == "BASE TABLE" {
		table.Type = "table"
	} else if tableType == "VIEW" {
		table.Type = "view"
	} else {
		table.Type = "unknown"
	}
	return table, nil
}

func mysqlColumns(ctx context.Context, q querier, schema, table string) ([]Column, error) {
	rows, err := q.Query(ctx, `
		SELECT COLUMN_NAME, ORDINAL_POSITION, COLUMN_TYPE, IS_NULLABLE = 'YES',
		       COLUMN_DEFAULT, EXTRA, GENERATION_EXPRESSION
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		ORDER BY ORDINAL_POSITION`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Column, 0)
	for rows.Next() {
		var column Column
		var extra, generatedExpression string
		if err := rows.Scan(&column.Name, &column.Position, &column.DataType, &column.Nullable,
			&column.Default, &extra, &generatedExpression); err != nil {
			return nil, err
		}
		lowerExtra := strings.ToLower(extra)
		if strings.Contains(lowerExtra, "auto_increment") {
			column.Identity = "auto_increment"
		}
		if generatedExpression != "" {
			if strings.Contains(lowerExtra, "stored") {
				column.Generated = "stored"
			} else {
				column.Generated = "virtual"
			}
		}
		result = append(result, column)
	}
	return result, rows.Err()
}

func mysqlConstraints(ctx context.Context, q querier, schema, table string) ([]Constraint, error) {
	rows, err := q.Query(ctx, `
		SELECT tc.CONSTRAINT_NAME, tc.CONSTRAINT_TYPE, COALESCE(cc.CHECK_CLAUSE, '')
		FROM information_schema.TABLE_CONSTRAINTS tc
		LEFT JOIN information_schema.CHECK_CONSTRAINTS cc
		  ON cc.CONSTRAINT_SCHEMA = tc.CONSTRAINT_SCHEMA AND cc.CONSTRAINT_NAME = tc.CONSTRAINT_NAME
		WHERE tc.TABLE_SCHEMA = ? AND tc.TABLE_NAME = ?
		ORDER BY tc.CONSTRAINT_NAME`, schema, table)
	if err != nil {
		return nil, err
	}
	type constraintRow struct {
		constraint  Constraint
		checkClause string
	}
	base := make([]constraintRow, 0)
	for rows.Next() {
		var constraint Constraint
		var nativeType, checkClause string
		if err := rows.Scan(&constraint.Name, &nativeType, &checkClause); err != nil {
			rows.Close()
			return nil, err
		}
		constraint.Type = mysqlConstraintType(nativeType)
		base = append(base, constraintRow{constraint: constraint, checkClause: checkClause})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	result := make([]Constraint, 0, len(base))
	for _, item := range base {
		constraint := item.constraint
		columns, reference, err := mysqlConstraintColumns(ctx, q, schema, table, constraint.Name)
		if err != nil {
			return nil, err
		}
		constraint.Columns = columns
		constraint.References = reference
		constraint.Definition = mysqlConstraintDefinition(constraint, item.checkClause)
		result = append(result, constraint)
	}
	return result, nil
}

func mysqlConstraintColumns(ctx context.Context, q querier, schema, table, constraint string) ([]string, *ForeignKeyReference, error) {
	rows, err := q.Query(ctx, `
		SELECT COLUMN_NAME, REFERENCED_TABLE_SCHEMA, REFERENCED_TABLE_NAME, REFERENCED_COLUMN_NAME
		FROM information_schema.KEY_COLUMN_USAGE
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND CONSTRAINT_NAME = ?
		ORDER BY ORDINAL_POSITION`, schema, table, constraint)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	columns := make([]string, 0)
	var reference *ForeignKeyReference
	for rows.Next() {
		var column string
		var refSchema, refTable, refColumn sql.NullString
		if err := rows.Scan(&column, &refSchema, &refTable, &refColumn); err != nil {
			return nil, nil, err
		}
		columns = append(columns, column)
		if refSchema.Valid && refTable.Valid && refColumn.Valid {
			if reference == nil {
				reference = &ForeignKeyReference{Schema: refSchema.String, Table: refTable.String, Columns: []string{}}
			}
			reference.Columns = append(reference.Columns, refColumn.String)
		}
	}
	return columns, reference, rows.Err()
}

func mysqlConstraintType(native string) string {
	switch native {
	case "PRIMARY KEY":
		return "primary_key"
	case "FOREIGN KEY":
		return "foreign_key"
	case "UNIQUE":
		return "unique"
	case "CHECK":
		return "check"
	default:
		return "other"
	}
}

func mysqlConstraintDefinition(constraint Constraint, checkClause string) string {
	columns := quoteMySQLIdentifiers(constraint.Columns)
	switch constraint.Type {
	case "primary_key":
		return "PRIMARY KEY (" + columns + ")"
	case "unique":
		return "UNIQUE (" + columns + ")"
	case "foreign_key":
		if constraint.References != nil {
			return "FOREIGN KEY (" + columns + ") REFERENCES " + quoteMySQLIdentifier(constraint.References.Schema) + "." +
				quoteMySQLIdentifier(constraint.References.Table) + " (" + quoteMySQLIdentifiers(constraint.References.Columns) + ")"
		}
	case "check":
		return "CHECK (" + checkClause + ")"
	}
	return constraint.Type
}

func mysqlIndexes(ctx context.Context, q querier, schema, table string) ([]Index, error) {
	rows, err := q.Query(ctx, `
		SELECT INDEX_NAME, NON_UNIQUE = 0, INDEX_NAME = 'PRIMARY', INDEX_TYPE,
		       COALESCE(COLUMN_NAME, EXPRESSION, '')
		FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		ORDER BY INDEX_NAME, SEQ_IN_INDEX`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Index, 0)
	positions := make(map[string]int)
	for rows.Next() {
		var name, method, column string
		var unique, primary bool
		if err := rows.Scan(&name, &unique, &primary, &method, &column); err != nil {
			return nil, err
		}
		position, exists := positions[name]
		if !exists {
			position = len(result)
			positions[name] = position
			result = append(result, Index{Name: name, Unique: unique, Primary: primary, Method: strings.ToLower(method), Columns: []string{}})
		}
		result[position].Columns = append(result[position].Columns, column)
	}
	for index := range result {
		prefix := "INDEX"
		if result[index].Unique {
			prefix = "UNIQUE INDEX"
		}
		result[index].Definition = fmt.Sprintf("%s %s (%s)", prefix, quoteMySQLIdentifier(result[index].Name), quoteMySQLIdentifiers(result[index].Columns))
	}
	return result, rows.Err()
}

func mysqlPrimaryKeyColumns(ctx context.Context, q querier, schema, table string) ([]string, error) {
	rows, err := q.Query(ctx, `SELECT COLUMN_NAME FROM information_schema.KEY_COLUMN_USAGE
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND CONSTRAINT_NAME = 'PRIMARY'
		ORDER BY ORDINAL_POSITION`, schema, table)
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

func (mysqlCatalog) TableDetail(ctx context.Context, q querier, schema, table string) (TableDetail, error) {
	resolved, err := mysqlResolveTable(ctx, q, schema, table)
	if err != nil {
		return TableDetail{}, err
	}
	columns, err := mysqlColumns(ctx, q, schema, table)
	if err != nil {
		return TableDetail{}, err
	}
	constraints, err := mysqlConstraints(ctx, q, schema, table)
	if err != nil {
		return TableDetail{}, err
	}
	indexes, err := mysqlIndexes(ctx, q, schema, table)
	if err != nil {
		return TableDetail{}, err
	}
	return TableDetail{Table: resolved, Columns: columns, Constraints: constraints, Indexes: indexes}, nil
}

func (mysqlCatalog) Rows(ctx context.Context, q querier, schema, table string, page Page) (RowsPage, error) {
	_, err := mysqlResolveTable(ctx, q, schema, table)
	if err != nil {
		return RowsPage{}, err
	}
	columns, err := mysqlColumns(ctx, q, schema, table)
	if err != nil {
		return RowsPage{}, err
	}
	orderBy, err := mysqlPrimaryKeyColumns(ctx, q, schema, table)
	if err != nil {
		return RowsPage{}, err
	}
	query := "SELECT * FROM " + quoteMySQLIdentifier(schema) + "." + quoteMySQLIdentifier(table)
	if len(orderBy) > 0 {
		query += " ORDER BY " + quoteMySQLIdentifiers(orderBy)
	}
	query += " LIMIT ? OFFSET ?"
	rows, err := q.Query(ctx, query, page.Limit+1, page.Offset)
	if err != nil {
		return RowsPage{}, err
	}
	defer rows.Close()
	result := RowsPage{Columns: make([]RowColumn, 0, len(columns)), Rows: make([][]any, 0, page.Limit), Limit: page.Limit, Offset: page.Offset, OrderedBy: orderBy}
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
			return RowsPage{}, err
		}
		row := make([]any, len(values))
		for index, value := range values {
			normalized, truncated := normalizeValue(value)
			row[index] = normalized
			result.Truncated = result.Truncated || truncated
		}
		result.Rows = append(result.Rows, row)
	}
	return result, rows.Err()
}

func quoteMySQLIdentifier(value string) string {
	return "`" + strings.ReplaceAll(value, "`", "``") + "`"
}

func quoteMySQLIdentifiers(values []string) string {
	quoted := make([]string, len(values))
	for index, value := range values {
		quoted[index] = quoteMySQLIdentifier(value)
	}
	return strings.Join(quoted, ", ")
}
