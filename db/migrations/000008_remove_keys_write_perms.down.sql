-- 回滚：为存量系统集合（users/sessions/identities）文档重新插入 keys 的
-- update/delete 权限行，并恢复集合级元数据（幂等，ON CONFLICT 防重复）。
DO $$
DECLARE
    s text;
    c text;
BEGIN
    FOR s IN
        SELECT nspname FROM pg_namespace WHERE nspname ~ '^TORCHWOOD_[0-9]+_default$'
    LOOP
        IF to_regclass(quote_ident(s) || '._perms') IS NOT NULL THEN
            FOREACH c IN ARRAY ARRAY['users', 'sessions', 'identities'] LOOP
                BEGIN
                    EXECUTE format(
                        'INSERT INTO %I._perms (_tenant, _collection, _document, _type, _permission) '
                        || 'SELECT _tenant, %L, _id, t.t, ''keys'' FROM %I.%I, (VALUES (''update''), (''delete'')) AS t(t) '
                        || 'ON CONFLICT DO NOTHING',
                        s, c, s, c
                    );
                EXCEPTION WHEN undefined_table THEN
                    NULL;
                END;
            END LOOP;
        END IF;
    END LOOP;

    UPDATE document_collections
    SET permissions = permissions || ARRAY['update:keys', 'delete:keys']
    WHERE database_id = 'default'
      AND id IN ('users', 'sessions', 'identities')
      AND permissions IS NOT NULL
      AND NOT permissions @> ARRAY['update:keys', 'delete:keys'];
END $$;
