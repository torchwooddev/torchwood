-- Transactional outbox：用户集合文档写事件（v2 设计 §3.1）。
-- 与租户 schema 文档写同实例、同 COMMIT（event_id 即 outbox PK）。

CREATE TABLE IF NOT EXISTS document_events_outbox (
    event_id        TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL,
    topic           TEXT NOT NULL,
    payload         JSONB NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    available_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    attempts        INT NOT NULL DEFAULT 0,
    dispatched_at   TIMESTAMPTZ,
    published_at    TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS document_events_outbox_poll
    ON document_events_outbox (available_at)
    WHERE published_at IS NULL;

CREATE TABLE IF NOT EXISTS document_events_outbox_dead (
    event_id     TEXT PRIMARY KEY,
    project_id   TEXT NOT NULL,
    topic        TEXT NOT NULL,
    payload      JSONB NOT NULL,
    attempts     INT NOT NULL,
    last_error   TEXT,
    failed_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at   TIMESTAMPTZ NOT NULL
);
