-- 000022 down：重建空表结构（与 000013/015/016/017 对齐），不恢复数据。
-- public 幽灵表已迁至 tw_<project>，down 仅为可逆性占位。

CREATE TABLE IF NOT EXISTS public.payment_orders (
    id                  TEXT PRIMARY KEY,
    project_id          TEXT NOT NULL REFERENCES public.projects(id),
    user_id             TEXT NOT NULL,
    provider            TEXT NOT NULL,
    idempotency_key     TEXT NOT NULL,
    provider_session_id TEXT,
    provider_order_id   TEXT,
    amount              BIGINT NOT NULL CHECK (amount > 0),
    currency            TEXT NOT NULL,
    purpose_kind        TEXT NOT NULL,
    purpose             JSONB NOT NULL,
    status              TEXT NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    paid_at             TIMESTAMPTZ,
    expires_at          TIMESTAMPTZ NOT NULL,
    CONSTRAINT payment_orders_idempotency UNIQUE (project_id, idempotency_key)
);
CREATE UNIQUE INDEX IF NOT EXISTS payment_orders_provider_order ON public.payment_orders (provider, provider_order_id) WHERE provider_order_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS payment_orders_provider_session ON public.payment_orders (provider, provider_session_id) WHERE provider_session_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS payment_orders_user ON public.payment_orders (project_id, user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS payment_orders_close_scan ON public.payment_orders (expires_at) WHERE status IN ('created', 'paying');

CREATE TABLE IF NOT EXISTS public.payment_callback_events (
    id                TEXT PRIMARY KEY,
    project_id        TEXT,
    provider          TEXT NOT NULL,
    provider_event_id TEXT NOT NULL,
    event_type        TEXT NOT NULL,
    order_id          TEXT,
    payload           JSONB NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT payment_callback_events_dedupe UNIQUE (provider, provider_event_id)
);

CREATE TABLE IF NOT EXISTS public.payment_fulfillments (
    id           TEXT PRIMARY KEY,
    order_id     TEXT NOT NULL REFERENCES public.payment_orders(id),
    project_id   TEXT NOT NULL,
    purpose_kind TEXT NOT NULL,
    ref          TEXT NOT NULL,
    status       TEXT NOT NULL,
    detail       JSONB,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT payment_fulfillments_once UNIQUE (order_id, purpose_kind),
    CONSTRAINT payment_fulfillments_ref UNIQUE (ref)
);

CREATE TABLE IF NOT EXISTS public.asset_defs (
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
    CONSTRAINT asset_defs_code UNIQUE (project_id, code)
);

CREATE TABLE IF NOT EXISTS public.asset_holdings (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES public.projects(id),
    owner_type  TEXT NOT NULL DEFAULT 'user',
    owner_id    TEXT NOT NULL,
    def_id      TEXT NOT NULL REFERENCES public.asset_defs(id),
    quantity    BIGINT NOT NULL CHECK (quantity > 0),
    expires_at  TIMESTAMPTZ,
    level       INTEGER NOT NULL DEFAULT 0,
    metadata    JSONB,
    bucket_key  TEXT NOT NULL DEFAULT '',
    version     BIGINT NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT asset_holdings_bucket UNIQUE NULLS NOT DISTINCT (owner_type, owner_id, def_id, expires_at, bucket_key)
);

CREATE TABLE IF NOT EXISTS public.asset_ledger_entries (
    id               TEXT PRIMARY KEY,
    project_id       TEXT NOT NULL REFERENCES public.projects(id),
    holding_id       TEXT,
    owner_type       TEXT NOT NULL,
    owner_id         TEXT NOT NULL,
    def_id           TEXT NOT NULL REFERENCES public.asset_defs(id),
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
    CONSTRAINT asset_ledger_idempotency UNIQUE (project_id, idempotency_key)
);

CREATE TABLE IF NOT EXISTS public.usage_rollups (
    id           TEXT PRIMARY KEY,
    project_id   TEXT NOT NULL REFERENCES public.projects(id),
    metric       TEXT NOT NULL,
    period_start TIMESTAMPTZ NOT NULL,
    value        BIGINT NOT NULL CHECK (value >= 0),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT usage_rollups_bucket UNIQUE (project_id, metric, period_start)
);

CREATE TABLE IF NOT EXISTS public.billing_statements (
    id            TEXT PRIMARY KEY,
    project_id    TEXT NOT NULL REFERENCES public.projects(id),
    period_start  TIMESTAMPTZ NOT NULL,
    period_end    TIMESTAMPTZ NOT NULL,
    status        TEXT NOT NULL,
    details       JSONB NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finalized_at  TIMESTAMPTZ,
    CONSTRAINT billing_statements_period UNIQUE (project_id, period_start)
);

CREATE TABLE IF NOT EXISTS public.subscription_plans (
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
    CONSTRAINT subscription_plans_code UNIQUE (project_id, code)
);

CREATE TABLE IF NOT EXISTS public.subscriptions (
    id                    TEXT PRIMARY KEY,
    project_id            TEXT NOT NULL REFERENCES public.projects(id),
    user_id               TEXT NOT NULL,
    plan_id               TEXT NOT NULL REFERENCES public.subscription_plans(id),
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
    CONSTRAINT subscriptions_idempotency UNIQUE (project_id, idempotency_key)
);
