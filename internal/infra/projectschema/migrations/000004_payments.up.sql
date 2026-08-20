-- 项目账本：订单 / 回调事件 / 履约。占位符 {{schema}} 由 Apply 替换。

CREATE TABLE IF NOT EXISTS {{schema}}.payment_orders (
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

CREATE UNIQUE INDEX IF NOT EXISTS payment_orders_provider_order
    ON {{schema}}.payment_orders (provider, provider_order_id)
    WHERE provider_order_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS payment_orders_provider_session
    ON {{schema}}.payment_orders (provider, provider_session_id)
    WHERE provider_session_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS payment_orders_user
    ON {{schema}}.payment_orders (project_id, user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS payment_orders_close_scan
    ON {{schema}}.payment_orders (expires_at)
    WHERE status IN ('created', 'paying');

CREATE TABLE IF NOT EXISTS {{schema}}.payment_callback_events (
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

CREATE TABLE IF NOT EXISTS {{schema}}.payment_fulfillments (
    id           TEXT PRIMARY KEY,
    order_id     TEXT NOT NULL REFERENCES {{schema}}.payment_orders(id),
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
