// Package viewer menampilkan schema, table, column, constraint, index, dan data
// dari managed database. Semua query berjalan di transaksi READ ONLY dengan
// statement_timeout, setelah authz memastikan user minimal view_only.
package viewer

import "errors"

var (
	ErrSchemaNotFound = errors.New("schema not found")
	ErrTableNotFound  = errors.New("table not found")
	// ErrInvalidPagination: limit/offset di luar batas.
	ErrInvalidPagination = errors.New("invalid pagination")
	// ErrTargetPermissionDenied: role target tidak punya privilege SELECT.
	ErrTargetPermissionDenied = errors.New("target role lacks privilege")
	// ErrTargetTimeout: query melewati statement_timeout/context deadline.
	ErrTargetTimeout = errors.New("target query timed out")
	// ErrTargetQueryFailed: kegagalan query target lain (detail tidak diekspos).
	ErrTargetQueryFailed = errors.New("target query failed")
)

type Schema struct {
	Name  string `json:"name"`
	Owner string `json:"owner"`
}

type Table struct {
	Schema        string `json:"schema"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	EstimatedRows int64  `json:"estimatedRows"`
}

type Column struct {
	Name     string  `json:"name"`
	Position int     `json:"position"`
	DataType string  `json:"dataType"`
	Nullable bool    `json:"nullable"`
	Default  *string `json:"default"`
	Identity string  `json:"identity,omitempty"`
	// Generated berisi "stored"/"virtual" untuk generated column.
	Generated string `json:"generated,omitempty"`
}

type ForeignKeyReference struct {
	Schema  string   `json:"schema"`
	Table   string   `json:"table"`
	Columns []string `json:"columns"`
}

type Constraint struct {
	Name       string               `json:"name"`
	Type       string               `json:"type"`
	Columns    []string             `json:"columns"`
	Definition string               `json:"definition"`
	References *ForeignKeyReference `json:"references,omitempty"`
}

type Index struct {
	Name       string   `json:"name"`
	Unique     bool     `json:"unique"`
	Primary    bool     `json:"primary"`
	Method     string   `json:"method"`
	Columns    []string `json:"columns"`
	Definition string   `json:"definition"`
}

type TableDetail struct {
	Table       Table        `json:"table"`
	Columns     []Column     `json:"columns"`
	Constraints []Constraint `json:"constraints"`
	Indexes     []Index      `json:"indexes"`
}

type RowColumn struct {
	Name     string `json:"name"`
	DataType string `json:"dataType"`
}

type RowsPage struct {
	Columns []RowColumn `json:"columns"`
	Rows    [][]any     `json:"rows"`
	Limit   int         `json:"limit"`
	Offset  int64       `json:"offset"`
	HasMore bool        `json:"hasMore"`
	// OrderedBy berisi kolom primary key yang dipakai untuk urutan stabil.
	OrderedBy []string `json:"orderedBy"`
	// Truncated true bila ada nilai sel yang dipotong karena terlalu panjang.
	Truncated bool `json:"truncated"`
}
