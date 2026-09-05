-- roles_sig 双钥轮换窗口（转出 POC 门禁 A4，docs/developer/15-exit-poc.md；
-- redesign §3.2 GUC 伪造面挂账"单密钥 + 滚动重启轮换，双钥窗口转出 POC 前"）。
--
-- 载体选择：000031 前向补丁而非 000029 原地修订——遵 A5 迁移方案
--（docs/design/poc-to-release-migration.md §6）确立的规则方向："已应用的
-- 迁移文件永不原地修订；语义修订一律新版本号前向补丁（POC 期重建兜底的
-- 只是数据，不是迁移历史）"。已应用 000029 的存量库经 db:migrate 原地升级，
-- 无需重建；本迁移的 CREATE OR REPLACE 同时把停留在 000029 首版（R16 前
-- 旧消息 roles|exp）函数面的存量库矫正为当前验签形态。
--
-- 语义：tw_secrets 单钥（purpose 主键，覆盖式 UPSERT 换钥）→ 双钥槽位
--（is_current 布尔 + (purpose, key_hex) 主键）。is_current = TRUE 为
-- current（Go 进程派生钥的落位，签名信任面），FALSE 为 previous（紧邻
-- 上一把，换钥窗口内旧 sig 的命中面）。tw_sig_match 改"任一钥命中即通过"
-- ——滚动重启换钥期间，旧进程签发的 sig 在 60s TTL 窗口内经 previous
-- 验签通过、窗口外（exp 过期）依旧拒绝，消除换钥降级窗口。R16 语义
--（tenant|roles|exp 消息、tenant 绑定、可见性门）与 60s 窗口语义不变；
-- tw_roles()/tw_tenant() 走同一 match，无需改动。
-- Go 侧平移逻辑见 clients.SyncRolesSigKey（current→previous 平移 +
-- third 条直接删，行数不变量 ≤2；同钥重启幂等）。

-- 表：单钥 → 双钥槽位。部分唯一索引保证每 purpose 至多一把 current
--（Go 侧平移逻辑的表级兜底约束）。
ALTER TABLE public.tw_secrets
    DROP CONSTRAINT tw_secrets_pkey,
    ADD COLUMN is_current BOOLEAN NOT NULL DEFAULT TRUE,
    ADD PRIMARY KEY (purpose, key_hex);
CREATE UNIQUE INDEX tw_secrets_single_current ON public.tw_secrets (purpose) WHERE is_current;

-- tw_sig_match：sig 验签单源（消息 = tenant|roles|exp）。任一钥命中即通过
-- ——current/previous 两行都进 EXISTS 面；过期判定先于钥匹配，previous
-- 命中无法给窗口外的 sig 续命。仅做密码学校验；"仅 tw_app 身份"的限定由
-- 调用方（tw_roles/tw_tenant）承载。密钥缺失/格式错/过期/mac 不符 → false
--（fail-closed）。search_path 锁定 pg_catalog。CREATE OR REPLACE 保留
-- 000029 的 ACL（tw_system EXECUTE）。
CREATE OR REPLACE FUNCTION public.tw_sig_match()
RETURNS boolean
LANGUAGE sql STABLE
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
    SELECT COALESCE((
        SELECT sig.exp ~ '^[0-9]+$'
           AND (sig.exp)::bigint >= (EXTRACT(epoch FROM now()))::bigint
           AND EXISTS (
               SELECT 1 FROM public.tw_secrets k
               WHERE k.purpose = 'tw-roles-guc-v1'
                 AND encode(public.hmac(
                       COALESCE(current_setting('app.tenant', true), '') || '|' ||
                       current_setting('app.roles', true) || '|' || sig.exp,
                       k.key_hex, 'sha256'), 'hex') = sig.mac
           )
        FROM (SELECT split_part(current_setting('app.roles_sig', true), '|', 1) AS exp,
                     split_part(current_setting('app.roles_sig', true), '|', 2) AS mac) sig
    ), false)
$$;
