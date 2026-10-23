package migration

import "time"

type Status string

const (
	StatusDraft       Status = "draft"
	StatusRunning     Status = "running"
	StatusSucceeded   Status = "succeeded"
	StatusFailed      Status = "failed"
	StatusInterrupted Status = "interrupted"
)

func (s Status) Terminal() bool {
	return s == StatusSucceeded || s == StatusFailed || s == StatusInterrupted
}

type Entry struct {
	ID           string     `json:"id"`
	DatabaseID   string     `json:"databaseId"`
	Version      int64      `json:"version"`
	Name         string     `json:"name"`
	SQL          string     `json:"sql"`
	Status       Status     `json:"status"`
	CreatedBy    string     `json:"createdBy"`
	ExecutedBy   *string    `json:"executedBy"`
	CreatedAt    time.Time  `json:"createdAt"`
	StartedAt    *time.Time `json:"startedAt"`
	FinishedAt   *time.Time `json:"finishedAt"`
	ErrorMessage *string    `json:"errorMessage"`
	CanEdit      bool       `json:"canEdit"`
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
