-- J4-3（E-P2-5）并发 Subscribe 双开防护：同 (project_id, user_id, plan_id)
-- 至多一条活跃（trialing/active/past_due）订阅的 partial unique index——
-- 终态（canceled/expired）行不占位，取消/过期后可用新幂等键重新订阅。
-- 存量项目 schema 由本迁移目录的版本化重放自动补建（EnsureAll / Scoped）。
-- 状态串与 internal/domain/subscriptions/subscription.go 的状态常量一致。
CREATE UNIQUE INDEX IF NOT EXISTS subscriptions_live_unique
    ON {{schema}}.subscriptions (project_id, user_id, plan_id)
    WHERE status IN ('trialing', 'active', 'past_due');
