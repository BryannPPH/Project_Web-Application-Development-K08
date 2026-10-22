-- +goose Up

-- Authentication accepts the previous token only during the configured grace
-- period. This partial index keeps that lookup bounded as session history grows.
CREATE INDEX sessions_previous_token_hash_active_idx
    ON sessions (previous_token_hash)
    WHERE previous_token_hash IS NOT NULL AND revoked_at IS NULL;

-- +goose Down

DROP INDEX IF EXISTS sessions_previous_token_hash_active_idx;
