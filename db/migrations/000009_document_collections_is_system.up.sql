-- 为 document_collections 增加 is_system 标记列，并回填存量系统集合：
-- default 库中由专用服务（Users/Groups/Storage/Auth）独占管理的 7 个集合。
ALTER TABLE document_collections ADD COLUMN is_system BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE document_collections SET is_system = TRUE
WHERE database_id = 'default'
  AND id IN ('users', 'sessions', 'identities', 'groups', 'memberships', 'buckets', 'files');
