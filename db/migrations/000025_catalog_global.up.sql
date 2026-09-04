-- 全局 catalog（redesign §4.2 / C1 / G1，阶段②包 A）：catalog 定位 cluster 内
-- 全局，POC 单集群即 public。attrs/indexes/permissions 以 JSONB 列合一
--（预决策 1）：GetCollection 热路径从 3 查询收敛为 1，default_value 等全量
-- 属性契约以 catalog 为唯一源，四表模型的契约断裂类漂移从结构上消灭。
-- 每项目 catalog 四表随 projectschema 000011 退役（本迁移不搬数据，POC 测试库重建）。

CREATE TABLE catalog_databases (
    project_id  TEXT NOT NULL REFERENCES public.projects(id) ON DELETE CASCADE,
    database_id TEXT NOT NULL,
    name        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (project_id, database_id),
    UNIQUE (project_id, name)
);

CREATE TABLE catalog_collections (
    project_id        TEXT NOT NULL,
    database_id       TEXT NOT NULL,
    collection_id     TEXT NOT NULL,
    name              TEXT NOT NULL,
    -- 物理名服务端分配（c_<base32(8)>，全局唯一）——内部实现细节，不出现在
    -- 任何 API 响应；sentinel 系统集合物理名 = 逻辑名（静态表不可改名）。
    physical_name     TEXT NOT NULL,
    document_security BOOLEAN NOT NULL DEFAULT TRUE,
    disabled          BOOLEAN NOT NULL DEFAULT FALSE,
    is_system         BOOLEAN NOT NULL DEFAULT FALSE,
    -- JSONB 合一（预决策 1）：attrs 含 key/type/size/required/array/default/options
    -- 全量契约；indexes 含 id/type/attributes/orders；permissions 为 type:role 对。
    permissions       JSONB NOT NULL DEFAULT '[]'::jsonb,
    attrs             JSONB NOT NULL DEFAULT '[]'::jsonb,
    indexes           JSONB NOT NULL DEFAULT '[]'::jsonb,
    -- schema_version 仅立列，演进状态机语义挂账 redesign §4.6；
    -- ddl_seq 是元数据写路径的乐观锁（CAS 递增，redesign §4.4）。
    schema_version    BIGINT NOT NULL DEFAULT 1,
    ddl_seq           BIGINT NOT NULL DEFAULT 1,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (project_id, database_id, collection_id),
    UNIQUE (project_id, database_id, name),
    FOREIGN KEY (project_id, database_id)
        REFERENCES catalog_databases (project_id, database_id) ON DELETE CASCADE
);

-- 物理名唯一性：分配名（c_<base32>）全局唯一；sentinel 系统集合的物理名 =
-- 静态表名（tw_<p>.users，schema 内局部），跨项目必然同名，排除在约束外。
-- 部分唯一索引承载该语义（约束名即 23505 判别键）。
CREATE UNIQUE INDEX uq_catalog_collections_physical_name
    ON catalog_collections (physical_name)
    WHERE database_id <> '_';

-- ListCollections 按 (project_id, database_id) 过滤 + created_at DESC 排序。
CREATE INDEX idx_catalog_collections_db_created
    ON catalog_collections (project_id, database_id, created_at DESC);
