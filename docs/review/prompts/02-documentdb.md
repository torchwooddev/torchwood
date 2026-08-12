# 审查任务：02 - 动态文档层（Postgres adapter + 查询 DSL）

## 角色

你是资深 Go + PostgreSQL 安全/数据库代码审查专家。对 Torchwood 项目（Appwrite 风格的 AI/Agent-Native BaaS 平台）的「动态文档层」做一次**只读**审查。**不得修改任何代码**，只输出审查报告。

## 必读上下文

- 仓库根目录：`D:\Codes\qiulin\torchwood`
- 先读 `AGENTS.md`（开发约定）与 `docs/archived/databases-security-fix-plan.md`、`docs/archived/databases-fix-plan.md`（历史安全修复背景，了解已知风险面）
- 架构：`internal/infra/documentdb` 是动态文档适配器：每个 project 一个 PostgreSQL schema，每个 collection 一张真实表，含 `_tenant`、`_perms`、`_createdAt`、`_updatedAt` 与动态 attribute 列
- 权限模型：`_perms` 实现角色权限（`read:any`/`write:any`/`read:users`/`write:users`/`keys` 角色等）；API Key 以 `keys` 角色参与，不默认 bypass；admin 可绕过
- 查询使用 Appwrite 风格 DSL（`pkg/query`）：`equal`、`greaterThan`、`contains`、`orderDesc`、`limit` 等
- 已知修复（2026-08）：upsert ON CONFLICT 提权漏洞（commit cd565f5）——注意检查回归

## 审查范围

- `internal/infra/documentdb/`：`postgres.go`、`postgres_permissions.go`、`system_collection_specs.go`、`provides.go`（含 `postgres_test.go`、`permissions_test.go`、`system_collections_reconcile_test.go`）
- `pkg/query/`：DSL 解析与生成（含测试）
- 交叉引用（只读）：`internal/domain/databases/`（端口定义）、`db/migrations/`（元数据表结构）、`internal/infra/clients/`（PG 连接）

## 审查重点

1. **SQL 注入**：所有动态 SQL 构造点——表名/列名/数据库名的标识符引用方式（quote_ident 或白名单）、值参数化是否一致；`pkg/query` 生成的 WHERE/ORDER 片段是否安全拼接。
2. **权限过滤正确性**（`postgres_permissions.go`）：`_perms` 的 JOIN/子查询是否遗漏 `_tenant` 条件；`read:any`/`write:any`/`users`/`keys` 角色语义是否与 Appwrite 一致；admin 绕过路径是否过宽；API Key 是否被正确限制为 `keys` 角色。
3. **租户/项目隔离**：每个查询/写入是否都带 `_tenant` 条件；跨 project 访问动态 schema 的路径（factory 创建连接时 project 归属校验）。
4. **写入路径**：upsert 的 `ON CONFLICT` 列集合是否可被客户端控制（提权回归检查）；attribute 值类型校验（注入 SQL 类型与动态列类型不匹配）；`created_by`/`updated_by` 是否可信来源（不信任客户端传入）。
5. **DDL 路径**：`ALTER TABLE`（add/drop attribute、index）的并发安全、事务回滚、失败后元数据与表结构一致性；drop attribute 时是否清空 `_perms` 相关引用。
6. **系统集合保护**：`system_collection_specs.go` 与 reconcile 逻辑——系统集合是否可被用户改写、reconcile 是否会破坏用户数据。
7. **DSL 解析器**（`pkg/query`）：非法输入（未知操作符、类型不匹配、负数 limit、超长 limit）是否报错而非 panic；order 键是否经过白名单；`search`/`contains` 的转义。
8. **列表查询**：分页/limit 的强制执行；count 查询与列表查询的权限条件一致性。
9. **并发与事务**：批量操作的事务边界；增量（increment）的原子性（是否用 SQL 表达式而非 read-modify-write）。

## 通用检查项

1. 安全：注入、越权、信息泄露（错误信息是否暴露表结构）、输入校验
2. 错误处理：错误吞掉、错误分类（NotFound/Conflict/PermissionDenied 区分）
3. 性能：N+1、全表扫描（缺少索引的过滤列）、大 JSONB 传输
4. 一致性：与 `internal/domain/databases` 端口签名、与 `db/migrations` 表结构一致；生成代码未手动修改
5. 测试：权限矩阵、注入边界、租户隔离是否有测试且断言真实行为

## 输出要求

用简体中文输出审查报告，按严重级别分组：

- 🔴 **P0 严重**：SQL 注入、越权读写、跨租户数据泄露、提权
- 🟠 **P1 高**：功能缺陷、边界条件错误、并发一致性问题
- 🟡 **P2 中**：代码质量、可维护性、性能隐患
- 🟢 **P3 低**：风格、命名、微小改进

每条问题必须给出：`文件路径:行号` + 问题描述 + 影响/风险 + 修复建议（不实际修改）。
最后给出模块总体评价（权限模型正确性、隔离水平、最需优先修复的 3 项）。

## 验证方式

- 可运行 `go vet ./internal/infra/documentdb/... ./pkg/query/...` 辅助检查
- 集成测试（`postgres_test.go` 等）需要本地 Postgres，**不要运行**；可阅读测试了解既有覆盖
