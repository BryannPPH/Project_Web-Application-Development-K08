-- +goose Up

ALTER TABLE migration_entries
    ALTER COLUMN status TYPE VARCHAR(32);

ALTER TABLE migration_entries
    DROP CONSTRAINT migration_entries_status_check;

ALTER TABLE migration_entries
    ADD CONSTRAINT migration_entries_status_check
    CHECK (status IN ('draft', 'running', 'succeeded', 'failed', 'interrupted', 'needs_reconciliation', 'reconciled'));

ALTER TABLE migration_entries
    ADD COLUMN reconciled_from_status VARCHAR(32),
    ADD COLUMN reconciled_by UUID REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN reconciled_at TIMESTAMPTZ,
    ADD COLUMN reconciliation_note TEXT;

ALTER TABLE migration_entries
    ADD CONSTRAINT migration_entries_reconciliation_fields_check
    CHECK (
        (status = 'reconciled'
            AND reconciled_from_status IS NOT NULL
            AND reconciled_from_status IN ('interrupted', 'needs_reconciliation')
            AND reconciled_at IS NOT NULL
            AND reconciliation_note IS NOT NULL
            AND length(btrim(reconciliation_note)) > 0)
        OR
        (status <> 'reconciled'
            AND reconciled_from_status IS NULL
            AND reconciled_by IS NULL
            AND reconciled_at IS NULL
            AND reconciliation_note IS NULL)
    );

-- +goose Down

UPDATE migration_entries
SET status = COALESCE(reconciled_from_status, 'interrupted'),
    reconciled_from_status = NULL,
    reconciled_by = NULL,
    reconciled_at = NULL,
    reconciliation_note = NULL
WHERE status = 'reconciled';

ALTER TABLE migration_entries
    DROP CONSTRAINT migration_entries_reconciliation_fields_check;

ALTER TABLE migration_entries
    DROP COLUMN reconciled_from_status,
    DROP COLUMN reconciled_by,
    DROP COLUMN reconciled_at,
    DROP COLUMN reconciliation_note;

ALTER TABLE migration_entries
    DROP CONSTRAINT migration_entries_status_check;

ALTER TABLE migration_entries
    ADD CONSTRAINT migration_entries_status_check
    CHECK (status IN ('draft', 'running', 'succeeded', 'failed', 'interrupted', 'needs_reconciliation'));
