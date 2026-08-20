-- 项目订阅：计划 / 合同。占位符 {{schema}} 由 Apply 替换。

CREATE TABLE IF NOT EXISTS {{schema}}.subscription_plans (
    id                  TEXT PRIMARY KEY,
    project_id          TEXT NOT NULL REFERENCES public.projects(id),
    code                TEXT NOT NULL,
    name                TEXT NOT NULL,
    amount              BIGINT NOT NULL CHECK (amount >= 0),
    currency            TEXT NOT NULL,
    interval            TEXT NOT NULL,
    interval_days       BIGINT NOT NULL DEFAULT 0,
    grace_days          INTEGER NOT NULL DEFAULT 0 CHECK (grace_days >= 0),
    trial_days          INTEGER NOT NULL DEFAULT 0 CHECK (trial_days >= 0),
    benefits            JSONB NOT NULL DEFAULT '{"grants":[],"entitlements":[]}'::jsonb,
    provider_overrides  JSONB,
    status              TEXT NOT NULL DEFAULT 'active',
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
    ON {{schema}}.subscription_plans (project_id, created_at DESC);

CREATE TABLE IF NOT EXISTS {{schema}}.subscriptions (
    id                    TEXT PRIMARY KEY,
    project_id            TEXT NOT NULL REFERENCES public.projects(id),
    user_id               TEXT NOT NULL,
    plan_id               TEXT NOT NULL REFERENCES {{schema}}.subscription_plans(id),
    mode                  TEXT NOT NULL,
    provider              TEXT,
    provider_sub_id       TEXT,
    status                TEXT NOT NULL,
    current_period_start  TIMESTAMPTZ NOT NULL,
    current_period_end    TIMESTAMPTZ NOT NULL,
    cancel_at_period_end  BOOLEAN NOT NULL DEFAULT FALSE,
    grace_until           TIMESTAMPTZ,
    billing_asset_code    TEXT,
    benefits              JSONB NOT NULL,
    idempotency_key       TEXT NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT subscriptions_mode CHECK (mode IN ('hosted', 'platform')),
    CONSTRAINT subscriptions_status CHECK (status IN (
        'trialing', 'active', 'past_due', 'canceled', 'expired'
    )),
    CONSTRAINT subscriptions_idempotency UNIQUE (project_id, idempotency_key)
);

CREATE UNIQUE INDEX IF NOT EXISTS subscriptions_provider_sub
    ON {{schema}}.subscriptions (provider, provider_sub_id)
    WHERE provider_sub_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS subscriptions_user
    ON {{schema}}.subscriptions (project_id, user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS subscriptions_project
    ON {{schema}}.subscriptions (project_id, created_at DESC);

CREATE INDEX IF NOT EXISTS subscriptions_billing_scan
    ON {{schema}}.subscriptions (current_period_end)
    WHERE mode = 'platform' AND status IN ('trialing', 'active', 'past_due');

CREATE INDEX IF NOT EXISTS subscriptions_grace_scan
    ON {{schema}}.subscriptions (grace_until)
    WHERE status = 'past_due' AND grace_until IS NOT NULL;
