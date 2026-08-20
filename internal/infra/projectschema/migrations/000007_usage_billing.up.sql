-- 项目用量与月账单。占位符 {{schema}} 由 Apply 替换。

CREATE TABLE IF NOT EXISTS {{schema}}.usage_rollups (
    id           TEXT PRIMARY KEY,
    project_id   TEXT NOT NULL REFERENCES public.projects(id),
    metric       TEXT NOT NULL,
    period_start TIMESTAMPTZ NOT NULL,
    value        BIGINT NOT NULL CHECK (value >= 0),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT usage_rollups_bucket UNIQUE (project_id, metric, period_start)
);

CREATE INDEX IF NOT EXISTS usage_rollups_project_period
    ON {{schema}}.usage_rollups (project_id, period_start DESC);

CREATE INDEX IF NOT EXISTS usage_rollups_project_metric_period
    ON {{schema}}.usage_rollups (project_id, metric, period_start DESC);

CREATE TABLE IF NOT EXISTS {{schema}}.billing_statements (
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

CREATE INDEX IF NOT EXISTS billing_statements_project_period
    ON {{schema}}.billing_statements (project_id, period_start DESC);
