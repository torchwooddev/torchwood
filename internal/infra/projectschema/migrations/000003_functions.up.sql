-- Functions 静态表。占位符 {{schema}} 由 Apply 替换为 quoteIdent。

CREATE TABLE IF NOT EXISTS {{schema}}.functions (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL,
    name            TEXT NOT NULL,
    runtime         TEXT NOT NULL,
    entrypoint      TEXT NOT NULL DEFAULT 'index.main',
    timeout_seconds INT NOT NULL DEFAULT 15,
    spec            TEXT NOT NULL DEFAULT 'shared-1x',
    enabled         BOOLEAN NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS functions_project_idx ON {{schema}}.functions (project_id);

CREATE TABLE IF NOT EXISTS {{schema}}.function_deployments (
    id          TEXT PRIMARY KEY,
    function_id TEXT NOT NULL REFERENCES {{schema}}.functions(id) ON DELETE CASCADE,
    project_id  TEXT NOT NULL,
    size        BIGINT NOT NULL DEFAULT 0,
    status      TEXT NOT NULL DEFAULT 'pending',
    error       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS function_deployments_function_idx ON {{schema}}.function_deployments (function_id);

CREATE TABLE IF NOT EXISTS {{schema}}.function_variables (
    id          TEXT PRIMARY KEY,
    function_id TEXT NOT NULL REFERENCES {{schema}}.functions(id) ON DELETE CASCADE,
    project_id  TEXT NOT NULL,
    key         TEXT NOT NULL,
    value       TEXT NOT NULL,
    UNIQUE (function_id, key)
);

CREATE TABLE IF NOT EXISTS {{schema}}.function_executions (
    id                  TEXT PRIMARY KEY,
    function_id         TEXT NOT NULL REFERENCES {{schema}}.functions(id) ON DELETE CASCADE,
    project_id          TEXT NOT NULL,
    deployment_id       TEXT NOT NULL REFERENCES {{schema}}.function_deployments(id) ON DELETE CASCADE,
    status              TEXT NOT NULL DEFAULT 'queued',
    response            TEXT NOT NULL DEFAULT '',
    response_truncated  BOOLEAN NOT NULL DEFAULT FALSE,
    stdout              TEXT NOT NULL DEFAULT '',
    stdout_truncated    BOOLEAN NOT NULL DEFAULT FALSE,
    stderr              TEXT NOT NULL DEFAULT '',
    stderr_truncated    BOOLEAN NOT NULL DEFAULT FALSE,
    status_code         INT NOT NULL DEFAULT 0,
    duration_ms         BIGINT NOT NULL DEFAULT 0,
    error               TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS function_executions_function_idx ON {{schema}}.function_executions (function_id, created_at DESC);
