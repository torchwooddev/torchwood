-- 系统表 cut：拷贝已由 Apply 在本版本 Exec 前于同一事务调用 CopySystemDocuments。
-- 幂等：文档表（有 _id）才 DROP；sys_* 在最终名不存在时 RENAME；已 cut 则 no-op。
-- 禁止在静态 users（有 id、无 _id）上 DROP TABLE users。
-- 探测用 information_schema，避免缺失表上的 ::regclass 抛错。

DO $$
DECLARE
    sch text := trim(both '"' from '{{schema}}');
    users_has_doc_id boolean;
BEGIN
    SELECT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = sch AND table_name = 'users' AND column_name = '_id'
    ) INTO users_has_doc_id;

    IF users_has_doc_id THEN
        IF EXISTS (
            SELECT 1 FROM information_schema.tables
            WHERE table_schema = sch AND table_name = '_perms'
        ) THEN
            DELETE FROM {{schema}}._perms
            WHERE _collection IN (
                'users', 'sessions', 'identities', 'groups',
                'memberships', 'buckets', 'files'
            );
            DROP TABLE {{schema}}._perms;
        END IF;
        DROP TABLE IF EXISTS {{schema}}.files;
        DROP TABLE IF EXISTS {{schema}}.memberships;
        DROP TABLE IF EXISTS {{schema}}.sessions;
        DROP TABLE IF EXISTS {{schema}}.identities;
        DROP TABLE IF EXISTS {{schema}}.buckets;
        DROP TABLE IF EXISTS {{schema}}.groups;
        DROP TABLE IF EXISTS {{schema}}.users;
    END IF;
END $$;

DO $$
DECLARE
    sch text := trim(both '"' from '{{schema}}');
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = sch AND table_name = 'sys_users'
    ) AND NOT EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = sch AND table_name = 'users'
    ) THEN
        ALTER TABLE {{schema}}.sys_users RENAME TO users;
        ALTER TABLE {{schema}}.sys_sessions RENAME TO sessions;
        ALTER TABLE {{schema}}.sys_identities RENAME TO identities;
        ALTER TABLE {{schema}}.sys_groups RENAME TO groups;
        ALTER TABLE {{schema}}.sys_memberships RENAME TO memberships;
        ALTER TABLE {{schema}}.sys_buckets RENAME TO buckets;
        ALTER TABLE {{schema}}.sys_files RENAME TO files;
    END IF;
END $$;

DO $$
DECLARE
    r record;
    sch text := trim(both '"' from '{{schema}}');
BEGIN
    FOR r IN
        SELECT i.relname AS old_name
        FROM pg_class i
        JOIN pg_namespace n ON n.oid = i.relnamespace
        WHERE n.nspname = sch
          AND i.relkind = 'i'
          AND i.relname LIKE 'sys_%'
    LOOP
        EXECUTE format('ALTER INDEX {{schema}}.%I RENAME TO %I', r.old_name, substring(r.old_name from 5));
    END LOOP;

    FOR r IN
        SELECT t.relname AS tbl, c.conname AS old_name
        FROM pg_constraint c
        JOIN pg_class t ON t.oid = c.conrelid
        JOIN pg_namespace n ON n.oid = t.relnamespace
        WHERE n.nspname = sch
          AND c.conname LIKE 'sys_%'
    LOOP
        EXECUTE format('ALTER TABLE {{schema}}.%I RENAME CONSTRAINT %I TO %I', r.tbl, r.old_name, substring(r.old_name from 5));
    END LOOP;
END $$;

DELETE FROM {{schema}}.document_indexes
WHERE database_id = '_'
  AND collection_id IN (
      'users', 'sessions', 'identities', 'groups',
      'memberships', 'buckets', 'files'
  );
DELETE FROM {{schema}}.document_attributes
WHERE database_id = '_'
  AND collection_id IN (
      'users', 'sessions', 'identities', 'groups',
      'memberships', 'buckets', 'files'
  );
DELETE FROM {{schema}}.document_collections
WHERE database_id = '_'
  AND id IN (
      'users', 'sessions', 'identities', 'groups',
      'memberships', 'buckets', 'files'
  );
DELETE FROM {{schema}}.document_databases
WHERE id = '_';
