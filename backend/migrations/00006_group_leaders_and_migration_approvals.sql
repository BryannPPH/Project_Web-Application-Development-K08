-- +goose Up

-- Satu leader per group. FK komposit memastikan leader adalah anggota group;
-- leader yang dikeluarkan dari group otomatis tercopot (CASCADE).
CREATE TABLE group_leaders (
    group_id UUID PRIMARY KEY,
    user_id UUID NOT NULL,
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT group_leaders_member_fk
        FOREIGN KEY (group_id, user_id)
        REFERENCES group_members (group_id, user_id)
        ON DELETE CASCADE
);

CREATE INDEX group_leaders_user_id_idx ON group_leaders (user_id);

-- Revisi naik setiap nama/SQL diedit; approval diikat ke revisi.
ALTER TABLE migration_entries
    ADD COLUMN revision BIGINT NOT NULL DEFAULT 1,
    ADD CONSTRAINT migration_entries_revision_positive CHECK (revision > 0);

-- +goose StatementBegin
CREATE FUNCTION migration_entries_bump_revision() RETURNS trigger AS $$
BEGIN
    IF NEW.sql_text IS DISTINCT FROM OLD.sql_text
        OR NEW.name IS DISTINCT FROM OLD.name THEN
        NEW.revision := OLD.revision + 1;
    ELSE
        NEW.revision := OLD.revision;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER migration_entries_revision_trigger
    BEFORE UPDATE ON migration_entries
    FOR EACH ROW
    EXECUTE FUNCTION migration_entries_bump_revision();

-- Keputusan leader per (migration, action, revision). Append-only: keputusan
-- terbaru per approver yang dihitung, riwayat lama tetap tersimpan.
CREATE TABLE migration_approvals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    migration_id UUID NOT NULL REFERENCES migration_entries (id) ON DELETE CASCADE,
    action VARCHAR(16) NOT NULL,
    revision BIGINT NOT NULL,
    approver_id UUID NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    decision VARCHAR(16) NOT NULL,
    note TEXT,
    decided_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT migration_approvals_action_check CHECK (action IN ('apply', 'rollback', 'forward')),
    CONSTRAINT migration_approvals_decision_check CHECK (decision IN ('approved', 'rejected')),
    CONSTRAINT migration_approvals_revision_positive CHECK (revision > 0),
    CONSTRAINT migration_approvals_note_length CHECK (note IS NULL OR length(note) <= 1000)
);

CREATE INDEX migration_approvals_lookup_idx
    ON migration_approvals (migration_id, action, revision, approver_id, decided_at DESC);

-- +goose StatementBegin
CREATE FUNCTION migration_approvals_forbid_update() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'migration_approvals is append-only';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER migration_approvals_append_only
    BEFORE UPDATE ON migration_approvals
    FOR EACH ROW
    EXECUTE FUNCTION migration_approvals_forbid_update();

-- +goose Down

DROP TRIGGER IF EXISTS migration_approvals_append_only ON migration_approvals;
DROP FUNCTION IF EXISTS migration_approvals_forbid_update();
DROP TABLE IF EXISTS migration_approvals;

DROP TRIGGER IF EXISTS migration_entries_revision_trigger ON migration_entries;
DROP FUNCTION IF EXISTS migration_entries_bump_revision();
ALTER TABLE migration_entries
    DROP CONSTRAINT IF EXISTS migration_entries_revision_positive,
    DROP COLUMN IF EXISTS revision;

DROP TABLE IF EXISTS group_leaders;
