-- 移除存量项目中 keys 角色对系统敏感集合（users/sessions/identities）的
-- update/delete 权限（安全评审 C1 第 3 层 / M2）。
-- _perms 表按项目 schema 动态存在（TORCHWOOD_<internalID>_default._perms），
-- 这里通过 pg_namespace 遍历全部存量 schema 执行清理；
-- document_collections.permissions 集合级元数据同步收窄。
-- teams/memberships 的 keys 管理权限是合法语义，不在清理范围。
-- 幂等：可重复执行（无匹配行时为空操作）。
DO $$
DECLARE
    s text;
BEGIN
    FOR s IN
        SELECT nspname FROM pg_namespace WHERE nspname ~ '^TORCHWOOD_[0-9]+_default$'
    LOOP
        IF to_regclass(quote_ident(s) || '._perms') IS NOT NULL THEN
            EXECUTE format(
                'DELETE FROM %I._perms WHERE _permission = ''keys'' AND _type IN (''update'',''delete'') AND _collection IN (''users'',''sessions'',''identities'')',
                s
            );
        END IF;
    END LOOP;

    UPDATE document_collections
    SET permissions = ARRAY(SELECT x FROM unnest(permissions) AS x
                            WHERE x NOT IN ('update:keys', 'delete:keys'))
    WHERE database_id = 'default'
      AND id IN ('users', 'sessions', 'identities')
      AND permissions IS NOT NULL;
END $$;
