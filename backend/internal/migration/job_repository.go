package migration

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	shareduuid "github.com/BryannPPH/Project_Web-Application-Development-K08/backend/internal/shared/uuid"
)

type PGJobRepository struct {
	pool *pgxpool.Pool
}

func NewPGJobRepository(pool *pgxpool.Pool) *PGJobRepository {
	return &PGJobRepository{pool: pool}
}

const jobColumns = `id::text,database_id::text,migration_id::text,requested_revision,action,
	execution_mode,actor_id::text,actor_username,status,retry_count,execution_id::text,
	correlation_id,created_at,started_at,finished_at,next_attempt_at,error_code,error_message,
	lease_token::text,lease_expires_at,worker_id,traceparent,tracestate`

const jobColumnsJ = `j.id::text,j.database_id::text,j.migration_id::text,j.requested_revision,j.action,
	j.execution_mode,j.actor_id::text,j.actor_username,j.status,j.retry_count,j.execution_id::text,
	j.correlation_id,j.created_at,j.started_at,j.finished_at,j.next_attempt_at,j.error_code,j.error_message,
	j.lease_token::text,j.lease_expires_at,j.worker_id,j.traceparent,j.tracestate`

func newUUID() (string, error) { return shareduuid.New() }

func (r *PGJobRepository) Enqueue(ctx context.Context, input enqueueJobInput) (Job, bool, error) {
	if input.Metadata.IdempotencyKey != "" {
		job, created, err := r.enqueueIdempotent(ctx, input)
		return job, created, err
	}
	jobID, err := newUUID()
	if err != nil {
		return Job{}, false, err
	}
	query := `INSERT INTO migration_jobs
		(id,database_id,migration_id,requested_revision,action,execution_mode,actor_id,actor_username,
		 idempotency_key,correlation_id,traceparent,tracestate,status,next_attempt_at,created_at,updated_at)
		VALUES ($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7::uuid,$8,NULLIF($9,''),NULLIF($10,''),
		 NULLIF($11,''),NULLIF($12,''),'queued',$13,$13,$13)
		RETURNING ` + jobColumns
	job, err := scanJob(r.pool.QueryRow(ctx, query,
		jobID, input.DatabaseID, input.MigrationID, input.RequestedRevision, input.Action, input.ExecutionMode,
		input.ActorID, input.ActorUsername, input.Metadata.IdempotencyKey, input.Metadata.CorrelationID,
		input.Metadata.Traceparent, input.Metadata.Tracestate, input.Now))
	if err != nil {
		return Job{}, false, mapRepositoryError(err)
	}
	return job, true, nil
}

func (r *PGJobRepository) enqueueIdempotent(ctx context.Context, input enqueueJobInput) (Job, bool, error) {
	jobID, err := newUUID()
	if err != nil {
		return Job{}, false, err
	}
	query := `INSERT INTO migration_jobs
		(id,database_id,migration_id,requested_revision,action,execution_mode,actor_id,actor_username,
		 idempotency_key,correlation_id,traceparent,tracestate,status,next_attempt_at,created_at,updated_at)
	VALUES ($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7::uuid,$8,$9,NULLIF($10,''),NULLIF($11,''),NULLIF($12,''),
		'queued',$13,$13,$13)
	ON CONFLICT (database_id,actor_id,idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING
	RETURNING ` + jobColumns
	job, err := scanJob(r.pool.QueryRow(ctx, query,
		jobID, input.DatabaseID, input.MigrationID, input.RequestedRevision, input.Action, input.ExecutionMode,
		input.ActorID, input.ActorUsername, input.Metadata.IdempotencyKey, input.Metadata.CorrelationID,
		input.Metadata.Traceparent, input.Metadata.Tracestate, input.Now))
	if err == nil {
		return job, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Job{}, false, mapRepositoryError(err)
	}
	job, err = scanJob(r.pool.QueryRow(ctx, `SELECT `+jobColumns+` FROM migration_jobs
		WHERE database_id=$1::uuid AND actor_id=$2::uuid AND idempotency_key=$3`,
		input.DatabaseID, input.ActorID, input.Metadata.IdempotencyKey))
	if err != nil {
		return Job{}, false, mapRepositoryError(err)
	}
	return job, false, nil
}

func (r *PGJobRepository) Get(ctx context.Context, databaseID, jobID string) (Job, error) {
	job, err := scanJob(r.pool.QueryRow(ctx, `SELECT `+jobColumns+` FROM migration_jobs
		WHERE database_id=$1::uuid AND id=$2::uuid`, databaseID, jobID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	return job, mapRepositoryError(err)
}

func (r *PGJobRepository) LatestJobID(ctx context.Context, databaseID, migrationID string) (*string, error) {
	var id string
	err := r.pool.QueryRow(ctx, `SELECT id::text FROM migration_jobs
		WHERE database_id=$1::uuid AND migration_id=$2::uuid ORDER BY created_at DESC, id DESC LIMIT 1`, databaseID, migrationID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	return &id, nil
}

func (r *PGJobRepository) ClaimNext(ctx context.Context, workerID, leaseToken string, lease time.Duration, now time.Time) (Job, bool, error) {
	query := `WITH candidate AS MATERIALIZED (
		SELECT j.id
		FROM migration_jobs j
		WHERE j.status IN ('queued','retrying') AND j.next_attempt_at <= $1
		  AND NOT EXISTS (
			SELECT 1 FROM migration_jobs earlier
			WHERE earlier.database_id=j.database_id
			  AND (earlier.created_at,earlier.id) < (j.created_at,j.id)
			  AND (
				(earlier.status IN ('queued','running','retrying') AND NOT (
					j.action IN ('retry','rollback','forward') AND EXISTS (
						SELECT 1 FROM migration_entries corrective
						WHERE corrective.id=j.migration_id AND corrective.database_id=j.database_id
						  AND (corrective.status='partial'
						    OR (j.action='rollback' AND corrective.status='partially_reverted'))
						  AND EXISTS (
							SELECT 1 FROM migration_entries blocked
							WHERE blocked.id=earlier.migration_id AND blocked.database_id=j.database_id
							  AND blocked.version > corrective.version
						  )
					)
				))
				OR (earlier.status IN ('partial','partially_reverted') AND EXISTS (
					SELECT 1 FROM migration_entries e WHERE e.id=earlier.migration_id
					  AND ((e.status='partial' AND NOT (earlier.migration_id=j.migration_id AND j.action IN ('retry','rollback','forward')))
					    OR (e.status='partially_reverted' AND NOT (earlier.migration_id=j.migration_id AND j.action='rollback')))
				))
				OR (earlier.status='needs_reconciliation' AND EXISTS (
					SELECT 1 FROM migration_entries e WHERE e.id=earlier.migration_id
					  AND e.status <> 'reconciled'
				))
			  )
		  )
		ORDER BY j.created_at,j.id
		LIMIT 1
		FOR UPDATE OF j SKIP LOCKED
	)
	UPDATE migration_jobs j SET status='running',worker_id=$2,lease_token=$3::uuid,
		lease_expires_at=$4,
		started_at=COALESCE(started_at,$1),updated_at=$1
	FROM candidate c WHERE j.id=c.id
		RETURNING ` + jobColumnsJ
	job, err := scanJob(r.pool.QueryRow(ctx, query, now, workerID, leaseToken, now.Add(lease)))
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, mapRepositoryError(err)
	}
	return job, true, nil
}

func (r *PGJobRepository) Heartbeat(ctx context.Context, job Job, now time.Time, lease time.Duration) error {
	command, err := r.pool.Exec(ctx, `UPDATE migration_jobs SET lease_expires_at=$4,updated_at=$5
		WHERE id=$1::uuid AND status='running' AND lease_token=$2::uuid AND worker_id=$3`,
		job.ID, valueOrEmpty(job.leaseToken), valueOrEmpty(job.workerID), now.Add(lease), now)
	if err != nil {
		return mapRepositoryError(err)
	}
	if command.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return nil
}

func (r *PGJobRepository) Finish(ctx context.Context, job Job, status JobStatus, errorCode, errorMessage *string, now time.Time) error {
	if !status.Terminal() {
		return ErrInvalidInput
	}
	command, err := r.pool.Exec(ctx, `UPDATE migration_jobs SET status=$4,finished_at=$5,updated_at=$5,
		error_code=$6,error_message=$7,worker_id=NULL,lease_token=NULL,lease_expires_at=NULL
		WHERE id=$1::uuid AND status='running' AND lease_token=$2::uuid AND worker_id=$3`,
		job.ID, valueOrEmpty(job.leaseToken), valueOrEmpty(job.workerID), status, now, errorCode, errorMessage)
	if err != nil {
		return mapRepositoryError(err)
	}
	if command.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return nil
}

func (r *PGJobRepository) ScheduleRetry(ctx context.Context, job Job, next time.Time, errorCode, errorMessage string, now time.Time) error {
	command, err := r.pool.Exec(ctx, `UPDATE migration_jobs SET status='retrying',retry_count=retry_count+1,
		next_attempt_at=$4,updated_at=$5,error_code=$6,error_message=$7,
		worker_id=NULL,lease_token=NULL,lease_expires_at=NULL
		WHERE id=$1::uuid AND status='running' AND lease_token=$2::uuid AND worker_id=$3`,
		job.ID, valueOrEmpty(job.leaseToken), valueOrEmpty(job.workerID), next, now, errorCode, errorMessage)
	if err != nil {
		return mapRepositoryError(err)
	}
	if command.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return nil
}

func (r *PGJobRepository) ListExpired(ctx context.Context, before time.Time, limit int) ([]Job, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+jobColumns+` FROM migration_jobs
		WHERE status='running' AND lease_expires_at <= $1
		ORDER BY lease_expires_at,id LIMIT $2`, before, limit)
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	defer rows.Close()
	jobs := make([]Job, 0)
	for rows.Next() {
		job, scanErr := scanJob(rows)
		if scanErr != nil {
			return nil, mapRepositoryError(scanErr)
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, mapRepositoryError(err)
	}
	return jobs, nil
}

func (r *PGJobRepository) RequeueExpiredWithoutExecution(ctx context.Context, job Job, now time.Time) (bool, error) {
	command, err := r.pool.Exec(ctx, `UPDATE migration_jobs SET status='queued',retry_count=retry_count+1,
		next_attempt_at=$4,updated_at=$4,
		error_code=NULL,error_message=NULL,worker_id=NULL,lease_token=NULL,lease_expires_at=NULL
		WHERE id=$1::uuid AND status='running' AND lease_token=$2::uuid AND execution_id IS NULL
		  AND lease_expires_at <= $3`, job.ID, valueOrEmpty(job.leaseToken), now, now)
	if err != nil {
		return false, mapRepositoryError(err)
	}
	return command.RowsAffected() == 1, nil
}

func scanJob(row rowScanner) (Job, error) {
	var job Job
	var action, mode, status string
	err := row.Scan(&job.ID, &job.DatabaseID, &job.MigrationID, &job.RequestedRevision, &action,
		&mode, &job.ActorID, &job.ActorUsername, &status, &job.RetryCount, &job.ExecutionID,
		&job.CorrelationID, &job.CreatedAt, &job.StartedAt, &job.FinishedAt, &job.NextAttemptAt,
		&job.ErrorCode, &job.ErrorMessage, &job.leaseToken, &job.leaseExpiresAt, &job.workerID,
		&job.traceparent, &job.tracestate)
	job.Action, job.ExecutionMode, job.Status = ExecutionAction(action), ExecutionMode(mode), JobStatus(status)
	return job, err
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
