-- J4-3（E-P2-5）并发 Subscribe 双开防护。订阅表运行时位于各项目数据面 schema
-- tw_<project>（projectschema 迁移器管理；public 副本已被 000022 删除），因此
-- 本迁移遍历存量 tw_ schema 立即补建 partial unique index；新建 / 重启的项目由
-- projectschema 000010_subscriptions_live_unique 覆盖。无 tw_ schema 时为 no-op。
-- 同 (project_id, user_id, plan_id) 至多一条活跃（trialing/active/past_due）订阅；
-- 终态行不占位，取消/过期后可重新订阅。
DO $$
DECLARE
    ns TEXT;
BEGIN
    FOR ns IN
        SELECT nspname FROM pg_catalog.pg_namespace
        WHERE nspname LIKE 'tw\_%' ESCAPE '\'
    LOOP
        CONTINUE WHEN pg_catalog.to_regclass(ns || '.subscriptions') IS NULL;
        EXECUTE format(
            'CREATE UNIQUE INDEX IF NOT EXISTS subscriptions_live_unique ON %I.subscriptions (project_id, user_id, plan_id) WHERE status IN (''trialing'', ''active'', ''past_due'')',
            ns);
    END LOOP;
END
$$;
