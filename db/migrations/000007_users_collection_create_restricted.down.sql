-- 回滚：恢复 users 集合的 create:any 权限（仅当确有需要时执行）。
UPDATE document_collections
SET permissions = array_replace(permissions, 'create:keys', 'create:any')
WHERE database_id = 'default'
  AND id = 'users'
  AND 'create:keys' = ANY(permissions);
