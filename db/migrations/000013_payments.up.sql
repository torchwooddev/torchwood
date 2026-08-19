-- v3 支付骨架（设计 §1.3 / §1.5）：订单 / 回调事件 / 履约记录。
-- 全部落元数据库 public schema 静态表（决策 D1）；金额一律 bigint 最小货币单位。

CREATE TABLE IF NOT EXISTS payment_orders (
    id                  TEXT PRIMARY KEY,              -- ULID
    project_id          TEXT NOT NULL REFERENCES projects(id),
    user_id             TEXT NOT NULL,                 -- 付款终端用户（client JWT sub）
    provider            TEXT NOT NULL,                 -- stripe | wechat | alipay | ios_iap
    idempotency_key     TEXT NOT NULL,                 -- 客户端建单幂等键
    provider_session_id TEXT,                          -- 渠道会话（Stripe Checkout Session cs_...）
    provider_order_id   TEXT,                          -- 渠道支付单（Stripe PaymentIntent pi_...）
    amount              BIGINT NOT NULL CHECK (amount > 0),
    currency            TEXT NOT NULL,                 -- ISO-4217 / 渠道约定，3 字母
    purpose_kind        TEXT NOT NULL,                 -- topup | item_purchase | subscription
    purpose             JSONB NOT NULL,
    status              TEXT NOT NULL,                 -- created|paying|paid|failed|closed|refunding|refunded
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    paid_at             TIMESTAMPTZ,
    expires_at          TIMESTAMPTZ NOT NULL,
    -- 幂等锚点一：客户端建单幂等键（同键返回原单，不新建）。
    CONSTRAINT payment_orders_idempotency UNIQUE (project_id, idempotency_key)
);

-- 幂等锚点二：渠道事件唯一（NULL 相互不冲突，Postgres 默认 NULLS DISTINCT）。
CREATE UNIQUE INDEX IF NOT EXISTS payment_orders_provider_order
    ON payment_orders (provider, provider_order_id)
    WHERE provider_order_id IS NOT NULL;

-- 回调按渠道会话定位订单（无 project 上下文，全局查）。
CREATE INDEX IF NOT EXISTS payment_orders_provider_session
    ON payment_orders (provider, provider_session_id)
    WHERE provider_session_id IS NOT NULL;

-- 本人订单查询（ListMyOrders）。
CREATE INDEX IF NOT EXISTS payment_orders_user
    ON payment_orders (project_id, user_id, created_at DESC);

-- 超时关单 worker 扫描（created/paying 且已过期）。
CREATE INDEX IF NOT EXISTS payment_orders_close_scan
    ON payment_orders (expires_at)
    WHERE status IN ('created', 'paying');

-- 渠道回调事件登记表：重放 / 重推按 (provider, provider_event_id) 幂等短路，
-- 状态机不重入（设计 §1.3）。
CREATE TABLE IF NOT EXISTS payment_callback_events (
    id                TEXT PRIMARY KEY,                -- ULID
    project_id        TEXT,                            -- 命中订单时为订单项目；未命中可空
    provider          TEXT NOT NULL,
    provider_event_id TEXT NOT NULL,                   -- Stripe event.id / 微信流水号 / notify_id / transactionId
    event_type        TEXT NOT NULL,                   -- 归一化类型：paid|failed|refunded|...
    order_id          TEXT,                            -- 命中的订单；解析不到订单时为 NULL
    payload           JSONB NOT NULL,                  -- 归一化 CallbackEvent（含原始事件摘要）
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT payment_callback_events_dedupe UNIQUE (provider, provider_event_id)
);

-- 履约记录（设计 §1.5）：订单翻 paid 同事务内插入；
-- PR1 仅记录（实际发放由 Fulfiller 端口 hook，PR2 联通资产系统）。
CREATE TABLE IF NOT EXISTS payment_fulfillments (
    id           TEXT PRIMARY KEY,                     -- ULID
    order_id     TEXT NOT NULL REFERENCES payment_orders(id),
    project_id   TEXT NOT NULL,
    purpose_kind TEXT NOT NULL,                        -- topup | item_purchase | subscription
    -- 幂等锚点三：履约 ref（一单一类履约恰好一次）。
    ref          TEXT NOT NULL,                        -- "order:{order_id}"；PR2 起指向资产 ledger entry
    status       TEXT NOT NULL,                        -- pending | done | failed
    detail       JSONB,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT payment_fulfillments_once UNIQUE (order_id, purpose_kind),
    CONSTRAINT payment_fulfillments_ref UNIQUE (ref)
);
