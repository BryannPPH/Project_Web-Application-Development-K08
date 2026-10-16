-- +goose Up

CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username VARCHAR(64) NOT NULL,
    password_hash TEXT NOT NULL,
    platform_role VARCHAR(32) NOT NULL DEFAULT 'user',
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT users_username_length CHECK (length(btrim(username)) BETWEEN 3 AND 64),
    CONSTRAINT users_platform_role_check CHECK (platform_role IN ('user', 'platform_admin'))
);

CREATE UNIQUE INDEX users_username_lower_unique ON users (lower(username));

CREATE TABLE groups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL,
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT groups_name_length CHECK (length(btrim(name)) BETWEEN 1 AND 100)
);

CREATE UNIQUE INDEX groups_name_lower_unique ON groups (lower(name));

CREATE TABLE group_members (
    group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (group_id, user_id)
);

CREATE INDEX group_members_user_id_idx ON group_members (user_id);

CREATE TABLE managed_databases (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL,
    host TEXT NOT NULL,
    port INTEGER NOT NULL DEFAULT 5432,
    database_name TEXT NOT NULL,
    username TEXT NOT NULL,
    credential_ciphertext BYTEA NOT NULL,
    credential_key_version SMALLINT NOT NULL DEFAULT 1,
    ssl_mode VARCHAR(16) NOT NULL DEFAULT 'require',
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT managed_databases_name_length CHECK (length(btrim(name)) BETWEEN 1 AND 100),
    CONSTRAINT managed_databases_host_not_empty CHECK (length(btrim(host)) > 0),
    CONSTRAINT managed_databases_port_range CHECK (port BETWEEN 1 AND 65535),
    CONSTRAINT managed_databases_database_name_not_empty CHECK (length(btrim(database_name)) > 0),
    CONSTRAINT managed_databases_username_not_empty CHECK (length(btrim(username)) > 0),
    CONSTRAINT managed_databases_key_version_positive CHECK (credential_key_version > 0),
    CONSTRAINT managed_databases_ssl_mode_check CHECK (
        ssl_mode IN ('disable', 'allow', 'prefer', 'require', 'verify-ca', 'verify-full')
    )
);

CREATE UNIQUE INDEX managed_databases_name_lower_unique ON managed_databases (lower(name));

CREATE TABLE group_database_access (
    group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    database_id UUID NOT NULL REFERENCES managed_databases(id) ON DELETE CASCADE,
    access_level VARCHAR(16) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (group_id, database_id),
    CONSTRAINT group_database_access_level_check CHECK (access_level IN ('view_only', 'view_edit'))
);

CREATE INDEX group_database_access_database_id_idx ON group_database_access (database_id);

CREATE TABLE sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    current_token_hash BYTEA NOT NULL,
    previous_token_hash BYTEA,
    previous_token_valid_until TIMESTAMPTZ,
    csrf_token_hash BYTEA NOT NULL,
    last_activity_at TIMESTAMPTZ NOT NULL,
    rotated_at TIMESTAMPTZ NOT NULL,
    absolute_expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT sessions_previous_token_pair_check CHECK (
        (previous_token_hash IS NULL AND previous_token_valid_until IS NULL)
        OR (previous_token_hash IS NOT NULL AND previous_token_valid_until IS NOT NULL)
    ),
    CONSTRAINT sessions_absolute_expiry_check CHECK (absolute_expires_at > created_at)
);

CREATE UNIQUE INDEX sessions_current_token_hash_unique ON sessions (current_token_hash);
CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_active_expiry_idx
    ON sessions (absolute_expires_at)
    WHERE revoked_at IS NULL;

CREATE TABLE migration_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    database_id UUID NOT NULL REFERENCES managed_databases(id) ON DELETE RESTRICT,
    version BIGINT NOT NULL,
    name VARCHAR(160) NOT NULL,
    sql_text TEXT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'draft',
    created_by UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    executed_by UUID REFERENCES users(id) ON DELETE SET NULL,
    error_message TEXT,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT migration_entries_version_positive CHECK (version > 0),
    CONSTRAINT migration_entries_name_not_empty CHECK (length(btrim(name)) > 0),
    CONSTRAINT migration_entries_sql_not_empty CHECK (length(btrim(sql_text)) > 0),
    CONSTRAINT migration_entries_status_check CHECK (
        status IN ('draft', 'running', 'succeeded', 'failed', 'interrupted')
    ),
    CONSTRAINT migration_entries_finished_after_started CHECK (
        finished_at IS NULL OR started_at IS NULL OR finished_at >= started_at
    ),
    UNIQUE (database_id, version)
);

CREATE INDEX migration_entries_database_status_idx
    ON migration_entries (database_id, status);
CREATE INDEX migration_entries_created_by_idx ON migration_entries (created_by);

-- +goose Down

DROP TABLE IF EXISTS migration_entries;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS group_database_access;
DROP TABLE IF EXISTS managed_databases;
DROP TABLE IF EXISTS group_members;
DROP TABLE IF EXISTS groups;
DROP TABLE IF EXISTS users;

