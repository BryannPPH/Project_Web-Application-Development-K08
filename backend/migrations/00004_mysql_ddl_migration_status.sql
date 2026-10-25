-- +goose Up

ALTER TABLE migration_entries
    ALTER COLUMN status TYPE VARCHAR(32);

ALTER TABLE migration_entries
    DROP CONSTRAINT migration_entries_status_check;

ALTER TABLE migration_entries
    ADD CONSTRAINT migration_entries_status_check
    CHECK (status IN ('draft', 'running', 'succeeded', 'failed', 'interrupted', 'needs_reconciliation'));

-- +goose Down

UPDATE migration_entries
SET status = 'interrupted',
    error_message = COALESCE(error_message, 'DDL migration was partially applied; reconcile target state')
WHERE status = 'needs_reconciliation';

ALTER TABLE migration_entries
    DROP CONSTRAINT migration_entries_status_check;

ALTER TABLE migration_entries
    ADD CONSTRAINT migration_entries_status_check
    CHECK (status IN ('draft', 'running', 'succeeded', 'failed', 'interrupted'));

ALTER TABLE migration_entries
    ALTER COLUMN status TYPE VARCHAR(16);
