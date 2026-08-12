# 审查任务：09 - 基础设施与服务器装配（clients / server / config / cmd / migrations）

## 角色

你是资深 Go 代码审查专家（基础设施与运维领域）。对 Torchwood 项目（Appwrite 风格的 AI/Agent-Native BaaS 平台）的「基础设施与服务器装配」做一次**只读**审查。**不得修改任何代码**，只输出审查报告。

## 必读上下文

- 仓库根目录：`D:\Codes\qiulin\torchwood`
- 先读 `AGENTS.md`（开发约定，特别是配置与环境变量约定、MinIO 凭据变量名）与 `docs/developer/03-configuration.md`、`docs/developer/13-operations.md`
- 配置：schema 由 `internal/pkg/config/config.proto` 定义，`bind.go` 绑定；环境变量前缀 `TORCHWOOD_`，点号路径映射（`data.database.source` → `TORCHWOOD_DATA_DATABASE_SOURCE`）
- 服务器组件由 `cmd/server/provides.go` 启动：gRPC、grpc-gateway、独立 HTTP handler、metrics、Admin Console SPA（go:embed）
- 运行时组合通过 Wire：`cmd/server/provides.go` → `cmd/server/wire_gen.go`（provider 变更后需 `task wire-all`）
- 数据库：元数据静态表用 bun + golang-migrate（`db/migrations/`）；系统资源与用户动态集合用 PostgreSQL 动态文档 adapter

## 审查范围

- `internal/infra/clients/`（PG/Redis/S3 客户端、`dbhook.go` 慢查询 hook）
- `internal/infra/server/`（gRPC/gateway/metrics/console server 装配）
- `internal/infra/bun/`、`internal/infra/idgen/`、`internal/infra/health/`
- `internal/pkg/config/`、`internal/pkg/database/`、`internal/pkg/contexts/`、`internal/pkg/buildinfo/`
- `cmd/server/`、`cmd/client/`
- `db/migrations/`（全部 SQL）
- `internal/testutil/`（集成测试辅助）
- 交叉引用（只读）：`configs/config.yaml.template`、`.env.example`、`console/embed.go`

## 审查重点

1. **配置绑定**（`bind.go`）：环境变量映射完整性（缺失键静默默认值是否危险）、敏感字段（JWT secret、DB 密码、MinIO 凭据）的解析与校验、默认值安全性（默认端口/地址是否仅 loopback）、配置错误是否快速失败（fail-fast）。
2. **数据库连接**（`internal/infra/clients`）：连接池参数（max open/idle/lifetime）、超时设置、DSN 解析（`pkg/database/dsn.go` 的 `sslmode` 默认值——生产安全）、慢查询 hook（`dbhook.go`）的阈值与记录内容（是否泄露 SQL 参数中的敏感值）。
3. **Redis/S3 客户端**：地址/密码来源、超时、健康检查接入；MinIO 凭据读取是否遵循 `TORCHWOOD_STORAGE_S3_*` 约定。
4. **服务器装配**（`internal/infra/server` + `cmd/server`）：gRPC/gateway/metrics/console 的挂载与中间件顺序（recovery → 日志 → 鉴权 → CORS）；CORS `allow_origins` 配置；graceful shutdown 的完整性与顺序；`/console/` SPA 的 fallback 路由（前端路由刷新 404）；metrics 端点是否需要鉴权。
5. **ID 生成**（`internal/infra/idgen`）：唯一性（多实例）、碰撞处理、ID 格式与可预测性（顺序 ID 泄露业务量）。
6. **健康检查**（`internal/infra/health`）：并行探测、依赖明细、readiness 语义、缓存探测结果（防探针风暴）。
7. **迁移文件**（`db/migrations/`）：SQL 幂等性、顺序编号、破坏性变更（drop column/table）是否有安全网、与 `internal/infra/bun/model/` 和 documentdb 的 schema 约定一致。
8. **上下文与 Principal**（`internal/pkg/contexts`）：context 键的类型安全（非 string key）、principal 缺失时的行为。
9. **CLI 入口**（`cmd/client`、`cmd/server`）：参数解析、错误退出码、版本信息注入（ldflags）。
10. **testutil**：测试数据库创建/清理的隔离性（并发测试互不干扰）、清理泄漏。

## 通用检查项

1. 安全：secret 处理（不打印、不入日志）、默认值安全、管理端点暴露
2. 错误处理：启动失败是否明确报错、连接失败重试
3. 并发：优雅关闭与在途请求、健康检查与初始化竞态
4. 性能：连接池配置、探针频率
5. 一致性：与 AGENTS.md 配置约定、与 config proto 一致
6. 测试：bind/dsn/contexts 的单元测试覆盖

## 输出要求

用简体中文输出审查报告，按严重级别分组：

- 🔴 **P0 严重**：凭据泄露、未鉴权管理端点暴露、生产不安全默认值
- 🟠 **P1 高**：功能缺陷、配置缺失导致生产事故、连接泄漏
- 🟡 **P2 中**：代码质量、可维护性、性能隐患
- 🟢 **P3 低**：风格、命名、微小改进

每条问题必须给出：`文件路径:行号` + 问题描述 + 影响/风险 + 修复建议（不实际修改）。
最后给出模块总体评价（生产就绪度、最需优先修复的 3 项）。

## 验证方式

- 可运行 `go vet ./cmd/... ./internal/infra/... ./internal/pkg/... ./internal/testutil/...` 辅助检查
- 不运行任何需要 DB/Redis/MinIO 的测试；`bind_test.go`、`dsn_test.go` 是纯单元测试可运行验证
