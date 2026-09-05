-- roles_sig 验签（A2 简化版 + R16 修订，redesign §3.2/§11-J，阶段③-b 包 C）
-- + _acl 刘改通道唯一化（A6：tw_set_document_acl）。
--
-- R16 修订（返工会话 #8-R，四件套）：
--   ① sig 消息扩展覆盖 tenant：消息从 roles|exp 扩为 tenant|roles|exp
--      （tenant = projects.internal_id 十进制串），随 app.tenant GUC 同事务
--      注入；tw_tenant() 验签函数（失败 → NULL）供 _acl 函数消费。
--   ② tw_set_document_acl 函数内两道强制校验：p_tenant = tw_tenant()
--      （跨租户/跨项目死锁，不满足 RETURN 0）+ 目标行 tw_visible 可见性
--      （堵项目内改他人 ACL 提权读；"可写即可读"保证合法路径权限面 ⊆
--      tw_visible 面，全部既有路径不受影响）。
--   ③ create/upsert 插入支的 _acl 恢复随 INSERT 携带（新行无旧行、可见性
--      校验不适用，内容治理在 app 层授予校验）；INSERT 列授权恢复 _acl
--      （UPDATE 排除保持，R13a 不回退）——本迁移不再钳制 INSERT。
--   ④ definer 调用链授权：tw_set_document_acl（definer=tw_system）内部调用
--      tw_sig_match/tw_tenant/tw_roles 按 definer 检查 EXECUTE → 显式授
--      tw_system。
--
-- 原语义承诺（000029 首版保持不变的部分）：
--   - tw_roles() 是 SECURITY DEFINER 验签函数：仅 tw_app 身份、sig 未过期
--     时返回角色；sig 缺失/格式错/过期/验签失败/密钥缺失 → 空数组 = 零角色
--     （fail-closed，与漏注入同语义）。app.roles/app.tenant GUC 可被任何持
--     SQL 会话者 set_config 伪造，验签后伪造通道封死。
--   - 密钥 = HMAC-SHA256(security.jwt.secret, 'tw-roles-guc-v1') 的 hex，
--     Go 侧进程内派生（page-token 同模式），启动钩子落 tw_secrets
--     （表不授予任何运行时角色——仅验签函数经 SECURITY DEFINER 以 owner 读）。
--     sig 格式 "<exp_unix>|<hexmac>"，窗口 60s（DB 时钟偏差容差）。
--
-- A4 修订（转出 POC 门禁，2026-09-05，原地修订——存量本地库需重建）：
--   tw_secrets 单钥 UPSERT 改双钥槽位（is_current 布尔）：新钥落 current、
--   旧 current 降级 previous（third 条直接删，previous 至多保留紧邻上一把；
--   部分唯一索引保证每 purpose 至多一把 current），tw_sig_match 改"任一钥
--   命中即通过"——滚动重启换钥期间，旧进程签发的 sig 在 60s TTL 窗口内经
--   previous 验签通过、窗口外（exp 过期）依旧拒绝，消除换钥降级窗口。
--   - tw_set_document_acl 是 _acl 删改（UPDATE/REPLACE）的唯一通道：p_table
--     经 catalog physical_name 白名单校验（防注入）；EXECUTE 仅授 tw_app、
--     REVOKE FROM public。

-- pgcrypto：验签需要 hmac()（trusted extension，DB owner 可装）。
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- 签名密钥表：Go 组合根启动期双钥平移（clients.SyncRolesSigKey）。
-- 不授予任何角色：tw_app 不可读（否则可自签），仅验签函数经
-- SECURITY DEFINER 以 owner（迁移执行者）身份读取。
-- 双钥槽位（A4）：is_current = TRUE 为 current（验签优先信任面），FALSE 为
-- previous（紧邻上一把，换钥窗口内旧 sig 的命中面）；主键 (purpose, key_hex)
-- 允许同 purpose 两行，部分唯一索引保证每 purpose 至多一把 current。
CREATE TABLE public.tw_secrets (
    purpose    TEXT NOT NULL,
    key_hex    TEXT NOT NULL,
    is_current BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (purpose, key_hex)
);
REVOKE ALL ON public.tw_secrets FROM PUBLIC;
CREATE UNIQUE INDEX tw_secrets_single_current ON public.tw_secrets (purpose) WHERE is_current;

-- tw_sig_match：sig 验签单源（消息 = tenant|roles|exp）。仅做密码学校验；
-- "仅 tw_app 身份"的限定由调用方（tw_roles/tw_tenant）承载。密钥缺失/格式
-- 错/过期/mac 不符 → false（fail-closed）。search_path 锁定 pg_catalog。
-- A4：任一钥命中即通过——current/previous 两行都进 EXISTS 面，过期判定
-- 先于钥匹配，previous 命中无法给窗口外的 sig 续命。
CREATE FUNCTION public.tw_sig_match()
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

-- tw_roles()：验签版（A2 + R16 ①）。仅 current_setting('role') = 'tw_app' 走
-- 验签（SECURITY DEFINER 只切 current_user，不切 role setting）；其余身份
-- 直接零角色。
CREATE OR REPLACE FUNCTION public.tw_roles()
RETURNS text[]
LANGUAGE sql STABLE PARALLEL SAFE
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
    SELECT CASE
        WHEN current_setting('role', true) <> 'tw_app' THEN ARRAY[]::text[]
        WHEN COALESCE(current_setting('app.roles', true), '') = '' THEN ARRAY[]::text[]
        WHEN public.tw_sig_match() THEN string_to_array(current_setting('app.roles', true), chr(31))
        ELSE ARRAY[]::text[]
    END
$$;

-- tw_tenant()：同一 sig 的租户解包（R16 ①）——验签通过返回 app.tenant 的
-- bigint，否则 NULL（供 tw_set_document_acl 强制 p_tenant 绑定）。
CREATE FUNCTION public.tw_tenant()
RETURNS bigint
LANGUAGE sql STABLE
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
    SELECT CASE
        WHEN current_setting('role', true) <> 'tw_app' THEN NULL
        WHEN current_setting('app.tenant', true) ~ '^-?[0-9]+$' AND public.tw_sig_match()
            THEN (current_setting('app.tenant', true))::bigint
        ELSE NULL
    END
$$;

-- 默认 ACL 是 EXECUTE TO PUBLIC：SECURITY DEFINER 函数必须收紧。tw_app 是
-- policy 表达式的执行身份（tw_roles）；tw_system 是 tw_set_document_acl 的
-- definer（其函数体内调用 tw_sig_match/tw_tenant/tw_roles 按 definer 检查，
-- R16 ④）。tw_tenant 同授 tw_app（只读验签探针：返回验签 tenant 或 NULL，
-- 无信息泄露——调用方本就知道自己注入的 tenant）。tw_sig_match 不直接授
-- tw_app（经 tw_roles/tw_tenant 间接可达）。
REVOKE ALL ON FUNCTION public.tw_sig_match() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.tw_tenant() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.tw_roles() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.tw_roles() TO tw_app;
GRANT EXECUTE ON FUNCTION public.tw_tenant() TO tw_app;
GRANT EXECUTE ON FUNCTION public.tw_sig_match() TO tw_system;
GRANT EXECUTE ON FUNCTION public.tw_tenant() TO tw_system;
GRANT EXECUTE ON FUNCTION public.tw_roles() TO tw_system;

-- tw_set_document_acl：_acl 删改唯一通道（A6 + R16 ②）。owner 需为
-- BYPASSRLS 角色才能绕开 FORCE RLS 的 SELECT policy 新行复检（自锁规避）——
-- 建为迁移执行者后 ALTER OWNER TO tw_system（tw_system 无 CREATE ON public，
-- 迁移显式补授；tw_system 是 NOLOGIN 的信任根角色，该授权不扩大可达面）。
GRANT CREATE ON SCHEMA public TO tw_system;
CREATE FUNCTION public.tw_set_document_acl(
    p_schema text, p_table text, p_tenant bigint, p_doc text, p_acl text[]
) RETURNS integer
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
DECLARE
    v_docsec boolean;
    v_perms jsonb;
    v_acl text[];
    v_sigtenant bigint;
    n integer;
BEGIN
    -- R16 ②-a 租户绑定：p_tenant 必须等于验签 tenant（app.tenant GUC 伪造会
    -- 使 sig 失配 → NULL → 0）。跨租户/跨项目在签名层死锁。
    v_sigtenant := public.tw_tenant();
    IF v_sigtenant IS NULL OR v_sigtenant <> p_tenant THEN
        RETURN 0;
    END IF;
    -- 白名单（防注入）：p_table 必须命中 catalog physical_name（c_<base32>
    -- 服务端分配或 sentinel 逻辑名）；顺带取 docsec/perms 供可见性判定。
    SELECT cc.document_security, cc.permissions INTO v_docsec, v_perms
    FROM public.catalog_collections cc WHERE cc.physical_name = p_table;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'unknown collection table: %', p_table USING ERRCODE = '42704';
    END IF;
    -- R16 ②-b 可见性校验：以验签 roles 跑 tw_visible（可写即可读），不可见
    -- → 0（行不存在同样落入 0——_acl 列 NOT NULL，NULL 即无行）。
    EXECUTE format('SELECT "_acl" FROM %I.%I WHERE "_id" = $1 AND "_tenant" = $2', p_schema, p_table)
    INTO v_acl USING p_doc, p_tenant;
    IF v_acl IS NULL THEN
        RETURN 0;
    END IF;
    IF NOT public.tw_visible(v_acl, public.tw_roles(), v_docsec,
            public.tw_coll_allows(v_perms, public.tw_roles(), 'read'),
            public.tw_coll_allows(v_perms, public.tw_roles(), 'update'),
            public.tw_coll_allows(v_perms, public.tw_roles(), 'delete')) THEN
        RETURN 0;
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
