-- 还原 000012_document_transactions 的 schema（migrate down 仍可用）。

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
    conflict_columns TEXT[],
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (transaction_id, seq)
);
