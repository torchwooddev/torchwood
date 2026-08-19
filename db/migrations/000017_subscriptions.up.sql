-- v3 订阅（设计 §3）：计划 / 合同。000016 预留给并行 PR5 usage_billing。
-- 全部落元数据库 public schema 静态表（决策 D1）；金额一律 bigint 最小货币单位。

CREATE TABLE IF NOT EXISTS subscription_plans (
    id                  TEXT PRIMARY KEY,              -- ULID
    project_id          TEXT NOT NULL REFERENCES projects(id),
    code                TEXT NOT NULL,                 -- 项目内唯一
    name                TEXT NOT NULL,
    amount              BIGINT NOT NULL CHECK (amount >= 0), -- 最小货币单位；0 = 免费计划
    currency            TEXT NOT NULL,                 -- ISO-4217 三字母
    interval            TEXT NOT NULL,                 -- month | year | custom_days
    interval_days       BIGINT NOT NULL DEFAULT 0,     -- custom_days 必填 >0；其余忽略
    grace_days          INTEGER NOT NULL DEFAULT 0 CHECK (grace_days >= 0), -- 宽限期（D16）
    trial_days          INTEGER NOT NULL DEFAULT 0 CHECK (trial_days >= 0),
    benefits            JSONB NOT NULL DEFAULT '{"grants":[],"entitlements":[]}'::jsonb,
    provider_overrides  JSONB,                         -- {stripe_price_id:...}
    status              TEXT NOT NULL DEFAULT 'active', -- active | archived
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT subscription_plans_code UNIQUE (project_id, code),
    CONSTRAINT subscription_plans_interval CHECK (interval IN ('month', 'year', 'custom_days')),
    CONSTRAINT subscription_plans_status CHECK (status IN ('active', 'archived')),
    CONSTRAINT subscription_plans_currency CHECK (char_length(currency) = 3),
    CONSTRAINT subscription_plans_custom_days CHECK (
        (interval <> 'custom_days') OR (interval_days > 0)
    )
);

CREATE INDEX IF NOT EXISTS subscription_plans_project
    ON subscription_plans (project_id, created_at DESC);

CREATE TABLE IF NOT EXISTS subscriptions (
    id                    TEXT PRIMARY KEY,            -- ULID
    project_id            TEXT NOT NULL REFERENCES projects(id),
    user_id               TEXT NOT NULL,
    plan_id               TEXT NOT NULL REFERENCES subscription_plans(id),
    mode                  TEXT NOT NULL,               -- hosted | platform
    provider              TEXT,                        -- stripe | （platform 可空或 stripe 一次性订单）
    provider_sub_id       TEXT,                        -- 渠道侧订阅 id（hosted）
    status                TEXT NOT NULL,               -- trialing|active|past_due|canceled|expired
    current_period_start  TIMESTAMPTZ NOT NULL,
    current_period_end    TIMESTAMPTZ NOT NULL,
    cancel_at_period_end  BOOLEAN NOT NULL DEFAULT FALSE,
    grace_until           TIMESTAMPTZ,                 -- past_due 宽限截止 = now + plan.grace_days
    billing_asset_code    TEXT,                        -- platform 余额扣款的 currency/stack def code
    benefits              JSONB NOT NULL,              -- 订阅时从 plan 快照，履约不跟随后续改计划
    idempotency_key       TEXT NOT NULL,               -- 客户端 Subscribe 幂等键
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT subscriptions_mode CHECK (mode IN ('hosted', 'platform')),
    CONSTRAINT subscriptions_status CHECK (status IN (
        'trialing', 'active', 'past_due', 'canceled', 'expired'
    )),
    CONSTRAINT subscriptions_idempotency UNIQUE (project_id, idempotency_key)
);

-- hosted 镜像：渠道订阅 id 全局唯一（NULL 互不冲突）。
CREATE UNIQUE INDEX IF NOT EXISTS subscriptions_provider_sub
    ON subscriptions (provider, provider_sub_id)
    WHERE provider_sub_id IS NOT NULL;

-- 本人订阅查询。
CREATE INDEX IF NOT EXISTS subscriptions_user
    ON subscriptions (project_id, user_id, created_at DESC);

-- 项目订阅列表（Server/Console）。
CREATE INDEX IF NOT EXISTS subscriptions_project
    ON subscriptions (project_id, created_at DESC);

-- platform worker：到期扣款 / 期末取消 / 宽限过期。
CREATE INDEX IF NOT EXISTS subscriptions_billing_scan
    ON subscriptions (current_period_end)
    WHERE mode = 'platform' AND status IN ('trialing', 'active', 'past_due');

CREATE INDEX IF NOT EXISTS subscriptions_grace_scan
    ON subscriptions (grace_until)
    WHERE status = 'past_due' AND grace_until IS NOT NULL;
