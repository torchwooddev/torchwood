-- 项目资产：目录 / 持有 / 流水。占位符 {{schema}} 由 Apply 替换。

CREATE TABLE IF NOT EXISTS {{schema}}.asset_defs (
    id               TEXT PRIMARY KEY,
    project_id       TEXT NOT NULL REFERENCES public.projects(id),
    code             TEXT NOT NULL,
    name             TEXT NOT NULL,
    class            TEXT NOT NULL,
    decimals         SMALLINT NOT NULL DEFAULT 0,
    max_quantity     BIGINT,
    expires_in       BIGINT,
    tradable         BOOLEAN NOT NULL DEFAULT FALSE,
    unique_per_owner BOOLEAN NOT NULL DEFAULT FALSE,
    upgradeable      BOOLEAN NOT NULL DEFAULT FALSE,
    metadata         JSONB,
    status           TEXT NOT NULL DEFAULT 'active',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT asset_defs_code UNIQUE (project_id, code),
    CONSTRAINT asset_defs_class CHECK (class IN ('currency', 'stack', 'instance', 'entitlement')),
    CONSTRAINT asset_defs_status CHECK (status IN ('active', 'archived')),
    CONSTRAINT asset_defs_decimals CHECK (decimals >= 0 AND decimals <= 18),
    CONSTRAINT asset_defs_max_quantity CHECK (max_quantity IS NULL OR max_quantity > 0),
    CONSTRAINT asset_defs_expires_in CHECK (expires_in IS NULL OR expires_in > 0)
);

CREATE INDEX IF NOT EXISTS asset_defs_project
    ON {{schema}}.asset_defs (project_id, created_at DESC);

CREATE TABLE IF NOT EXISTS {{schema}}.asset_holdings (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES public.projects(id),
    owner_type  TEXT NOT NULL DEFAULT 'user',
    owner_id    TEXT NOT NULL,
    def_id      TEXT NOT NULL REFERENCES {{schema}}.asset_defs(id),
    quantity    BIGINT NOT NULL CHECK (quantity > 0),
    expires_at  TIMESTAMPTZ,
    level       INTEGER NOT NULL DEFAULT 0,
    metadata    JSONB,
    bucket_key  TEXT NOT NULL DEFAULT '',
    version     BIGINT NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT asset_holdings_owner_type CHECK (owner_type IN ('user', 'group', 'project')),
    CONSTRAINT asset_holdings_bucket UNIQUE NULLS NOT DISTINCT (owner_type, owner_id, def_id, expires_at, bucket_key)
);

CREATE INDEX IF NOT EXISTS asset_holdings_owner
    ON {{schema}}.asset_holdings (project_id, owner_type, owner_id);

CREATE INDEX IF NOT EXISTS asset_holdings_def_owner
    ON {{schema}}.asset_holdings (project_id, def_id, owner_type, owner_id);

CREATE INDEX IF NOT EXISTS asset_holdings_expire_scan
    ON {{schema}}.asset_holdings (expires_at)
    WHERE expires_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS {{schema}}.asset_ledger_entries (
    id               TEXT PRIMARY KEY,
    project_id       TEXT NOT NULL REFERENCES public.projects(id),
    holding_id       TEXT,
    owner_type       TEXT NOT NULL,
    owner_id         TEXT NOT NULL,
    def_id           TEXT NOT NULL REFERENCES {{schema}}.asset_defs(id),
    kind             TEXT NOT NULL,
    delta            BIGINT NOT NULL,
    quantity_after   BIGINT NOT NULL,
    expires_at       TIMESTAMPTZ,
    bucket_key       TEXT NOT NULL DEFAULT '',
    ref_type         TEXT,
    ref_id           TEXT,
    idempotency_key  TEXT NOT NULL,
    tx_id            TEXT,
    operator         JSONB,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT asset_ledger_kind CHECK (kind IN (
        'grant', 'consume', 'transfer_out', 'transfer_in', 'mutate', 'expire', 'adjust'
    )),
    CONSTRAINT asset_ledger_owner_type CHECK (owner_type IN ('user', 'group', 'project')),
    CONSTRAINT asset_ledger_idempotency UNIQUE (project_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS asset_ledger_owner
    ON {{schema}}.asset_ledger_entries (project_id, owner_type, owner_id, created_at DESC);

CREATE INDEX IF NOT EXISTS asset_ledger_def_owner
    ON {{schema}}.asset_ledger_entries (project_id, def_id, owner_id, created_at);

CREATE INDEX IF NOT EXISTS asset_ledger_ref
    ON {{schema}}.asset_ledger_entries (project_id, ref_type, ref_id)
    WHERE ref_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS asset_ledger_holding
    ON {{schema}}.asset_ledger_entries (holding_id)
    WHERE holding_id IS NOT NULL;
