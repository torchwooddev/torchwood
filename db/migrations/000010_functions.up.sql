-- Functions：函数/部署/变量/执行记录（静态表，bun + golang-migrate）。

CREATE TABLE IF NOT EXISTS functions (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL,
    name            TEXT NOT NULL,
    runtime         TEXT NOT NULL,              -- node-18.0 / python-3.11
    entrypoint      TEXT NOT NULL DEFAULT 'index.main',  -- MVP 仅占位
    timeout_seconds INT NOT NULL DEFAULT 15,
    spec            TEXT NOT NULL DEFAULT 'shared-1x',
    enabled         BOOLEAN NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_functions_project ON functions (project_id);

CREATE TABLE IF NOT EXISTS function_deployments (
    id          TEXT PRIMARY KEY,
    function_id TEXT NOT NULL REFERENCES functions(id) ON DELETE CASCADE,
    project_id  TEXT NOT NULL,
    size        BIGINT NOT NULL DEFAULT 0,
    status      TEXT NOT NULL DEFAULT 'pending',   -- pending/building/ready/failed
    error       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_function_deployments_function ON function_deployments (function_id);

CREATE TABLE IF NOT EXISTS function_variables (
    id          TEXT PRIMARY KEY,
    function_id TEXT NOT NULL REFERENCES functions(id) ON DELETE CASCADE,
    project_id  TEXT NOT NULL,
    key         TEXT NOT NULL,
    value       TEXT NOT NULL,                -- MVP 明文存储
    UNIQUE (function_id, key)
);

CREATE TABLE IF NOT EXISTS function_executions (
    id                  TEXT PRIMARY KEY,
    function_id         TEXT NOT NULL REFERENCES functions(id) ON DELETE CASCADE,
    project_id          TEXT NOT NULL,
    deployment_id       TEXT NOT NULL REFERENCES function_deployments(id) ON DELETE CASCADE,
    status              TEXT NOT NULL DEFAULT 'queued',    -- queued/building/running/completed/failed
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
CREATE INDEX IF NOT EXISTS idx_function_executions_function ON function_executions (function_id, created_at DESC);
