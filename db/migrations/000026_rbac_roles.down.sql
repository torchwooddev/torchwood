-- 对称回滚（阶段③包 B）：对象所有权回 authenticator → 撤销三角色持有的
-- 权限 → 撤 membership → 删角色。REASSIGN/DROP OWNED 仅作用于当前库（角色是
-- 集群级对象，跨库残留需 DBA 处置——与 golang-migrate 单库作用域一致）。
-- 逐角色分支容错：部分角色缺失（异常手工态）时其余仍可清理。

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'tw_owner') THEN
        EXECUTE format('REASSIGN OWNED BY tw_owner TO %I', current_user);
        EXECUTE 'DROP OWNED BY tw_owner';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'tw_app') THEN
        EXECUTE format('REASSIGN OWNED BY tw_app TO %I', current_user);
        EXECUTE 'DROP OWNED BY tw_app';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'tw_system') THEN
        EXECUTE format('REASSIGN OWNED BY tw_system TO %I', current_user);
        EXECUTE 'DROP OWNED BY tw_system';
    END IF;
END
$$;

REVOKE tw_owner, tw_app, tw_system FROM CURRENT_USER;
DROP ROLE IF EXISTS tw_owner;
DROP ROLE IF EXISTS tw_app;
DROP ROLE IF EXISTS tw_system;
