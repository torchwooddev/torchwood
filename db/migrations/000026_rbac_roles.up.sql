-- RBAC 角色分层（redesign §3.2 工程纪律 / §4.3，阶段③包 B）：单一变色龙
-- authenticator（现有 DSN 用户）+ 三执行角色，事务首条 SET LOCAL ROLE 完成切换
--（PostgREST authenticator 模式，替代多连接池）。权限判定执行点（RLS policy，
-- tw_visible）在包 C 落地；本迁移只建角色、membership 与库级基础权限。
--   tw_owner  —— DDL/迁移专用（catalog DML、CREATE ON DATABASE），不跑业务查询
--   tw_app    —— 运行时（非 owner 无 BYPASSRLS）；c_* 表 DML 由建表路径授予
--   tw_system —— BYPASSRLS 内部旁路（SystemPrincipal / PlatformAdmin）
-- 依赖顺序：projectschema 0012 的 schema 级 GRANT 依赖本迁移先建出三角色
--（public 迁移先于项目 schema Apply：服务启动与 testutil 均满足）。

-- 并行安全（A6）：三角色是集群级对象，多库（go test -p N 并行建库跑迁移）可能
-- 同时到达本迁移；"IF NOT EXISTS 检查 + CREATE"非原子（检查时另一库尚未提交），
-- 用 PL/pgSQL 异常容错实现原子幂等——duplicate 视为另一库已建成就绪，语义不变。
DO $$
BEGIN
    BEGIN
        CREATE ROLE tw_owner NOLOGIN;
    EXCEPTION WHEN duplicate_object THEN NULL;
    END;
    BEGIN
        CREATE ROLE tw_app NOLOGIN;
    EXCEPTION WHEN duplicate_object THEN NULL;
    END;
    BEGIN
        CREATE ROLE tw_system NOLOGIN BYPASSRLS;
    EXCEPTION WHEN duplicate_object THEN NULL;
    END;
END
$$;

-- authenticator membership：现有 DSN 用户成员含三角色。
GRANT tw_owner, tw_app, tw_system TO CURRENT_USER;

GRANT USAGE ON SCHEMA public TO tw_app, tw_owner, tw_system;

-- tw_app（运行时热路径）：租户解析（projects）+ catalog 只读 + outbox 追加
--（SELECT 供 bun INSERT ... RETURNING 回读 default 列）。
GRANT SELECT ON TABLE projects, catalog_databases, catalog_collections TO tw_app;
GRANT INSERT, SELECT ON TABLE document_events_outbox TO tw_app;

-- tw_owner（DDL 面）：catalog 全 DML + 建库建表（CREATE ON DATABASE）。
GRANT SELECT ON TABLE projects TO tw_owner;
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE catalog_databases, catalog_collections TO tw_owner;

-- tw_system（内部旁路）：与 tw_app 同基线；c_* 表 ALL 由建表路径授予。
GRANT SELECT ON TABLE projects, catalog_databases, catalog_collections TO tw_system;
GRANT INSERT, SELECT ON TABLE document_events_outbox TO tw_system;

DO $$
BEGIN
    EXECUTE format('GRANT CREATE ON DATABASE %I TO tw_owner', current_database());
END
$$;
