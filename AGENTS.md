# AGENTS 指南

## 总体说明
- 本仓库使用 Lynx + Clean Architecture：`internal/api`（传输层）、`internal/app`（用例层）、`internal/domain`（领域与端口）、`internal/infra`（适配器层）。
- Torchwood 产品定位包含 **AI/Agent-Native**：Protobuf + OpenAPI 定义可机器读取的 API；Server API 通过 scoped API Key 供 Agent/自动化调用；详见 `docs/roadmap.md` §0 与 `sdk/README.md`。
- 运行时组合通过 Wire 注入：`cmd/server/provides.go` -> `cmd/server/wire_gen.go`。
- 服务器组件由 `cmd/server/provides.go` 启动，包含 gRPC、grpc-gateway、独立 HTTP handler、metrics、Admin Console SPA。
- gRPC/API Proto 定义在 `proto/client`、`proto/server`、`proto/console`、`proto/shared`，生成代码位于 `genproto/`。
- 典型调用链：gRPC handler -> app use-case -> domain repo port -> infra adapter（bun 或 documentdb）。
- 认证中间件位于 `pkg/grpc/interceptor` 中，使用 Principal 注入；API_KEY 方法同时允许 admin console session（需带 `X-Torchwood-Project` header）。

## 项目结构补充
- `console/`：React + Vite + TanStack Query + shadcn/ui 管理后台前端，通过 `console/embed.go` 嵌入 Go 二进制。
- `cmd/client/`：Torchwood CLI 二进制（`bin/torchwood`），cobra 实现，通过 sdk/go（server 包 InvokeJSON）以 API Key 调用 Server API；CLI 源码不直接 import genproto/grpc（有 import_guard_test 兜底），方法覆盖完整性由 `sdk/go/server` 的测试保证，新增 RPC 无需在 CLI 登记。
- `internal/api/serverhttp/`：自定义 HTTP handler，例如 Storage multipart 上传下载。
- `pkg/query/`：Appwrite 风格查询 DSL 解析器，供动态文档层使用。
- `internal/testutil/`：集成测试数据库辅助工具。

## 开发流程
- 以 Task 作为主要工作流执行器（`Taskfile.yml`）。常用任务：
  - 基础：`task install-tools`、`task up`、`task down`、`task clean`、`task migrate`
  - 生成：`task generate-proto`、`task generate-config`、`task wire-all`、`task generate-all`
  - 前端：`task console-install`、`task console-build`、`task console-dev`
  - 开发：`task dev-server`
  - 质量与构建：`task test`、`task build`
- Proto 生成由 Buf 驱动（`buf.yaml`、`buf.gen.yaml`），输出到 `genproto/`；不要手工编辑生成的 `*.pb.go` 文件。
- 依赖注入由 Wire 驱动；provider 变更后请执行 `task wire-all`。
- 配置 proto 生成由 `task generate-config` 负责；API protobuf 生成由 `task generate-proto` 负责。
- 本地基础设施来自 `docker/local/docker-compose.yml`（Postgres + Redis + MinIO）。
- 修改 Console 代码后需先 `task console-build` 再 `task build`，否则 Go embed 会打包旧版本。

## 配置与环境约定
- 配置 schema 由 `internal/pkg/config/config.proto` 定义，运行时绑定位于 `internal/pkg/config/bind.go`。
- 环境变量覆盖前缀为 `TORCHWOOD_`；键名会从点号路径映射而来，例如 `data.database.source` -> `TORCHWOOD_DATA_DATABASE_SOURCE`。
- `TORCHWOOD_ENV`（development/production）决定关停排水窗口：development 为 0，production 默认 30s；可被 `TORCHWOOD_SERVER_DRAIN_TIMEOUT` 覆盖。Lynx 在绑定 YAML 之前就需要该值，因此不进 `config.proto`。
- MinIO 凭据请使用 `TORCHWOOD_STORAGE_S3_ACCESS_KEY_ID` 和 `TORCHWOOD_STORAGE_S3_SECRET_ACCESS_KEY`。
- `cmd/server/main.go` 会通过 `godotenv` 尝试加载 `.env`，然后默认从 `./configs` 绑定配置。
- 请使用 `configs/config.yaml.template` 作为基础模板，并将敏感信息保持在环境变量中。
- 反向代理后需恢复客户端真实 IP 时设置 `security.trusted_proxies`（如 `TORCHWOOD_SECURITY_TRUSTED_PROXIES=127.0.0.1/32`）；默认不信任 X-Forwarded-For/X-Real-Ip，一律使用 gRPC peer 地址。
- Console admin 会话走 `TORCHWOOD_session_console` HttpOnly cookie（SameSite=Lax，refresh cookie 限 `/v1/console/auth` 路径），前端不再用 localStorage 存 token。

## 数据库约定
- 三层物理模型（见 `docs/design/project-data-plane-schema.md`）：`public` 控制面+事件脊柱（projects、admins、admin_projects、api_keys、audit_logs、provider_resource_index、outbox、document_transactions）用 bun + golang-migrate；项目数据面 `tw_<project.id>` 容纳系统文档集合、项目账本 / Functions / OAuth / 文档目录（bun，go:embed 项目迁移 `internal/infra/projectschema/`）；业务文档面 `tw_<project.id>_<database.id>` 只放用户 collection。
- 系统集合以 sentinel `_`（`ident.ProjectDataPlaneID`）寻址，物理表在 `tw_<project>`；`default` 是普通第一库（可删可重建）。DDL 只走两段式 `businessSchema`，永不解析一段式。`project.id` / `database.id` 规则见 `docs/developer/06-databases.md`。
- 动态文档查询使用 Appwrite 风格 DSL（`pkg/query`），支持 `equal`、`greaterThan`、`contains`、`orderDesc`、`limit` 等。

## 编辑时应遵循的模式
- 保持端口在 domain、适配器在 infra。
- gRPC 方法必须带 proto authz 注解，否则 `collectMethodsByAccess` 会报错。
- **Proto 规范**：删除字段一律 `reserved`（字段号 + 字段名），禁止复用字段号；
  更新类请求的可选字段用 `proto3 optional` 表达 presence（未设置=不修改）；
  时间字段一律 `google.protobuf.Timestamp`（HTTP JSON 为 RFC3339）；
  OpenAPI 建模约定见 `docs/developer/09-api-guide.md` §1.4（swagger 扩展与
  `method_auth` 一致性由 `internal/infra/server/grpc_swagger_test.go` 断言）。
- 列表查询复用 `pkg/crud` 或等价的 AIP-132/158/160 抽象，不要手拼 SQL filter/order；动态文档优先使用 `pkg/query`。
- JWT claims 保持与 `pkg/jwtparser` 的映射兼容。
- Console 前端组件放在 `console/src/components/ui/`，样式基于 Tailwind + shadcn/ui。

## 特别约定
- 对话和文档优先使用简体中文。
- 管理后台通过 `/console/` 路径访问，由 Go server 嵌入并 serve。
