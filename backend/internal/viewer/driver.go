package viewer

import (
	"context"
	"fmt"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb"
)

type catalogDriver interface {
	Schemas(context.Context, querier) ([]Schema, error)
	Tables(context.Context, querier, string) ([]Table, error)
	TableDetail(context.Context, querier, string, string) (TableDetail, error)
	Rows(context.Context, querier, string, string, Page) (RowsPage, error)
}

func catalogFor(engine targetdb.Engine) (catalogDriver, error) {
	switch targetdb.NormalizeEngine(engine) {
	case targetdb.EnginePostgreSQL:
		return postgresCatalog{}, nil
	case targetdb.EngineMySQL:
		return mysqlCatalog{}, nil
	default:
		return nil, fmt.Errorf("unsupported target engine %q", engine)
	}
}

type postgresCatalog struct{}

func (postgresCatalog) Schemas(ctx context.Context, q querier) ([]Schema, error) {
	return listSchemas(ctx, q)
}

func (postgresCatalog) Tables(ctx context.Context, q querier, schema string) ([]Table, error) {
	return listTables(ctx, q, schema)
}

func (postgresCatalog) TableDetail(ctx context.Context, q querier, schema, table string) (TableDetail, error) {
	oid, resolved, err := resolveTable(ctx, q, schema, table)
	if err != nil {
		return TableDetail{}, err
	}
	columns, err := listColumns(ctx, q, oid)
	if err != nil {
		return TableDetail{}, err
	}
	constraints, err := listConstraints(ctx, q, oid)
	if err != nil {
		return TableDetail{}, err
	}
	indexes, err := listIndexes(ctx, q, oid)
	if err != nil {
		return TableDetail{}, err
	}
	return TableDetail{Table: resolved, Columns: columns, Constraints: constraints, Indexes: indexes}, nil
}

func (postgresCatalog) Rows(ctx context.Context, q querier, schema, table string, page Page) (RowsPage, error) {
	oid, resolved, err := resolveTable(ctx, q, schema, table)
	if err != nil {
		return RowsPage{}, err
	}
	columns, err := listColumns(ctx, q, oid)
	if err != nil {
		return RowsPage{}, err
	}
	orderBy, err := primaryKeyColumns(ctx, q, oid)
	if err != nil {
		return RowsPage{}, err
	}
	return fetchRows(ctx, q, resolved, columns, orderBy, page)
}
