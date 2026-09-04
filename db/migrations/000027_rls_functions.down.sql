-- 对称回滚（阶段③包 C）：判定函数随 policy 体系退役。
-- 各集合表的 policy/RLS 由 DeleteCollection 的 DROP TABLE 与 reconcile 路径
-- 自行清理；本迁移只撤函数。
DROP FUNCTION IF EXISTS public.tw_visible(text[], text[], boolean, boolean, boolean, boolean);
DROP FUNCTION IF EXISTS public.tw_coll_allows(jsonb, text[], text);
DROP FUNCTION IF EXISTS public.tw_can(text[], text[], text, boolean);
DROP FUNCTION IF EXISTS public.tw_roles();
