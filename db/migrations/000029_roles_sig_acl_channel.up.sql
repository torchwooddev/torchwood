-- roles_sig 验签（A2 简化版，redesign §3.2/§11-J，阶段③-b 包 C）+
-- _acl 列级锁死收口（A6：变更通道唯一化为 tw_set_document_acl）。
--
-- 语义承诺：
--   1. app.roles GUC 可被任何持 SQL 会话者 set_config 伪造——tw_roles() 改为
--      SECURITY DEFINER 验签函数后，仅 tw_app 身份、且 sig = HMAC-SHA256
--      (密钥, roles||'|'||exp) 未过期时返回角色；sig 缺失/格式错/过期/验签
--      失败/密钥缺失 → 空数组 = 零角色（fail-closed，与漏注入同语义）。
--      tw_system 旁路不评估 policy、tw_owner 不跑业务查询，均不依赖本函数。
--   2. 密钥 = HMAC-SHA256(security.jwt.secret, 'tw-roles-guc-v1') 的 hex，
--      Go 侧进程内派生（page-token 同模式），启动钩子 UPSERT 进 tw_secrets
--      （表不授予任何运行时角色——仅本函数经 SECURITY DEFINER 以 owner 读）。
--      单密钥 + 滚动重启轮换（改 jwt.secret 重启即换钥）；双钥窗口挂账转出
--      POC 前。sig 格式 "<exp_unix>|<hexmac>"，窗口 60s（DB 时钟偏差容差）。
--   3. tw_set_document_acl 是 _acl 变更的唯一通道：SECURITY DEFINER owner=
--      tw_system（BYPASSRLS——绕开 UPDATE 修改 SELECT policy 引用列的新行
--      复检，语义同原 tw_system 第二语句）；p_table 经 catalog physical_name
--      白名单校验（防注入）；EXECUTE 仅授 tw_app、REVOKE FROM public。
--      create/upsert 的 INSERT 内嵌 _acl 与 update/upsert/bulk 的 tw_system
--      第二语句自此退役，INSERT 列授权同步移除 _acl（Go 侧 rls_policy.go）。

-- pgcrypto：tw_roles() 验签需要 hmac()（trusted extension，DB owner 可装）。
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- 签名密钥表：Go 组合根启动期 UPSERT（documentdb.SyncRolesSigKey）。
-- 不授予任何角色：tw_app 不可读（否则可自签），仅 tw_roles() 经
-- SECURITY DEFINER 以 owner（迁移执行者）身份读取。
CREATE TABLE public.tw_secrets (
    purpose    TEXT PRIMARY KEY,
    key_hex    TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
REVOKE ALL ON public.tw_secrets FROM PUBLIC;

-- tw_roles()：验签版（A2）。仅 current_setting('role') = 'tw_app' 走验签
--（SECURITY DEFINER 只切 current_user，不切 role setting）；其余身份直接
-- 零角色。search_path 锁定 pg_catalog（防函数体内对象解析被劫持），跨
-- schema 引用一律全限定。
CREATE OR REPLACE FUNCTION public.tw_roles()
RETURNS text[]
LANGUAGE sql STABLE PARALLEL SAFE
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
    SELECT CASE
        WHEN current_setting('role', true) <> 'tw_app' THEN ARRAY[]::text[]
        WHEN COALESCE(current_setting('app.roles', true), '') = '' THEN ARRAY[]::text[]
        ELSE COALESCE((
            SELECT CASE
                WHEN sig.exp ~ '^[0-9]+$'
                 AND (sig.exp)::bigint >= (EXTRACT(epoch FROM now()))::bigint
                 AND encode(public.hmac(
                         current_setting('app.roles', true) || '|' || sig.exp,
                         k.key_hex, 'sha256'), 'hex') = sig.mac
                THEN string_to_array(current_setting('app.roles', true), chr(31))
                ELSE ARRAY[]::text[]
            END
            FROM (SELECT split_part(current_setting('app.roles_sig', true), '|', 1) AS exp,
                         split_part(current_setting('app.roles_sig', true), '|', 2) AS mac) sig
            LEFT JOIN LATERAL (
                SELECT key_hex FROM public.tw_secrets WHERE purpose = 'tw-roles-guc-v1' LIMIT 1
            ) k ON true
        ), ARRAY[]::text[])
    END
$$;
-- 默认 ACL 是 EXECUTE TO PUBLIC：SECURITY DEFINER 函数必须收紧为仅 tw_app
--（policy 表达式以查询者身份执行）。
REVOKE ALL ON FUNCTION public.tw_roles() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.tw_roles() TO tw_app;

-- tw_set_document_acl：_acl 变更唯一通道（A6）。owner 需为 BYPASSRLS 角色
-- 才能绕开 FORCE RLS 的 SELECT policy 新行复检（自锁规避）——建为迁移执行
-- 者后 ALTER OWNER TO tw_system（tw_system 无 CREATE ON public，迁移显式
-- 补授；tw_system 是 NOLOGIN 的信任根角色，该授权不扩大可达面）。
GRANT CREATE ON SCHEMA public TO tw_system;
CREATE FUNCTION public.tw_set_document_acl(
    p_schema text, p_table text, p_tenant bigint, p_doc text, p_acl text[]
) RETURNS integer
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
DECLARE
    n integer;
BEGIN
    -- 白名单（防注入）：p_table 必须命中 catalog physical_name（c_<base32>
    -- 服务端分配或 sentinel 逻辑名）；p_schema/p_table 由 format %I 引证。
    IF NOT EXISTS (
        SELECT 1 FROM public.catalog_collections cc WHERE cc.physical_name = p_table
    ) THEN
        RAISE EXCEPTION 'unknown collection table: %', p_table USING ERRCODE = '42704';
    END IF;
    EXECUTE format(
        'UPDATE %I.%I SET "_acl" = $1 WHERE "_id" = $2 AND "_tenant" = $3',
        p_schema, p_table)
    USING p_acl, p_doc, p_tenant;
    GET DIAGNOSTICS n = ROW_COUNT;
    RETURN n;
END
$$;
ALTER FUNCTION public.tw_set_document_acl(text, text, bigint, text, text[]) OWNER TO tw_system;
REVOKE ALL ON FUNCTION public.tw_set_document_acl(text, text, bigint, text, text[]) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.tw_set_document_acl(text, text, bigint, text, text[]) TO tw_app;
