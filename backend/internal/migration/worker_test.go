package migration

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/auth"
)

type workerAttemptRepository struct {
	Repository
	AttemptRepository
}

type workerTestAuthorizer struct{}

func (workerTestAuthorizer) RequireDatabaseView(context.Context, auth.Principal, string) error {
	return nil
}
func (workerTestAuthorizer) RequireDatabaseEdit(context.Context, auth.Principal, string) error {
	return nil
}

type workerTestLocker struct{ err error }

func (l workerTestLocker) Acquire(context.Context, string) (ExecutionLock, error) {
	if l.err != nil {
		return nil, l.err
	}
	return workerTestLock{}, nil
}

type workerTestLock struct{}

func (workerTestLock) Release(context.Context) error { return nil }

type workerTestExecutor struct{}

func (workerTestExecutor) Validate(context.Context, string, string) error { return nil }
func (workerTestExecutor) Execute(context.Context, string, string) ExecutionResult {
	return ExecutionResult{Status: StatusSucceeded}
}

type workerTestApproval struct{}

func (workerTestApproval) RequireApproved(context.Context, string, string, string, int64) error {
	return nil
}

type workerTestJobs struct {
	job           Job
	scheduledNext time.Time
	scheduled     int
	finished      int
	finishStatus  JobStatus
	finishCode    *string
}

func (j *workerTestJobs) Enqueue(context.Context, enqueueJobInput) (Job, bool, error) {
	return Job{}, false, errors.New("unexpected Enqueue")
}
func (j *workerTestJobs) Get(context.Context, string, string) (Job, error) { return j.job, nil }
func (j *workerTestJobs) ClaimNext(context.Context, string, string, time.Duration, time.Time) (Job, bool, error) {
	return Job{}, false, errors.New("unexpected ClaimNext")
}

func (j *workerTestJobs) LatestJobID(context.Context, string, string) (*string, error) {
	return nil, nil
}
func (j *workerTestJobs) Heartbeat(context.Context, Job, time.Time, time.Duration) error { return nil }
func (j *workerTestJobs) Finish(_ context.Context, _ Job, status JobStatus, code, _ *string, _ time.Time) error {
	j.finished++
	j.finishStatus, j.finishCode = status, code
	return nil
}
func (j *workerTestJobs) ScheduleRetry(_ context.Context, _ Job, next time.Time, _, _ string, _ time.Time) error {
	j.scheduled++
	j.scheduledNext = next
	return nil
}
func (j *workerTestJobs) ListExpired(context.Context, time.Time, int) ([]Job, error) {
	return nil, errors.New("unexpected ListExpired")
}
func (j *workerTestJobs) RequeueExpiredWithoutExecution(context.Context, Job, time.Time) (bool, error) {
	return false, errors.New("unexpected RequeueExpiredWithoutExecution")
}

func newWorkerForProcessTest(t *testing.T, jobs *workerTestJobs, acquireErr error, config WorkerConfig, now time.Time) *Worker {
	t.Helper()
	repository := workerAttemptRepository{}
	service, err := NewService(repository, workerTestAuthorizer{}, workerTestLocker{err: acquireErr}, workerTestExecutor{},
		workerTestApproval{}, fixedClock{current: now}, slog.New(slog.NewTextHandler(io.Discard, nil)), ServiceConfig{})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := NewWorker(jobs, service, config, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	worker.clock = fixedClock{current: now}
	return worker
}

func workerTestConfig(maxRetries int) WorkerConfig {
	return WorkerConfig{
		Concurrency: 1, PollInterval: 50 * time.Millisecond, LeaseDuration: 3 * time.Second,
		HeartbeatInterval: time.Second, RecoveryLockTimeout: 100 * time.Millisecond,
		MaxRetries: maxRetries, RetryBaseDelay: 100 * time.Millisecond, RetryMaxDelay: 400 * time.Millisecond,
	}
}

func TestRecoverBatchAttempt(t *testing.T) {
	tests := []struct {
		name        string
		action      ExecutionAction
		checkpoints BatchCheckpointState
		want        Status
	}{
		{name: "apply before first commit", action: ActionApply, want: StatusFailed},
		{name: "apply after committed batches", action: ActionApply, checkpoints: BatchCheckpointState{UpBatches: 2}, want: StatusPartial},
		{name: "rollback before first down commit", action: ActionRollback, checkpoints: BatchCheckpointState{UpBatches: 2}, want: StatusFailed},
		{name: "rollback partway through", action: ActionRollback, checkpoints: BatchCheckpointState{UpBatches: 3, DownBatches: 1}, want: StatusPartiallyReverted},
		{name: "rollback fully committed", action: ActionRollback, checkpoints: BatchCheckpointState{UpBatches: 3, DownBatches: 3}, want: StatusSucceeded},
		{name: "forward from fresh root before first commit", action: ActionForward, want: StatusFailed},
		{name: "forward after a committed batch", action: ActionForward, checkpoints: BatchCheckpointState{UpBatches: 1}, want: StatusPartial},
		{name: "unknown action remains conservative", action: ExecutionAction("unknown"), checkpoints: BatchCheckpointState{UpBatches: 1}, want: StatusNeedsReconciliation},
		{name: "missing source checkpoint remains conservative", action: ActionRollback, want: StatusNeedsReconciliation},
		{name: "inconsistent rollback remains conservative", action: ActionRollback, checkpoints: BatchCheckpointState{UpBatches: 1, DownBatches: 2}, want: StatusNeedsReconciliation},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, _ := recoverBatchAttempt(test.action, test.checkpoints)
			if got != test.want {
				t.Fatalf("recoverBatchAttempt() status = %q, want %q", got, test.want)
			}
		})
	}
}

func TestWorkerSchedulesTemporaryFailureWithinBackoffLimit(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	leaseToken, workerID := "lease-token", "worker-1"
	job := Job{ID: "job-1", DatabaseID: "database-1", MigrationID: "migration-1", ActorID: "actor-1",
		ActorUsername: "editor", Action: ActionApply, ExecutionMode: ExecutionModeSingle, Status: JobRunning,
		RetryCount: 1, leaseToken: &leaseToken, workerID: &workerID}
	jobs := &workerTestJobs{job: job}
	worker := newWorkerForProcessTest(t, jobs, context.DeadlineExceeded, workerTestConfig(2), now)

	worker.process(context.Background(), workerID, job)

	if jobs.scheduled != 1 || jobs.finished != 0 {
		t.Fatalf("temporary failure actions: scheduled=%d finished=%d", jobs.scheduled, jobs.finished)
	}
	delay := jobs.scheduledNext.Sub(now)
	if delay < 200*time.Millisecond || delay > 250*time.Millisecond || delay > worker.config.RetryMaxDelay {
		t.Fatalf("retry delay = %s, want [200ms,250ms] and no more than configured maximum", delay)
	}
}

func TestWorkerStopsRetryingAtConfiguredLimit(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	leaseToken, workerID := "lease-token", "worker-1"
	job := Job{ID: "job-1", DatabaseID: "database-1", MigrationID: "migration-1", ActorID: "actor-1",
		ActorUsername: "editor", Action: ActionApply, ExecutionMode: ExecutionModeSingle, Status: JobRunning,
		RetryCount: 2, leaseToken: &leaseToken, workerID: &workerID}
	jobs := &workerTestJobs{job: job}
	worker := newWorkerForProcessTest(t, jobs, context.DeadlineExceeded, workerTestConfig(2), now)

	worker.process(context.Background(), workerID, job)

	if jobs.scheduled != 0 || jobs.finished != 1 || jobs.finishStatus != JobFailed ||
		jobs.finishCode == nil || *jobs.finishCode != "RETRY_LIMIT_REACHED" {
		t.Fatalf("retry limit outcome: scheduled=%d finished=%d status=%s code=%v",
			jobs.scheduled, jobs.finished, jobs.finishStatus, jobs.finishCode)
	}
}

func TestWorkerNeverRetriesPermanentExecutionErrors(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	leaseToken, workerID := "lease-token", "worker-1"
	job := Job{ID: "job-1", DatabaseID: "database-1", MigrationID: "migration-1", ActorID: "actor-1",
		ActorUsername: "editor", Action: ActionApply, ExecutionMode: ExecutionModeSingle, Status: JobRunning,
		RetryCount: 0, leaseToken: &leaseToken, workerID: &workerID}
	jobs := &workerTestJobs{job: job}
	worker := newWorkerForProcessTest(t, jobs, errors.New("permanent lock failure"), workerTestConfig(2), now)

	worker.process(context.Background(), workerID, job)

	if jobs.scheduled != 0 || jobs.finished != 1 || jobs.finishStatus != JobFailed {
		t.Fatalf("permanent failure outcome: scheduled=%d finished=%d status=%s", jobs.scheduled, jobs.finished, jobs.finishStatus)
	}
}

func TestWorkerRetryDelayNeverExceedsConfiguredMaximum(t *testing.T) {
	worker := &Worker{config: workerTestConfig(5)}
	for attempt := 0; attempt < 20; attempt++ {
		delay := worker.retryDelay(attempt)
		if delay <= 0 || delay > worker.config.RetryMaxDelay {
			t.Fatalf("retryDelay(%d) = %s, outside (0,%s]", attempt, delay, worker.config.RetryMaxDelay)
		}
	}
	if delay := worker.retryDelay(20); delay != worker.config.RetryMaxDelay {
		t.Fatalf("retryDelay at cap = %s, want %s", delay, worker.config.RetryMaxDelay)
	}
}
