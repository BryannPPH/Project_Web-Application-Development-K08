-- +goose Up
ALTER TABLE migration_entries DROP CONSTRAINT migration_entries_reconciliation_fields_check;

ALTER TABLE migration_entries ADD CONSTRAINT migration_entries_reconciliation_fields_check
CHECK (
    (status <> 'reconciled' AND reconciled_from_status IS NULL AND reconciled_by IS NULL
        AND reconciled_at IS NULL AND reconciliation_note IS NULL)
    OR
    (status IN ('reconciled', 'succeeded', 'failed', 'rolled_back')
        AND reconciled_from_status IN ('interrupted', 'needs_reconciliation')
        AND reconciled_from_status IS NOT NULL
        AND reconciled_at IS NOT NULL
        AND reconciliation_note IS NOT NULL
        AND length(btrim(reconciliation_note)) > 0)
);

-- +goose Down
UPDATE migration_entries
SET reconciled_from_status = NULL, reconciled_by = NULL, reconciled_at = NULL, reconciliation_note = NULL
WHERE status <> 'reconciled' AND reconciled_at IS NOT NULL;

ALTER TABLE migration_entries DROP CONSTRAINT migration_entries_reconciliation_fields_check;

ALTER TABLE migration_entries ADD CONSTRAINT migration_entries_reconciliation_fields_check
CHECK (
    (status <> 'reconciled' AND reconciled_from_status IS NULL AND reconciled_by IS NULL
        AND reconciled_at IS NULL AND reconciliation_note IS NULL)
    OR
    (status = 'reconciled'
        AND reconciled_from_status IN ('interrupted', 'needs_reconciliation')
        AND reconciled_from_status IS NOT NULL
        AND reconciled_at IS NOT NULL
        AND reconciliation_note IS NOT NULL
        AND length(btrim(reconciliation_note)) > 0)
);
