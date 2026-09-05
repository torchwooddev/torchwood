-- 对称回滚（阶段③包 B；A6 修订：集群角色保留）：三角色 tw_owner/tw_app/tw_system
-- 是集群级对象、被同集群多个库共享（单库 up 以原子幂等方式创建），本 down 只回滚
-- "本库作用域"——对象所有权回 authenticator → 撤销三角色在本库的权限 → 撤
-- authenticator membership。不执行 DROP ROLE：角色依赖跨库不可见，多库/并行场景
-- （go test -p N 的并行测试库中 tw_owner 名下有表/函数）DROP ROLE 会撞
-- SQLSTATE 2BP01；且角色保留不影响 down→up 重放（创建幂等）。角色生命周期归
-- 集群供给方（DBA/部署）处置，与 golang-migrate 单库作用域一致（转出门禁 A8
-- 将补跨库探测与清理 runbook）。
-- 逐角色分支容错：部分角色缺失（异常手工态）时其余仍可清理；REVOKE membership
-- 同样收进分支，替代原库外单条 REVOKE（任一角色缺失即整条失败）。

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'tw_owner') THEN
        EXECUTE format('REASSIGN OWNED BY tw_owner TO %I', current_user);
        EXECUTE 'DROP OWNED BY tw_owner';
        EXECUTE 'REVOKE tw_owner FROM CURRENT_USER';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'tw_app') THEN
        EXECUTE format('REASSIGN OWNED BY tw_app TO %I', current_user);
        EXECUTE 'DROP OWNED BY tw_app';
        EXECUTE 'REVOKE tw_app FROM CURRENT_USER';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'tw_system') THEN
        EXECUTE format('REASSIGN OWNED BY tw_system TO %I', current_user);
        EXECUTE 'DROP OWNED BY tw_system';
        EXECUTE 'REVOKE tw_system FROM CURRENT_USER';
    END IF;
END
$$;
