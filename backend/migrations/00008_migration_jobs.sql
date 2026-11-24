-- +goose Up

CREATE TABLE migration_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    database_id UUID NOT NULL REFERENCES managed_databases(id) ON DELETE RESTRICT,
    migration_id UUID NOT NULL REFERENCES migration_entries(id) ON DELETE RESTRICT,
    requested_revision BIGINT NOT NULL CHECK (requested_revision > 0),
    action VARCHAR(16) NOT NULL CHECK (action IN ('apply', 'retry', 'rollback', 'forward')),
    execution_mode VARCHAR(16) NOT NULL CHECK (execution_mode IN ('single', 'batch')),
    actor_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    actor_username VARCHAR(255) NOT NULL CHECK (length(btrim(actor_username)) > 0),
    status VARCHAR(32) NOT NULL DEFAULT 'queued' CHECK (
        status IN ('queued', 'running', 'retrying', 'succeeded', 'failed',
                   'partial', 'partially_reverted', 'needs_reconciliation')
    ),
    retry_count INTEGER NOT NULL DEFAULT 0 CHECK (retry_count >= 0),
    execution_id UUID REFERENCES migration_execution_attempts(id) ON DELETE RESTRICT,
    idempotency_key VARCHAR(128),
    correlation_id VARCHAR(128),
    traceparent VARCHAR(256),
    tracestate VARCHAR(512),
    worker_id VARCHAR(128),
    lease_token UUID,
    lease_expires_at TIMESTAMPTZ,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    error_code VARCHAR(64),
    error_message TEXT,
    CONSTRAINT migration_jobs_lease_fields_check CHECK (
        (status = 'running' AND worker_id IS NOT NULL AND lease_token IS NOT NULL AND lease_expires_at IS NOT NULL)
        OR (status <> 'running' AND worker_id IS NULL AND lease_token IS NULL AND lease_expires_at IS NULL)
    ),
    CONSTRAINT migration_jobs_terminal_time_check CHECK (
        (status IN ('succeeded', 'failed', 'partial', 'partially_reverted', 'needs_reconciliation'))
        = (finished_at IS NOT NULL)
    )
);

ALTER TABLE migration_execution_attempts
    ADD COLUMN lock_wait_micros BIGINT NOT NULL DEFAULT 0 CHECK (lock_wait_micros >= 0);

CREATE INDEX migration_execution_events_batch_metrics_idx
    ON migration_execution_events (event) INCLUDE (rows_affected)
    WHERE event IN ('batch_committed', 'batch_failed', 'batch_reverted');

CREATE INDEX migration_jobs_ready_idx
    ON migration_jobs (next_attempt_at, created_at, id)
    WHERE status IN ('queued', 'retrying');

CREATE INDEX migration_jobs_database_order_idx
    ON migration_jobs (database_id, created_at, id);

CREATE INDEX migration_jobs_expired_lease_idx
    ON migration_jobs (lease_expires_at)
    WHERE status = 'running';

CREATE UNIQUE INDEX migration_jobs_idempotency_idx
    ON migration_jobs (database_id, actor_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

-- +goose Down

DROP TABLE IF EXISTS migration_jobs;
DROP INDEX IF EXISTS migration_execution_events_batch_metrics_idx;
ALTER TABLE migration_execution_attempts DROP COLUMN IF EXISTS lock_wait_micros;
