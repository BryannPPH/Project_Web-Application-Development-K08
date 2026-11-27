-- +goose Up
ALTER TABLE migration_execution_attempts
    ADD COLUMN target_journaled BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE migration_execution_attempts DROP COLUMN target_journaled;
