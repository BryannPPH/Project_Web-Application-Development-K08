// Package schemadiff membandingkan dua kondisi schema database (bonus "Schema
// diff dan migration generation") lalu menghasilkan kandidat SQL migration.
//
// Alur: snapshot schema "desired" (sumber) dan "current" (target) dibaca dari
// katalog database dalam transaksi READ ONLY, dibandingkan, kemudian SQL
// dihasilkan untuk engine target. SQL hanya kandidat: frontend menampilkannya
// agar pengguna meninjau dan mengubahnya sebelum dijadikan draft migration.
package schemadiff

import (
	"errors"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/targetdb"
)

var (
	ErrInvalidInput   = errors.New("invalid schema diff request")
	ErrSchemaNotFound = errors.New("schema not found")
	ErrEngineMismatch = errors.New("source and target use different engines")
)

// schemaPlaceholder menggantikan nama schema di dalam definisi (FK, index,
// default) sehingga definisi dari dua schema berbeda dapat dibandingkan.
const schemaPlaceholder = "{{schema}}."

// Snapshot adalah struktur satu schema.
type Snapshot struct {
	Engine targetdb.Engine
	Schema string
	Tables map[string]*Table
}

type Table struct {
	Name        string
	Columns     []Column
	Constraints []Constraint
	Indexes     []Index
}

type Column struct {
	Name     string
	Type     string
	Nullable bool
	// Default sudah dinormalisasi (schema diganti placeholder); kosong = tanpa default.
	Default string
	// AutoIncrement: serial (PostgreSQL) atau AUTO_INCREMENT (MySQL).
	AutoIncrement bool
	// Identity: "ALWAYS" / "BY DEFAULT" untuk kolom IDENTITY PostgreSQL.
	Identity string
	// Generated: ekspresi kolom generated (STORED/VIRTUAL).
	Generated string
	// Extra menyimpan klausa tambahan MySQL (mis. ON UPDATE CURRENT_TIMESTAMP).
	Extra string
}

type ConstraintType string

const (
	PrimaryKey ConstraintType = "primary_key"
	Unique     ConstraintType = "unique"
	ForeignKey ConstraintType = "foreign_key"
	Check      ConstraintType = "check"
)

type Constraint struct {
	Name string
	Type ConstraintType
	// Definition dinormalisasi, mis. "FOREIGN KEY (a) REFERENCES {{schema}}.t(id)".
	Definition string
}

type Index struct {
	Name   string
	Unique bool
	// Definition tanpa nama index; dipakai untuk membandingkan.
	Definition string
	// Create adalah statement CREATE INDEX lengkap (dengan placeholder schema).
	Create string
}

// ---------- Request/response ----------

// Ref menunjuk satu schema pada satu managed database.
type Ref struct {
	DatabaseID string `json:"databaseId"`
	Schema     string `json:"schema"`
}

type Request struct {
	Source Ref `json:"source"`
	Target Ref `json:"target"`
}

// ChangeKind adalah jenis perubahan yang ditampilkan ke pengguna.
type ChangeKind string

const (
	CreateTable    ChangeKind = "create_table"
	DropTable      ChangeKind = "drop_table"
	AddColumn      ChangeKind = "add_column"
	DropColumn     ChangeKind = "drop_column"
	AlterColumn    ChangeKind = "alter_column"
	AddConstraint  ChangeKind = "add_constraint"
	DropConstraint ChangeKind = "drop_constraint"
	CreateIndex    ChangeKind = "create_index"
	DropIndex      ChangeKind = "drop_index"
)

// Change adalah satu perbedaan beserta SQL untuk menyelesaikannya.
type Change struct {
	ID          string     `json:"id"`
	Kind        ChangeKind `json:"kind"`
	Table       string     `json:"table"`
	Object      string     `json:"object,omitempty"`
	Description string     `json:"description"`
	Before      string     `json:"before,omitempty"`
	After       string     `json:"after,omitempty"`
	Destructive bool       `json:"destructive"`
	Warning     string     `json:"warning,omitempty"`
	SQL         []string   `json:"sql"`
	phase       int
}

type Summary struct {
	Total       int `json:"total"`
	Destructive int `json:"destructive"`
	Tables      int `json:"tables"`
}

type Result struct {
	Engine    targetdb.Engine `json:"engine"`
	Source    Ref             `json:"source"`
	Target    Ref             `json:"target"`
	Identical bool            `json:"identical"`
	Changes   []Change        `json:"changes"`
	SQL       string          `json:"sql"`
	Summary   Summary         `json:"summary"`
	// Executable menandakan SQL diff dapat diteruskan sebagai migration.
	// Engine MySQL mengeksekusi DDL tanpa jaminan rollback lintas statement.
	Executable bool     `json:"executable"`
	Notes      []string `json:"notes"`
}
