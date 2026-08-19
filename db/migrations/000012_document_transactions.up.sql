-- 单库事务元数据（v2 设计 §5.1）：暂存 create/update/delete/upsert 操作，
-- Commit 时在单段事务内按 seq 应用并写 outbox。

CREATE TABLE IF NOT EXISTS document_transactions (
    id           TEXT PRIMARY KEY,
    project_id   TEXT NOT NULL REFERENCES projects(id),
    database_id  TEXT NOT NULL,
    status       TEXT NOT NULL,
    created_by   TEXT NOT NULL,
    expire_at    TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 同一 created_by + project + database 仅允许 1 个 pending；
-- 竞态第二次 Create 命中 23505 → transaction_already_pending。
CREATE UNIQUE INDEX IF NOT EXISTS document_transactions_one_pending
    ON document_transactions (created_by, project_id, database_id)
    WHERE status = 'pending';

CREATE TABLE IF NOT EXISTS document_transaction_ops (
    id               TEXT PRIMARY KEY,
    transaction_id   TEXT NOT NULL REFERENCES document_transactions(id) ON DELETE CASCADE,
    seq              INT NOT NULL,
    op_type          TEXT NOT NULL,
    collection_id    TEXT NOT NULL,
    document_id      TEXT NOT NULL,
    data             JSONB,
    permissions      TEXT[],
    increment        JSONB,
    version          BIGINT,
    -- upsert 的冲突列需在 Commit 时原样回放，随 op 持久化（设计 §5.1 增补列）。
    conflict_columns TEXT[],
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (transaction_id, seq)
);
