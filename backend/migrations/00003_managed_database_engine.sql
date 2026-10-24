-- +goose Up

ALTER TABLE managed_databases
    ADD COLUMN engine VARCHAR(16) NOT NULL DEFAULT 'postgresql';

ALTER TABLE managed_databases
    ADD CONSTRAINT managed_databases_engine_check
    CHECK (engine IN ('postgresql', 'mysql'));

-- +goose Down

ALTER TABLE managed_databases
    DROP CONSTRAINT IF EXISTS managed_databases_engine_check;

ALTER TABLE managed_databases
    DROP COLUMN IF EXISTS engine;
