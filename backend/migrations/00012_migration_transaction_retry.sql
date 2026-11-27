-- +goose Up
ALTER TABLE migration_jobs
    ADD COLUMN retry_execution_id UUID REFERENCES migration_execution_attempts(id) ON DELETE RESTRICT,
    ADD CONSTRAINT migration_jobs_one_execution_reference
        CHECK (execution_id IS NULL OR retry_execution_id IS NULL);

-- +goose Down
ALTER TABLE migration_jobs
    DROP CONSTRAINT migration_jobs_one_execution_reference,
    DROP COLUMN retry_execution_id;
