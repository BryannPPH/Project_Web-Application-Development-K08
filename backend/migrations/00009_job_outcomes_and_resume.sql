-- +goose Up

ALTER TABLE migration_execution_attempts
    ADD COLUMN previous_migration_status VARCHAR(32);

CREATE INDEX migration_jobs_migration_latest_idx
    ON migration_jobs (database_id, migration_id, created_at DESC, id DESC);

-- Earlier workers reported the migration's retained state as the result of a
-- failed rollback/forward. Repair the job projection from the immutable ledger.
UPDATE migration_jobs j
SET status = 'failed',
    error_code = 'MIGRATION_JOB_FAILED',
    error_message = latest.error_message
FROM migration_execution_attempts a
CROSS JOIN LATERAL (
    SELECT e.status, e.error_message
    FROM migration_execution_events e
    WHERE e.execution_id = a.id
    ORDER BY e.id DESC LIMIT 1
) latest
WHERE a.id = j.execution_id
  AND j.action IN ('rollback', 'forward')
  AND j.status = 'succeeded'
  AND latest.status = 'failed';

-- +goose Down

DROP INDEX migration_jobs_migration_latest_idx;
ALTER TABLE migration_execution_attempts DROP COLUMN previous_migration_status;
