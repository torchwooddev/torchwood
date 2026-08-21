-- 系统资源 staging 表（sys_*）。占位符 {{schema}} 由 Apply 替换为 quoteIdent。
-- 无 _tenant / _perms / _version / project_id。

CREATE TABLE IF NOT EXISTS {{schema}}.sys_users (
    id              TEXT PRIMARY KEY,
    email           VARCHAR(320) NOT NULL,
    password_hash   VARCHAR(512) NOT NULL DEFAULT '',
    name            VARCHAR(256) NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'active'
                    CHECK (status IN ('active', 'inactive', 'blocked')),
    email_verified  BOOLEAN NOT NULL DEFAULT FALSE,
    pending_email   VARCHAR(320) NOT NULL DEFAULT '',
    phone           VARCHAR(64) NOT NULL DEFAULT '',
    phone_verified  BOOLEAN NOT NULL DEFAULT FALSE,
    labels          JSONB NOT NULL DEFAULT '[]'::jsonb,
    prefs           JSONB NOT NULL DEFAULT '{}'::jsonb,
    factors         JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS sys_users_email_unique
    ON {{schema}}.sys_users (email);
CREATE INDEX IF NOT EXISTS sys_users_phone
    ON {{schema}}.sys_users (phone)
    WHERE phone <> '';

CREATE TABLE IF NOT EXISTS {{schema}}.sys_sessions (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES {{schema}}.sys_users(id) ON DELETE CASCADE,
    secret_hash  TEXT NOT NULL,
    provider     VARCHAR(64) NOT NULL DEFAULT 'email',
    user_agent   VARCHAR(1024) NOT NULL DEFAULT '',
    ip           VARCHAR(64) NOT NULL DEFAULT '',
    country      VARCHAR(8) NOT NULL DEFAULT '',
    factors      JSONB NOT NULL DEFAULT '{}'::jsonb,
    expire_at    TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS sys_sessions_user_id
    ON {{schema}}.sys_sessions (user_id);
CREATE INDEX IF NOT EXISTS sys_sessions_user_expire
    ON {{schema}}.sys_sessions (user_id, expire_at);

CREATE TABLE IF NOT EXISTS {{schema}}.sys_identities (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL REFERENCES {{schema}}.sys_users(id) ON DELETE CASCADE,
    provider        VARCHAR(64) NOT NULL,
    provider_uid    VARCHAR(256) NOT NULL,
    provider_email  VARCHAR(320) NOT NULL DEFAULT '',
    provider_data   JSONB NOT NULL DEFAULT '{}'::jsonb,
    expire_at       TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS sys_identities_user_id
    ON {{schema}}.sys_identities (user_id);
CREATE UNIQUE INDEX IF NOT EXISTS sys_identities_provider_uid
    ON {{schema}}.sys_identities (provider, provider_uid);

CREATE TABLE IF NOT EXISTS {{schema}}.sys_groups (
    id           TEXT PRIMARY KEY,
    name         VARCHAR(256) NOT NULL,
    permissions  JSONB NOT NULL DEFAULT '[]'::jsonb,
    total        BIGINT NOT NULL DEFAULT 0 CHECK (total >= 0),
    prefs        JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS sys_groups_name
    ON {{schema}}.sys_groups (name);

CREATE TABLE IF NOT EXISTS {{schema}}.sys_memberships (
    id          TEXT PRIMARY KEY,
    group_id    TEXT NOT NULL REFERENCES {{schema}}.sys_groups(id) ON DELETE CASCADE,
    user_id     TEXT REFERENCES {{schema}}.sys_users(id) ON DELETE CASCADE,
    email       VARCHAR(320) NOT NULL DEFAULT '',
    name        VARCHAR(256) NOT NULL DEFAULT '',
    roles       JSONB NOT NULL DEFAULT '[]'::jsonb,
    status      TEXT NOT NULL DEFAULT 'pending'
                CHECK (status IN ('pending', 'accepted', 'rejected')),
    invited_at  TIMESTAMPTZ,
    joined_at   TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (user_id IS NOT NULL OR email <> ''),
    CHECK (status <> 'accepted' OR user_id IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS sys_memberships_group_id
    ON {{schema}}.sys_memberships (group_id);
CREATE INDEX IF NOT EXISTS sys_memberships_user_id
    ON {{schema}}.sys_memberships (user_id)
    WHERE user_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS sys_memberships_email
    ON {{schema}}.sys_memberships (email)
    WHERE email <> '';
CREATE UNIQUE INDEX IF NOT EXISTS sys_memberships_group_user
    ON {{schema}}.sys_memberships (group_id, user_id)
    WHERE user_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS sys_memberships_group_email
    ON {{schema}}.sys_memberships (group_id, email)
    WHERE email <> '';

CREATE TABLE IF NOT EXISTS {{schema}}.sys_buckets (
    id           TEXT PRIMARY KEY,
    name         VARCHAR(256) NOT NULL,
    permissions  JSONB NOT NULL DEFAULT '[]'::jsonb,
    public       BOOLEAN NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS sys_buckets_name
    ON {{schema}}.sys_buckets (name);

CREATE TABLE IF NOT EXISTS {{schema}}.sys_files (
    id             TEXT PRIMARY KEY,
    bucket_id      TEXT NOT NULL REFERENCES {{schema}}.sys_buckets(id) ON DELETE CASCADE,
    name           VARCHAR(256) NOT NULL,
    mime_type      VARCHAR(128) NOT NULL DEFAULT '',
    size           BIGINT NOT NULL DEFAULT 0 CHECK (size >= 0),
    metadata       JSONB NOT NULL DEFAULT '{}'::jsonb,
    owner_user_id  TEXT REFERENCES {{schema}}.sys_users(id) ON DELETE SET NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS sys_files_bucket_id
    ON {{schema}}.sys_files (bucket_id);
CREATE INDEX IF NOT EXISTS sys_files_owner
    ON {{schema}}.sys_files (owner_user_id)
    WHERE owner_user_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS sys_files_name_fts
    ON {{schema}}.sys_files USING gin (to_tsvector('simple', name));
