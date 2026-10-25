package migration

import (
	"context"
	"time"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
)

type Authorizer interface {
	RequireDatabaseView(context.Context, auth.Principal, string) error
	RequireDatabaseEdit(context.Context, auth.Principal, string) error
}

type Repository interface {
	CreateDraft(context.Context, string, string, string, string, time.Time) (Entry, error)
	UpdateDraft(context.Context, string, string, string, string, time.Time) (Entry, error)
	List(context.Context, string, ListOptions) (Page, error)
	Get(context.Context, string, string) (Entry, error)
	MarkRunning(context.Context, string, string, string, time.Time) (Entry, error)
	MarkTerminal(context.Context, string, string, Status, *string, time.Time) (Entry, error)
	Reconcile(context.Context, string, string, string, string, time.Time) (Entry, error)
	RecoverRunning(context.Context, time.Time, string) (int64, error)
}

type ExecutionLock interface {
	Release(context.Context) error
}

type Locker interface {
	Acquire(context.Context, string) (ExecutionLock, error)
}

type ExecutionResult struct {
	Status        Status
	PublicError   *string
	InternalError error
}

type Executor interface {
	Validate(context.Context, string, string) error
	Execute(context.Context, string, string) ExecutionResult
}

type Clock interface {
	Now() time.Time
}

type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now().UTC() }
