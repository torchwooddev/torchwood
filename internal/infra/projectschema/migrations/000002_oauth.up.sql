-- 项目 OAuth 配置。占位符 {{schema}} 由 Apply 替换为 quoteIdent。

CREATE TABLE IF NOT EXISTS {{schema}}.project_oauth_providers (
    project_id    TEXT NOT NULL REFERENCES public.projects(id),
    provider      TEXT NOT NULL,
    enabled       BOOLEAN NOT NULL DEFAULT TRUE,
    client_id     TEXT NOT NULL,
    client_secret TEXT NOT NULL,
    scopes        TEXT[] NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (project_id, provider)
);
