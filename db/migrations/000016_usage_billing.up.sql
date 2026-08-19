-- v3 平台用量计费（设计 §4）：小时 rollup + 月账单文档。
-- 全部落元数据库 public schema 静态表（决策 D1）；数量一律 bigint 最小单位。
-- 不向 owner 收款、无发票 PDF、不挂钩配额/限流。

CREATE TABLE IF NOT EXISTS usage_rollups (
    id           TEXT PRIMARY KEY,                 -- ULID
    project_id   TEXT NOT NULL REFERENCES projects(id),
    metric       TEXT NOT NULL,                    -- api_calls | storage_bytes | function_duration_ms
    period_start TIMESTAMPTZ NOT NULL,             -- UTC 小时截断
    value        BIGINT NOT NULL CHECK (value >= 0),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- 幂等 upsert 键：同小时 bucket 重跑不翻倍（ON CONFLICT 覆盖为 Redis 当前值）。
    CONSTRAINT usage_rollups_bucket UNIQUE (project_id, metric, period_start)
);

CREATE INDEX IF NOT EXISTS usage_rollups_project_period
    ON usage_rollups (project_id, period_start DESC);

CREATE INDEX IF NOT EXISTS usage_rollups_project_metric_period
    ON usage_rollups (project_id, metric, period_start DESC);

CREATE TABLE IF NOT EXISTS billing_statements (
    id            TEXT PRIMARY KEY,                -- ULID
    project_id    TEXT NOT NULL REFERENCES projects(id),
    period_start  TIMESTAMPTZ NOT NULL,            -- UTC 月初
    period_end    TIMESTAMPTZ NOT NULL,            -- 下一 UTC 月初（不含）
    status        TEXT NOT NULL,                   -- draft | final
    details       JSONB NOT NULL,                  -- {metrics:{<metric>:int64}, hours:int}
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finalized_at  TIMESTAMPTZ,
    CONSTRAINT billing_statements_period UNIQUE (project_id, period_start)
);

CREATE INDEX IF NOT EXISTS billing_statements_project_period
    ON billing_statements (project_id, period_start DESC);
