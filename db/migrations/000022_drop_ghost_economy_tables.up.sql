-- B5：删除 public 幽灵经济表（运行时已迁至 tw_<project>，public 副本无读路径）。
-- 覆盖 000013–000017 在 public 创建的全部经济域表；保留 provider_resource_index、outbox 等系统表。
-- 顺序：先子表后主表，CASCADE 兜底 FK。

DROP TABLE IF EXISTS public.asset_ledger_entries CASCADE;
DROP TABLE IF EXISTS public.asset_holdings CASCADE;
DROP TABLE IF EXISTS public.asset_defs CASCADE;
DROP TABLE IF EXISTS public.payment_fulfillments CASCADE;
DROP TABLE IF EXISTS public.payment_callback_events CASCADE;
DROP TABLE IF EXISTS public.payment_orders CASCADE;
DROP TABLE IF EXISTS public.billing_statements CASCADE;
DROP TABLE IF EXISTS public.usage_rollups CASCADE;
DROP TABLE IF EXISTS public.subscriptions CASCADE;
DROP TABLE IF EXISTS public.subscription_plans CASCADE;
