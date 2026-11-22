-- +goose Up

ALTER TABLE migration_entries
    ADD COLUMN sql_down TEXT,
    ADD COLUMN irreversible_reason TEXT,
    ADD COLUMN is_active BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN execution_mode VARCHAR(16) NOT NULL DEFAULT 'single',
    ADD COLUMN batch_select_sql TEXT,
    ADD COLUMN batch_size INTEGER,
    ADD COLUMN batch_cursor_start BIGINT NOT NULL DEFAULT 0;

-- Milestone 1 definitions had no down SQL. Preserve them as explicitly irreversible.
UPDATE migration_entries
SET irreversible_reason = 'This migration predates reversible definitions and has no down SQL.'
WHERE sql_down IS NULL AND irreversible_reason IS NULL;

ALTER TABLE migration_entries
    ADD CONSTRAINT migration_entries_mode_check CHECK (execution_mode IN ('single', 'batch')),
    ADD CONSTRAINT migration_entries_reversibility_check CHECK (
        (sql_down IS NOT NULL AND length(btrim(sql_down)) > 0 AND irreversible_reason IS NULL)
        OR (sql_down IS NULL AND irreversible_reason IS NOT NULL AND length(btrim(irreversible_reason)) > 0)
    ),
    ADD CONSTRAINT migration_entries_batch_config_check CHECK (
        (execution_mode = 'single' AND batch_select_sql IS NULL AND batch_size IS NULL)
        OR (execution_mode = 'batch' AND batch_select_sql IS NOT NULL AND length(btrim(batch_select_sql)) > 0
            AND batch_size BETWEEN 1 AND 10000)
    );

UPDATE migration_entries SET is_active = TRUE WHERE status = 'succeeded';

ALTER TABLE migration_entries
    DROP CONSTRAINT migration_entries_status_check,
    ADD CONSTRAINT migration_entries_status_check CHECK (
        status IN ('draft', 'running', 'succeeded', 'failed', 'interrupted',
                   'needs_reconciliation', 'reconciled', 'rolled_back', 'partial', 'partially_reverted')
    );

CREATE TABLE migration_execution_attempts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    database_id UUID NOT NULL REFERENCES managed_databases(id) ON DELETE RESTRICT,
    migration_id UUID NOT NULL REFERENCES migration_entries(id) ON DELETE RESTRICT,
    version BIGINT NOT NULL,
    revision BIGINT NOT NULL,
    action VARCHAR(16) NOT NULL,
    actor_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    requested_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    sql_snapshot TEXT NOT NULL,
    related_execution_id UUID REFERENCES migration_execution_attempts(id) ON DELETE RESTRICT,
    checkpoint_root_id UUID NOT NULL,
    CONSTRAINT migration_execution_attempts_action_check
        CHECK (action IN ('apply', 'retry', 'rollback', 'forward')),
    CONSTRAINT migration_execution_attempts_version_positive CHECK (version > 0),
    CONSTRAINT migration_execution_attempts_revision_positive CHECK (revision > 0),
    CONSTRAINT migration_execution_attempts_sql_not_empty CHECK (length(btrim(sql_snapshot)) > 0),
    CONSTRAINT migration_execution_attempts_related_check CHECK (
        (action = 'apply' AND related_execution_id IS NULL)
        OR (action <> 'apply' AND related_execution_id IS NOT NULL)
    )
);

ALTER TABLE migration_entries
    ADD COLUMN last_execution_id UUID REFERENCES migration_execution_attempts(id) ON DELETE SET NULL;

CREATE INDEX migration_execution_attempts_database_time_idx
    ON migration_execution_attempts (database_id, requested_at DESC, id);
CREATE INDEX migration_execution_attempts_migration_time_idx
    ON migration_execution_attempts (migration_id, requested_at DESC, id);
CREATE INDEX migration_execution_attempts_related_idx
    ON migration_execution_attempts (related_execution_id) WHERE related_execution_id IS NOT NULL;
CREATE INDEX migration_execution_attempts_checkpoint_root_idx
    ON migration_execution_attempts (checkpoint_root_id);

CREATE TABLE migration_execution_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    execution_id UUID NOT NULL REFERENCES migration_execution_attempts(id) ON DELETE RESTRICT,
    status VARCHAR(32) NOT NULL,
    event VARCHAR(32) NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    error_message TEXT,
    query_sql TEXT,
    query_parameters JSONB,
    rows_affected BIGINT,
    batch_number BIGINT,
    cursor_start BIGINT,
    cursor_end BIGINT,
    CONSTRAINT migration_execution_events_status_check CHECK (
        status IN ('queued', 'running', 'succeeded', 'failed', 'interrupted',
                   'needs_reconciliation', 'partial', 'partially_reverted')
    ),
    CONSTRAINT migration_execution_events_event_check CHECK (
        event IN ('queued', 'started', 'batch_committed', 'batch_reverted', 'batch_failed', 'completed',
                  'failed', 'interrupted', 'needs_reconciliation', 'partial', 'partially_reverted')
    ),
    CONSTRAINT migration_execution_events_batch_fields_check CHECK (
        (batch_number IS NULL AND cursor_start IS NULL AND cursor_end IS NULL)
        OR (batch_number IS NOT NULL AND batch_number > 0 AND cursor_start IS NOT NULL
            AND cursor_end IS NOT NULL AND cursor_end >= cursor_start)
    )
);

CREATE INDEX migration_execution_events_latest_idx
    ON migration_execution_events (execution_id, id DESC);

-- An attempt's identifying data and events are immutable. State transitions are
-- represented by inserting a new event rather than updating history.
-- +goose StatementBegin
CREATE FUNCTION migration_forbid_history_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION '% is append-only', TG_TABLE_NAME;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER migration_execution_attempts_append_only
    BEFORE UPDATE OR DELETE ON migration_execution_attempts
    FOR EACH ROW EXECUTE FUNCTION migration_forbid_history_mutation();
CREATE TRIGGER migration_execution_events_append_only
    BEFORE UPDATE OR DELETE ON migration_execution_events
    FOR EACH ROW EXECUTE FUNCTION migration_forbid_history_mutation();

-- Once an attempt has captured a revision, that definition can never be edited.
-- +goose StatementBegin
CREATE FUNCTION migration_entries_freeze_executed_definition() RETURNS trigger AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM migration_execution_attempts WHERE migration_id = OLD.id)
       AND (NEW.name IS DISTINCT FROM OLD.name
            OR NEW.sql_text IS DISTINCT FROM OLD.sql_text
            OR NEW.sql_down IS DISTINCT FROM OLD.sql_down
            OR NEW.irreversible_reason IS DISTINCT FROM OLD.irreversible_reason
            OR NEW.execution_mode IS DISTINCT FROM OLD.execution_mode
            OR NEW.batch_select_sql IS DISTINCT FROM OLD.batch_select_sql
            OR NEW.batch_size IS DISTINCT FROM OLD.batch_size
            OR NEW.batch_cursor_start IS DISTINCT FROM OLD.batch_cursor_start) THEN
        RAISE EXCEPTION 'executed migration definition is immutable';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER migration_entries_freeze_executed_definition_trigger
    BEFORE UPDATE ON migration_entries
    FOR EACH ROW EXECUTE FUNCTION migration_entries_freeze_executed_definition();

-- Include every definition field in the approval revision.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION migration_entries_bump_revision() RETURNS trigger AS $$
BEGIN
    IF NEW.sql_text IS DISTINCT FROM OLD.sql_text
        OR NEW.name IS DISTINCT FROM OLD.name
        OR NEW.sql_down IS DISTINCT FROM OLD.sql_down
        OR NEW.irreversible_reason IS DISTINCT FROM OLD.irreversible_reason
        OR NEW.execution_mode IS DISTINCT FROM OLD.execution_mode
        OR NEW.batch_select_sql IS DISTINCT FROM OLD.batch_select_sql
        OR NEW.batch_size IS DISTINCT FROM OLD.batch_size
        OR NEW.batch_cursor_start IS DISTINCT FROM OLD.batch_cursor_start THEN
        NEW.revision := OLD.revision + 1;
    ELSE
        NEW.revision := OLD.revision;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose Down

ALTER TABLE migration_entries DROP COLUMN IF EXISTS last_execution_id;
DROP TRIGGER IF EXISTS migration_entries_freeze_executed_definition_trigger ON migration_entries;
DROP FUNCTION IF EXISTS migration_entries_freeze_executed_definition();
DROP TRIGGER IF EXISTS migration_execution_events_append_only ON migration_execution_events;
DROP TRIGGER IF EXISTS migration_execution_attempts_append_only ON migration_execution_attempts;
DROP FUNCTION IF EXISTS migration_forbid_history_mutation();
DROP TABLE IF EXISTS migration_execution_events;
DROP TABLE IF EXISTS migration_execution_attempts;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION migration_entries_bump_revision() RETURNS trigger AS $$
BEGIN
    IF NEW.sql_text IS DISTINCT FROM OLD.sql_text OR NEW.name IS DISTINCT FROM OLD.name THEN
        NEW.revision := OLD.revision + 1;
    ELSE
        NEW.revision := OLD.revision;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- Older schemas cannot represent partial or rolled-back states. Preserve the
-- fact that these entries need an operator review before removing the ledger.
UPDATE migration_entries
SET status = 'needs_reconciliation'
WHERE status IN ('rolled_back', 'partial', 'partially_reverted');

ALTER TABLE migration_entries
    DROP CONSTRAINT IF EXISTS migration_entries_status_check,
    ADD CONSTRAINT migration_entries_status_check CHECK (
        status IN ('draft', 'running', 'succeeded', 'failed', 'interrupted',
                   'needs_reconciliation', 'reconciled')
    ),
    DROP CONSTRAINT IF EXISTS migration_entries_batch_config_check,
    DROP CONSTRAINT IF EXISTS migration_entries_reversibility_check,
    DROP CONSTRAINT IF EXISTS migration_entries_mode_check,
    DROP COLUMN IF EXISTS batch_cursor_start,
    DROP COLUMN IF EXISTS batch_size,
    DROP COLUMN IF EXISTS batch_select_sql,
    DROP COLUMN IF EXISTS execution_mode,
    DROP COLUMN IF EXISTS is_active,
    DROP COLUMN IF EXISTS irreversible_reason,
    DROP COLUMN IF EXISTS sql_down;
