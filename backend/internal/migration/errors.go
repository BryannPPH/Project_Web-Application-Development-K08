package migration

import "errors"

var (
	ErrNotFound     = errors.New("migration not found")
	ErrConflict     = errors.New("migration state conflict")
	ErrWrongOrder   = errors.New("an earlier migration blocks execution")
	ErrInvalidInput = errors.New("invalid migration input")
	ErrSQLPolicy    = errors.New("sql violates migration policy")
	// ErrRevisionChanged: SQL diedit setelah gerbang approval dicek.
	ErrRevisionChanged = errors.New("migration revision changed during execution request")
)
