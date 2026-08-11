CREATE TABLE IF NOT EXISTS audit_logs (
    id          TEXT PRIMARY KEY,
    project_id  TEXT,
    actor_id    TEXT,
    actor_kind  TEXT NOT NULL,
    action      TEXT NOT NULL,
    status      TEXT NOT NULL,
    ip          TEXT,
    user_agent  TEXT,
    metadata    JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_audit_logs_project_created ON audit_logs(project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_actor_created ON audit_logs(actor_id, created_at DESC);

CREATE TABLE IF NOT EXISTS admin_projects (
    admin_id   TEXT NOT NULL REFERENCES admins(id) ON DELETE CASCADE,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (admin_id, project_id)
);
CREATE INDEX IF NOT EXISTS idx_admin_projects_project ON admin_projects(project_id);
