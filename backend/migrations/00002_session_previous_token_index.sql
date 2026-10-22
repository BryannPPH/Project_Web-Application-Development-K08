-- +goose Up

-- Token sebelumnya hanya diterima selama grace period yang dikonfigurasi.
-- Indeks parsial ini menjaga pencarian tetap efisien saat riwayat sesi bertambah.
CREATE INDEX sessions_previous_token_hash_active_idx
    ON sessions (previous_token_hash)
    WHERE previous_token_hash IS NOT NULL AND revoked_at IS NULL;

-- +goose Down

DROP INDEX IF EXISTS sessions_previous_token_hash_active_idx;
