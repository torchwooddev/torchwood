-- catalog_migrations：schema 演进 copy 迁移任务账本（转出 POC 门禁 B4，redesign
-- §4.6 / 预决策 3）。改类型/收紧 = 新列（物理名带版本后缀）→ 异步批量回填
-- （批 500 行、限速、游标可恢复）→ 锁窗校验 → 原子 swap（RENAME 列）→ 旧列
-- deprecated。任务行承载：进度（cursor_id/rows_done）、阶段
--（backfilling|swapped|retired|failed）、swap 后旧列的物理名（old_physical
-- —— retired 时 DROP 的目标）。schema_version 在 swap commit 时递增。
CREATE TABLE catalog_migrations (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    database_id TEXT NOT NULL,
    collection_id TEXT NOT NULL,
    attr_key TEXT NOT NULL,
    from_attr JSONB NOT NULL,
    to_attr JSONB NOT NULL,
    old_physical TEXT NOT NULL,
    new_physical TEXT NOT NULL,
    phase TEXT NOT NULL DEFAULT 'backfilling',
    cursor_id TEXT,
    rows_done BIGINT NOT NULL DEFAULT 0,
    error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 进行中任务的寻址索引（MigrateAttribute 重入 / 运维观测按集合定位）。
CREATE INDEX idx_catalog_migrations_pending
    ON catalog_migrations (project_id, database_id, collection_id, attr_key)
    WHERE phase = 'backfilling';

-- 角色授权（000026 三角色分层）：tw_owner 写任务账本（迁移创建/推进）；
-- tw_system 读任务行并执行回填数据语句（迁移期数据访问 = 运维面，BYPASSRLS
-- 身份执行——tw_owner 受 FORCE RLS 约束不可见业务行，A6 runbook 同源）。
GRANT SELECT, INSERT, UPDATE ON catalog_migrations TO tw_owner, tw_system;
