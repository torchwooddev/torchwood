-- 000023 down：撤销各项目 schema 的活跃订阅 partial unique index（与 up 对称，
-- 仅遍历 tw_ 前缀 schema 且存在 subscriptions 表者，无则 no-op）。
DO $$
DECLARE
    ns TEXT;
BEGIN
    FOR ns IN
        SELECT nspname FROM pg_catalog.pg_namespace
        WHERE nspname LIKE 'tw\_%' ESCAPE '\'
    LOOP
        CONTINUE WHEN pg_catalog.to_regclass(ns || '.subscriptions') IS NULL;
        EXECUTE format('DROP INDEX IF EXISTS %I.subscriptions_live_unique', ns);
    END LOOP;
END
$$;
