-- v3 统一资产系统（设计 §2.4）：目录 / 持有 / 流水。
-- 全部落元数据库 public schema 静态表（决策 D1）；数量一律 bigint 最小单位。

CREATE TABLE IF NOT EXISTS asset_defs (
    id               TEXT PRIMARY KEY,                 -- ULID
    project_id       TEXT NOT NULL REFERENCES projects(id),
    code             TEXT NOT NULL,                    -- 项目内唯一
    name             TEXT NOT NULL,
    class            TEXT NOT NULL,                    -- currency | stack | instance | entitlement
    decimals         SMALLINT NOT NULL DEFAULT 0,      -- 仅 currency 展示用；数量仍为 bigint
    max_quantity     BIGINT,                           -- stack/currency 上限，NULL=不限
    expires_in       BIGINT,                           -- 默认 TTL 秒，NULL=无；currency 禁止
    tradable         BOOLEAN NOT NULL DEFAULT FALSE,
    unique_per_owner BOOLEAN NOT NULL DEFAULT FALSE,
    upgradeable      BOOLEAN NOT NULL DEFAULT FALSE,
    metadata         JSONB,
    status           TEXT NOT NULL DEFAULT 'active',   -- active | archived
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
    ON asset_defs (project_id, created_at DESC);

-- holdings 是物化视图，ledger 是真相（D11）。
-- UNIQUE(owner_type, owner_id, def_id, expires_at) 是设计合并键；
-- Postgres UNIQUE 默认 NULLS DISTINCT，currency 的 expires_at 恒 NULL 会允许多行，
-- 故 NULLS NOT DISTINCT。
-- instance 每行一个个体：bucket_key = holding.id 打破并桶，否则同到期的两把
-- 独立实例会撞合并键（改名卡等多份同质物品应建模为 stack，见设计 §2.3）。
CREATE TABLE IF NOT EXISTS asset_holdings (
    id          TEXT PRIMARY KEY,                      -- ULID
    project_id  TEXT NOT NULL REFERENCES projects(id),
    owner_type  TEXT NOT NULL DEFAULT 'user',          -- 一期仅 user（D14）；列保留 team/project
    owner_id    TEXT NOT NULL,
    def_id      TEXT NOT NULL REFERENCES asset_defs(id),
    quantity    BIGINT NOT NULL CHECK (quantity > 0), -- 消耗到 0 则删行，不留尸体
    expires_at  TIMESTAMPTZ,                           -- currency 恒 NULL
    level       INTEGER NOT NULL DEFAULT 0,
    metadata    JSONB,
    bucket_key  TEXT NOT NULL DEFAULT '',              -- 非 instance 空串；instance = id
    version     BIGINT NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT asset_holdings_owner_type CHECK (owner_type IN ('user', 'team', 'project')),
    CONSTRAINT asset_holdings_bucket UNIQUE NULLS NOT DISTINCT (owner_type, owner_id, def_id, expires_at, bucket_key)
);

CREATE INDEX IF NOT EXISTS asset_holdings_owner
    ON asset_holdings (project_id, owner_type, owner_id);

CREATE INDEX IF NOT EXISTS asset_holdings_def_owner
    ON asset_holdings (project_id, def_id, owner_type, owner_id);

-- 到期扫描 worker（expires_at <= now）。
CREATE INDEX IF NOT EXISTS asset_holdings_expire_scan
    ON asset_holdings (expires_at)
    WHERE expires_at IS NOT NULL;

-- append-only 流水（禁止 UPDATE/DELETE；纠错用反向 adjust entry）。
-- holding_id 不建 FK：消耗/过期删 holding 后流水仍保留当时的 holding_id 作为历史引用。
-- expires_at 冗余落列，供删行后按合并键重放分桶（D11 对账）。
CREATE TABLE IF NOT EXISTS asset_ledger_entries (
    id               TEXT PRIMARY KEY,                 -- ULID
    project_id       TEXT NOT NULL REFERENCES projects(id),
    holding_id       TEXT,                             -- 可空：删行后仍保留原 id
    owner_type       TEXT NOT NULL,
    owner_id         TEXT NOT NULL,
    def_id           TEXT NOT NULL REFERENCES asset_defs(id),
    kind             TEXT NOT NULL,                    -- grant|consume|transfer_out|transfer_in|mutate|expire|adjust
    delta            BIGINT NOT NULL,
    quantity_after   BIGINT NOT NULL,
    expires_at       TIMESTAMPTZ,                      -- 当时分桶的到期时刻（currency 恒 NULL）
    bucket_key       TEXT NOT NULL DEFAULT '',
    ref_type         TEXT,
    ref_id           TEXT,
    idempotency_key  TEXT NOT NULL,
    tx_id            TEXT,                             -- 预留 D2（文档+资产一单原子）
    operator         JSONB,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT asset_ledger_kind CHECK (kind IN (
        'grant', 'consume', 'transfer_out', 'transfer_in', 'mutate', 'expire', 'adjust'
    )),
    CONSTRAINT asset_ledger_owner_type CHECK (owner_type IN ('user', 'team', 'project')),
    -- 幂等锚点：客户端幂等键按项目唯一（租户隔离，避免跨项目碰撞）。
    CONSTRAINT asset_ledger_idempotency UNIQUE (project_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS asset_ledger_owner
    ON asset_ledger_entries (project_id, owner_type, owner_id, created_at DESC);

CREATE INDEX IF NOT EXISTS asset_ledger_def_owner
    ON asset_ledger_entries (project_id, def_id, owner_id, created_at);

CREATE INDEX IF NOT EXISTS asset_ledger_ref
    ON asset_ledger_entries (project_id, ref_type, ref_id)
    WHERE ref_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS asset_ledger_holding
    ON asset_ledger_entries (holding_id)
    WHERE holding_id IS NOT NULL;
