package migration

import "time"

type Status string

const (
	StatusDraft               Status = "draft"
	StatusRunning             Status = "running"
	StatusSucceeded           Status = "succeeded"
	StatusFailed              Status = "failed"
	StatusInterrupted         Status = "interrupted"
	StatusNeedsReconciliation Status = "needs_reconciliation"
	StatusReconciled          Status = "reconciled"
)

func (s Status) Terminal() bool {
	return s == StatusSucceeded || s == StatusFailed || s == StatusInterrupted || s == StatusNeedsReconciliation || s == StatusReconciled
}

type Entry struct {
	ID                   string     `json:"id"`
	DatabaseID           string     `json:"databaseId"`
	Version              int64      `json:"version"`
	Name                 string     `json:"name"`
	SQL                  string     `json:"sql"`
	Status               Status     `json:"status"`
	CreatedBy            string     `json:"createdBy"`
	CreatedByUsername    string     `json:"createdByUsername"`
	ExecutedBy           *string    `json:"executedBy"`
	ExecutedByUsername   *string    `json:"executedByUsername"`
	CreatedAt            time.Time  `json:"createdAt"`
	StartedAt            *time.Time `json:"startedAt"`
	FinishedAt           *time.Time `json:"finishedAt"`
	ErrorMessage         *string    `json:"errorMessage"`
	ReconciledFrom       *Status    `json:"reconciledFrom"`
	ReconciledBy         *string    `json:"reconciledBy"`
	ReconciledByUsername *string    `json:"reconciledByUsername"`
	ReconciledAt         *time.Time `json:"reconciledAt"`
	ReconciliationNote   *string    `json:"reconciliationNote"`
	CanEdit              bool       `json:"canEdit"`
	CanReconcile         bool       `json:"canReconcile"`
}

type ListOptions struct {
	Limit  int
	Offset int
}

type Page struct {
	Items   []Entry `json:"items"`
	Limit   int     `json:"limit"`
	Offset  int     `json:"offset"`
	Total   int64   `json:"total"`
	CanEdit bool    `json:"canEdit"`
}
