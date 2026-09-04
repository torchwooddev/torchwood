-- 000029（含 R16 修订）逆序对称回滚：恢复 tw_roles 旧形态（000027 原文，
-- PUBLIC EXECUTE 默认 ACL）、DROP tw_set_document_acl / tw_sig_match /
-- tw_tenant / tw_secrets、回收 tw_system 的 CREATE ON public、卸载
-- pgcrypto（无其他依赖对象，失败即暴露）。

DROP FUNCTION IF EXISTS public.tw_set_document_acl(text, text, bigint, text, text[]);
DROP FUNCTION IF EXISTS public.tw_tenant();
DROP FUNCTION IF EXISTS public.tw_sig_match();
REVOKE CREATE ON SCHEMA public FROM tw_system;

CREATE OR REPLACE FUNCTION public.tw_roles()
RETURNS text[]
LANGUAGE sql STABLE PARALLEL SAFE
AS $$
    SELECT string_to_array(NULLIF(current_setting('app.roles', true), ''), chr(31))
$$;
GRANT EXECUTE ON FUNCTION public.tw_roles() TO PUBLIC;

DROP TABLE IF EXISTS public.tw_secrets;
DROP EXTENSION IF EXISTS pgcrypto;
