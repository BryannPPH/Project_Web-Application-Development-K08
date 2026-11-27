package migration

import "errors"

var (
	ErrNotFound               = errors.New("migration not found")
	ErrConflict               = errors.New("migration state conflict")
	ErrTargetOutcomeUncertain = errors.New("another connection has an uncertain migration outcome on this target")
	ErrTargetBatchBlocked     = errors.New("another connection has an unresolved batch on this target")
	ErrWrongOrder             = errors.New("an earlier migration blocks execution")
	ErrInvalidInput           = errors.New("invalid migration input")
	ErrSQLPolicy              = errors.New("sql violates migration policy")
	// ErrRevisionChanged: SQL diedit setelah gerbang approval dicek.
	ErrRevisionChanged = errors.New("migration revision changed during execution request")
	ErrNoAttempt       = errors.New("migration has no execution attempt")
	ErrIrreversible    = errors.New("migration is irreversible")
	ErrLeaseLost       = errors.New("migration job lease is no longer owned by this worker")
)
