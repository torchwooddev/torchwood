-- pgvector 扩展（会话 #10 §10.5 P0 最后一项：向量近邻查询）。
-- vector 属性类型落地为 pgvector 原生列 VECTOR(dims)，HNSW 索引与
-- vector_search 一等算子依赖本扩展。镜像侧由 pgvector/pgvector:0.8.6-pg18
-- 预装（docker/local + CI service container 同步）；此处按库启用。
-- 注意（原型 1 实证）：vector 不是 trusted extension，CREATE EXTENSION
-- 需 superuser——当前部署形态的迁移执行身份（compose/CI 的 POSTGRES_USER、
-- testutil admin source）均为 superuser，成立；转出 POC 前的部署方案需
-- 把"扩展安装走 superuser 引导步骤"写进 runbook。
CREATE EXTENSION IF NOT EXISTS vector;
