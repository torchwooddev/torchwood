# Torchwood 开发路线图

> 本文档基于已完成 P0 底座，规划 Torchwood 的短期、中期、长期开发方向。
> 最新更新：2026-08-10（**P1 MVP 全部完成**：Teams 团队偏好交付收尾 §2.3；M1 里程碑全量勾选，进入 P2 规划）。

---

## 0. AI / Agent-Native 战略（贯穿各阶段）

Torchwood 将 **AI/Agent-Native** 作为与 BaaS 核心能力并列的产品定位：后端不仅服务人类用户，也原生支持 LLM Agent、自动化脚本与 MCP Tool Server 以可预测、可授权的方式调用。

### 已具备（P0 / P1 部分）

| 能力 | 说明 | 关键组件 |
|------|------|----------|
| Protobuf + OpenAPI | API 单一事实来源，自动生成 Swagger | `proto/`、`buf.gen.yaml`、`genproto/**/*.swagger.json` |
| Server API + 细粒度 Scope | Agent/自动化通过 API Key 调用管理面，按 scope 限权 | `proto/server/v1/*`、`api_keys.scopes`、`pkg/grpc/interceptor` |
| 结构化鉴权注解 | 每个 gRPC 方法声明 `method_auth`，便于生成工具 schema | `proto/shared/v1/authz.proto` |
| TypeScript SDK | Client + Server API 封装，便于 Agent 工作流集成 | `sdk/typescript/`、`sdk/demo/` |
| 动态文档层 | Agent 可运行时建库/集合/文档，无需手工迁移 | `internal/infra/documentdb/`、`pkg/query/` |
| 文档级权限 | API Key 以 `keys` 角色参与 `_perms`，不默认 bypass | `internal/infra/documentdb/postgres_permissions.go` |

### 规划中

| 任务 | 说明 | 目标阶段 |
|------|------|----------|
| MCP Server | 暴露 Torchwood Server API 为 MCP Tools（Users/Databases/Storage 等） | P1 |
| OpenAPI 聚合与 Tool Schema | 合并各服务 Swagger，导出 Agent 可用的 operation 清单 | P1 |
| Agent 专用 API Key 模板 | Console 一键创建「只读 Agent」「文档读写 Agent」等预设 scope | P1 |
| Functions Tool 运行时 | Agent 触发函数作为 Tool 执行，返回结构化 JSON | P1 |
| Realtime 事件订阅 | Agent 订阅文档/用户变更，驱动多步工作流 | P2 |
| Webhooks 出站 | 业务事件推送到 Agent 编排系统（n8n、Temporal 等） | P2 |
| 多语言 SDK 生成 | 从 proto 生成 Python/Go SDK，扩大 Agent 生态 | P3 |

**验收标准（Agent-Native MVP）**：

- 仅凭 API Key + OpenAPI/Swagger，外部 Agent 框架可完成：列出用户、CRUD 文档、上传文件。
- Console 可创建带 scope 限制的 API Key，且越权调用返回明确 `PermissionDenied`。
- TypeScript SDK 演示站点可端到端验证上述流程。

---

## 1. 版本规划总览

| 阶段 | 目标 | 时间范围 | 状态 |
|------|------|----------|------|
| **P0 底座** | 可运行的工程骨架：动态文档层、Admin Console、基础认证、Storage/Functions 端口 | 已完成 | 完成 |
| **P1 MVP** | Client/Server 核心业务闭环：Account、Users、Teams、Databases Documents、Storage 交付、Functions 真实执行、Health | 短期：1-2 个月 | **完成** |
| **P2 增强** | Realtime、Webhooks/Events、Messaging、Project settings、Migrations、Tokens、Worker 框架 | 中期：3-6 个月 | 规划中 |
| **P3 生态** | Sites、Proxy、VCS、GraphQL、Avatars、Locale、Advisor、多区域/水平扩展 | 长期：6-12 个月 | 规划中 |

---

## 2. 短期（Short-term，未来 1-2 个月）

**目标**：让 Torchwood 达到可用的 MVP 状态，支持一个典型应用从注册、项目管理、数据库、文件存储到函数执行的完整闭环。

### 2.1 Client Account / Auth（最高优先级）

Client API 是终端用户直接使用的能力。Sprint 1 已补齐会话与 prefs；其余仍待实现。

| 任务 | 说明 | 关键端点 / 文件 | 状态 |
|------|------|-----------------|------|
| Refresh token | 用 refresh token 换取新的 access token | `POST /v1/account/refresh` | ✅ 完成 |
| 会话列表与删除 | 列出当前用户所有会话，可单独或全部删除 | `GET/DELETE /v1/account/sessions` | ✅ 完成 |
| 更新账号资料 | 修改 name | `PATCH /v1/account` | ✅ 完成（email/password 待扩展） |
| 账号偏好 | 读写用户级 `prefs` JSON | `GET/PATCH /v1/account/prefs` | ✅ 完成 |
| 匿名登录 | 创建无密码匿名用户 | `POST /v1/account/sessions/anonymous` | ✅ 完成 |
| Email OTP | 发送邮箱验证码 + 验证登录 | `POST /v1/account/sessions/email-otp` | ✅ 完成 |
| Phone OTP | 发送短信验证码 + 验证登录 | `POST /v1/account/sessions/phone-otp` | ✅ 完成 |
| Magic URL | 创建确认链接 + 确认登录 | `POST/PUT /v1/account/sessions/magic-url` | ✅ 完成 |
| 邮箱验证 | 发送验证邮件 + 确认 | `POST/PUT /v1/account/verification` | ✅ 完成 |
| 密码找回 | 创建找回链接 + 重置密码 | `POST/PUT /v1/account/recovery` | ✅ 完成 |
| JWT 签发 | 用当前会话换取一次性 JWT | `POST /v1/account/jwt` | ✅ 完成 |
| OAuth2（占位） | Google / GitHub 授权与回调 | `/v1/account/sessions/oauth2/*` | ✅ 完成 |
| MFA（占位） | factors 列表、TOTP 创建/验证/删除 + 登录二次验证 | `/v1/account/mfa/*` | ✅ 完成 |
| 账号日志 | 列出最近登录/操作记录 | `GET /v1/account/logs` | ✅ 完成 |

**验收标准**：

- 注册/登录后可获取 access/refresh token；refresh 成功；登出后 access token 失效且会话删除。
- 更新 email 后需重新验证；密码修改需旧密码验证。
- 所有新增端点均有 proto authz 注解、gRPC handler、use-case 单元测试。

---

### 2.2 Server Users 管理

Server API 当前支持列表/获取/更新/删除，缺少创建用户、会话/令牌管理、labels/prefs 完整映射。

| 任务 | 说明 | 关键端点 | 状态 |
|------|------|----------|------|
| 创建用户 | 服务端创建用户并指定密码哈希 | `POST /v1/server/users` | ✅ 完成 |
| 完整更新字段 | labels、status、emailVerified、prefs 支持 `google.protobuf.Struct` | `PATCH /v1/server/users/{id}` | ✅ 完成 |
| 用户会话管理 | 列出/删除指定用户的 sessions | `GET/DELETE /v1/server/users/{id}/sessions` | ✅ 完成 |
| 用户令牌管理 | 列出/删除指定用户的 tokens（与 sessions 合并：token 生命周期绑定会话） | `GET/DELETE /v1/server/users/{id}/sessions` | ✅ 完成 |
| 密码重置 | 服务端直接重置密码（撤销该用户全部会话） | `PATCH /v1/server/users/{id}/password` | ✅ 完成 |
| 模拟登录 | 生成目标用户的 JWT 用于调试 | `POST /v1/server/users/{id}/tokens` | ✅ 完成 |

**验收标准**：

- 服务端可创建用户并立即用其邮箱+密码登录成功。
- labels/prefs 的 CRUD 在数据库层面正确存储为 JSONB。
- 删除用户级联删除其 sessions/tokens。

---

### 2.3 Teams & Memberships

Sprint 1 已完成成员、邀请、角色与 Client/Console 页面；团队偏好已交付（2026-08-10，含存量集合 reconcile 自愈）。

| 任务 | 说明 | 关键端点 | 状态 |
|------|------|----------|------|
| 成员 CRUD | 列出、创建、获取、更新、删除成员 | `/v1/server/teams/{id}/memberships` | ✅ 完成 |
| 邀请流程 | 创建邀请 → 被邀请人接受/拒绝 | `POST` + `PATCH .../status` | ✅ 完成 |
| 角色体系 | owner / admin / member → JWT `team:{id}`、`member:{id}` | `PATCH .../memberships/{id}` | ✅ 完成 |
| 团队偏好 | `GET/PUT /v1/server/teams/{id}/prefs` | 扩展 `teams` 集合 | ✅ 完成 |
| Client Teams API | 当前用户创建/加入/退出团队 | `/v1/teams/*` | ✅ 完成 |
| Console Teams | 团队列表、详情、邀请与成员管理 | `/console/teams` | ✅ 完成 |

**验收标准**：

- 邀请被接受后，被邀请人拥有 `team:{teamID}` read 权限。
- owner 可删除团队；member 只能退出。
- 删除团队级联删除 memberships。

---

### 2.4 Databases Documents（核心）

Sprint 1 已完成 Server/Client Document CRUD；批量操作与 attribute/index 删除仍待实现。

| 任务 | 说明 | 关键端点 | 状态 |
|------|------|----------|------|
| Document CRUD | 创建、获取、更新、删除文档 | `/v1/server/databases/{db}/collections/{coll}/documents` | ✅ 完成 |
| Document 列表/计数 | 带 Appwrite DSL 查询、权限过滤 | `GET` / `count` | ✅ 完成 |
| Client Database API | 终端用户在授权下读写文档 | `/v1/databases/{db}/collections/{coll}/documents/*` | ✅ 完成 |
| Console 文档编辑器 | collection 下文档列表、新增/编辑/删除 | `/console/databases/.../documents` | ✅ 完成 |
| 批量操作 | 批量更新、删除、upsert | `.../documents/bulk` | ✅ 完成（Console 文档列表批量更新/删除对话框） |
| 字段自增/自减 | 对数值字段做原子增减 | `PATCH .../documents/{id}` | ✅ 完成（Document 详情页 Δ 增量输入） |
| Attribute 删除 | 删除属性并同步 `ALTER TABLE DROP COLUMN` | `DELETE .../attributes/{key}` | ✅ 完成（Schema tab 行内删除按钮） |
| Index 删除 | 删除索引 | `DELETE .../indexes/{id}` | ✅ 完成（Schema tab 行内删除按钮） |
| Collection 更新 | 修改 name / permissions | `PATCH .../collections/{coll}` | ✅ 完成（集合设置卡片：名称编辑 + 停用开关） |

**验收标准**：

- 可通过 Console 在任意 collection 中增删改查文档。
- `equal`、`greaterThan`、`contains`、`orderDesc`、`limit` 等查询组合返回正确结果。
- 普通用户只能读写自己有权限的文档；admin/key 可绕过。
- 删除 attribute 时同步清理 `document_attributes` 元数据与表结构。

---

### 2.5 Storage 文件交付

当前支持上传、下载、查看、预览、公开访问、File Token、元数据更新、Usage 统计、**分片上传（upload session）**。

| 任务 | 说明 | 关键端点 | 状态 |
|------|------|----------|------|
| 文件预览/缩略图 | 图片裁剪/缩放（使用 `disintegration/imaging`） | `GET /v1/storage/buckets/{id}/files/{id}/preview` | ✅ 完成（width/height 缩放、50MiB 上限、webp 解码 JPEG 输出） |
| 公开 bucket | bucket 级 `public` 标志，允许匿名读取 | `CreateBucket`/`UpdateBucket` 的 `public` 字段 | ✅ 完成（匿名读需 `?project=` 参数，文件级 read:any 兜底） |
| File Token | 生成短期文件访问令牌（HMAC 签名，默认 1h、上限 7d） | `POST /v1/storage/buckets/{id}/files/{id}/tokens` | ✅ 完成（下载 URL 携带 `?token=`，过期/篡改 401） |
| 文件元数据更新 | 修改 name、mime_type、metadata | `PATCH /v1/server/storage/buckets/{id}/files/{id}` | ✅ 完成（含 `UpdateBucket`：改名/公开开关） |
| 分片上传 | upload session 生命周期（create/get/chunk/complete/abort），断点续传 + 服务端合并（ComposeObject） | `POST /v1/storage/buckets/{id}/uploads`、`GET .../uploads/{uploadId}`、`POST .../uploads/{uploadId}/chunks/{partNumber}`、`POST .../uploads/{uploadId}/complete`、`DELETE .../uploads/{uploadId}` | ✅ 完成（分片 ≤16MiB、part_count ≤10000（≤156.25GB）、24h 会话 TTL、complete 互斥锁、Console 自动分片+进度+localStorage 续传；端点与早期 `files/{id}/chunks` 占位不同——会话语义见 `docs/implementation-storage-chunked-upload.md`） |
| Usage 统计 | bucket/files 数量与容量统计（`SumDocumentField` 聚合） | `GET /v1/server/storage/usage` | ✅ 完成 |

**验收标准**：

- 上传图片后可通过 `preview?width=200&height=200` 获取缩略图。
- 公开 bucket 中的文件可在无 Authorization 时通过 `view` 访问。
- File token 在过期前允许下载，过期后返回 401。

---

### 2.6 Functions 真实执行器

已交付（2026-08-09）：真实 Docker build/run、同步/异步执行、`cmd/worker` 消费队列。
「构建队列」任务以「CreateDeployment 同步构建」落地（对 roadmap 的 MVP 偏离，见
`docs/implementation-functions-executor.md` §2）。

| 任务 | 说明 | 关键端点 / 组件 | 状态 |
|------|------|-----------------|------|
| Runtime 列表 | 返回支持的运行时（node-18、python-3.11 等） | `GET /v1/server/functions/runtimes` | ✅ 完成 |
| Specification 列表 | 返回 CPU/内存规格 | `GET /v1/server/functions/specifications` | ✅ 完成 |
| Function CRUD | 创建/列表/获取/更新/删除函数 | `/v1/server/functions` | ✅ 完成 |
| Deployment CRUD | 上传代码包、列表、获取、删除 | `/v1/server/functions/{id}/deployments` | ✅ 完成 |
| Variables CRUD | 函数环境变量 | `/v1/server/functions/{id}/variables` | ✅ 完成 |
| Execution CRUD | 同步/异步执行、获取结果 | `POST/GET /v1/server/functions/{id}/executions` | ✅ 完成 |
| Docker build | 解压代码包，按运行时 Dockerfile 构建镜像（防 zip 炸弹/slip） | `internal/infra/functions/docker.go` | ✅ 完成 |
| Docker run | 运行容器，收集 stdout/stderr，超时控制与安全基线 | `internal/infra/functions/docker.go` | ✅ 完成 |
| 异步执行 Worker | `cmd/worker` 消费执行队列（BRPOP、N=4 并发、孤儿对账） | `cmd/worker/` | ✅ 完成 |
| 构建队列 | Redis List 队列 + CreateDeployment 同步构建（MVP 偏离） | `internal/domain/shared/ports.go`、`internal/infra/queue/` | ✅ 完成 |

**验收标准**：

- 上传一个 Node.js 函数后，可同步调用并返回 `console.log` 输出。
- 函数执行超时后返回 500 并清理容器。
- 异步执行可在 Console 中查看 execution 状态与日志。

---

### 2.7 Health & 可观测性

已交付（2026-08-09）：依赖探测健康检查、版本端点（ldflags 注入）、请求日志与统一 logger、慢查询 hook。

| 任务 | 说明 | 关键端点 | 状态 |
|------|------|----------|------|
| 健康检查 | DB、Redis、Storage 健康状态（并行探测 + 依赖明细 + readiness 503） | `GET /v1/health`、`/healthz/readiness` | ✅ 完成 |
| 版本端点 | 返回版本与构建信息 | `GET /v1/server/health/version` | ✅ 完成 |
| 结构化日志 | `slog` 统一 logger + gateway 请求日志（Debug 级） | 全局中间件 | ✅ 完成 |
| 慢查询日志 | 动态文档层记录慢 SQL（bun QueryHook，空值默认 500ms） | `internal/infra/clients/dbhook.go` | ✅ 完成 |

---

### 2.8 Admin Console UI

| 任务 | 说明 | 页面 | 状态 |
|------|------|------|------|
| 系统管理员管理 | 管理员 CRUD、角色与密码管理、owner 权限保护；`AdminsService`（`GET/POST/PATCH/DELETE /v1/console/admins`） | `console/src/routes/admins/`、`proto/console/v1/admins.proto` | ✅ 完成 |
| Storage 文件上传 | 在 Storage 页面直接上传文件、展示下载链接 | `console/src/routes/storage/` | ✅ 完成 |
| Databases 文档编辑器 | collection 下文档列表、新增/编辑/删除 | `console/src/routes/databases/` | ✅ 完成 |
| Attributes / Indexes 管理 | 在 collection 详情中增删属性与索引（含 Attribute/Index 行内删除） | Databases 子页面 | ✅ 完成 |
| Teams Memberships | 管理团队邀请与成员 | `console/src/routes/teams/` | ✅ 完成 |
| Functions 管理 | Functions / Deployments / Executions 页面 | 新增 `Functions.tsx` | 待办 |
| Settings 页面 | 项目基本信息编辑（`PATCH /v1/server/projects/{id}`）、OAuth Providers 配置、SMTP 只读说明 | `console/src/routes/settings/pages.tsx` | ✅ 完成 |
| 侧边栏菜单分组 | Dashboard 置顶；Develop（API Keys/Databases/Storage）、Auth（Users/Teams）、System（Projects/Admins/Settings）分组 | `console/src/components/Layout.tsx` | ✅ 完成 |

**验收标准**：

- Console 中可直接完成“创建 project → 创建 database → 创建 collection → 添加文档 → 上传文件”的闭环。
- 401 时自动跳转登录；全局错误 toast 提示。

---

### 2.9 工程化与质量

| 任务 | 说明 | 关键文件 | 状态 |
|------|------|----------|------|
| API Key scope 校验 | 拦截器解析 scopes，对端点做细粒度授权 | `pkg/grpc/interceptor/scope.go` | ✅ 完成 |
| 单元测试补齐 | 每个新增 use-case 至少一个单元测试（Functions use-case 已补） | `internal/app/**/*_test.go` | ✅ 完成 |
| 集成测试 | Account、Databases Documents、Storage 端到端测试；本次补 UpdateCollection/DeleteAttribute/DeleteIndex/increment 路径 | `internal/app/**/*_integration_test.go` | ✅ 完成 |
| Seed 数据增强 | 提供示例 collection、文件、函数 | ~~`cmd/seed/main.go`~~ | 已由首个管理员 bootstrap 取代（`docs/implementation-bootstrap-and-cli.md`），`cmd/seed` 移除 |
| GitHub Actions CI | lint（gofmt/vet/eslint）、test（含 Postgres 集成）、build、console-build | `.github/workflows/ci.yml` | ✅ 完成 |
| 代码格式化 | `gofmt`、`eslint`（prettier 未启用） | `Taskfile.yml`（`task lint`）、`console/eslint.config.js` | ✅ 完成 |

**验收标准**：

- CI 每次 PR 触发 `go test ./...` 和 `task build` 并通过。
- API Key 没有 `users.write` scope 时无法调用 `UpdateUser`。

---

## 3. 中期（Medium-term，未来 3-6 个月）

**目标**：把 MVP 打磨成可生产运行的 BaaS，补齐实时、事件、消息、项目设置、Worker 框架。

### 3.1 Realtime

| 任务 | 说明 | 关键组件 |
|------|------|----------|
| WebSocket 服务器 | Gorilla WebSocket，独立端口或复用 HTTP | `internal/api/realtime/server.go` |
| 连接认证握手 | 用 JWT 或 session cookie 鉴权 | Realtime 握手协议 |
| 频道订阅 | subscribe / unsubscribe `collections.{db}.{coll}`、`users.{id}` 等 | Channel manager |
| 事件广播 | 数据库/存储/函数变更后广播事件 | PG `LISTEN/NOTIFY` 或 Redis Pub/Sub |
| Presence | 在线状态、user/channel presence | Presence manager |
| Ping/Pong | 心跳保活 | 协议层 |

---

### 3.2 Webhooks / Events

| 任务 | 说明 | 关键组件 |
|------|------|----------|
| 事件目录 | 定义 `users.create`、`databases.documents.update`、`storage.files.create` 等 | `internal/domain/events/catalog.go` |
| Webhook CRUD | 创建/列表/获取/更新/删除 webhook | `/v1/server/webhooks` |
| 事件发布 | use-case 层在写操作后发布事件 | `internal/domain/events/publisher.go` |
| 投递 Worker | HTTP 投递、HMAC 签名、重试、失败队列 | `cmd/worker` webhook 任务 |
| 重试策略 | 指数退避、死信队列 | Queue adapter |

---

### 3.3 Messaging

| 任务 | 说明 | 关键端点 |
|------|------|----------|
| Providers | SMTP、Sendgrid、Mailgun、Twilio、FCM、APNS 等 | `/v1/server/messaging/providers` |
| Topics | 主题 CRUD | `/v1/server/messaging/topics` |
| Subscribers | 订阅者 CRUD | `/v1/server/messaging/topics/{id}/subscribers` |
| Messages | 创建邮件/SMS/Push 消息，异步发送 | `/v1/server/messaging/messages` |
| 发送 Worker | 调用 provider SDK 投递 | `cmd/worker` messaging 任务 |

---

### 3.4 Project settings

| 任务 | 说明 | 关键端点 |
|------|------|----------|
| Platforms | Web / Android / Apple / Linux / Windows 平台 CRUD | `/v1/server/projects/{id}/platforms` |
| OAuth providers | 30+ 提供商配置，优先 Google/GitHub/Apple | `/v1/server/projects/{id}/oauth2` |
| SMTP & Email templates | 邮件服务器配置与模板管理 | `/v1/server/projects/{id}/smtp`、`/email-templates` |
| Policies | 密码策略、会话策略、用户限制等 | `/v1/server/projects/{id}/policies` |
| Variables | 项目级环境变量 | `/v1/server/projects/{id}/variables` |
| Mock phone numbers | 测试手机号 | `/v1/server/projects/{id}/mock-phones` |
| DeleteProject RPC | 删除项目（repo 层已有；需级联清理动态 schema 与 admin_projects，成本高，独立排期） | `DELETE /v1/server/projects/{id}` |

---

### 3.5 高级数据库能力

| 任务 | 说明 | 关键组件 |
|------|------|----------|
| Relationships | 一对一、一对多、多对多关系 | Collection metadata 与 SQL JOIN |
| Transactions | 多文档事务接口 | `internal/infra/documentdb/transaction.go` |
| VectorsDB | pgvector 向量索引与相似度搜索 | 新增 `vector` attribute 类型 |
| 全文搜索 | 使用 `pg_trgm` 或 `to_tsvector` 优化 `search` | `buildAppwriteQuery` |
| Geo 支持 | PostGIS point/polygon | 新增 `point`、`polygon` 类型 |

---

### 3.6 Worker 框架与队列

| 任务 | 说明 | 关键组件 |
|------|------|----------|
| Queue 端口 | 抽象队列接口 | `internal/domain/shared/ports.go` |
| Redis 队列实现 | 基于 Redis List / Stream | `internal/infra/queue/redis.go` |
| Worker 二进制 | `cmd/worker` 启动消费者 | `cmd/worker/main.go`、`cmd/worker/provides.go` |
| 任务注册 | Functions、Webhooks、Messaging、Builds 等任务类型 | `internal/app/worker/registry.go` |
| 死信与重试 | 失败任务进入死信队列，可查看/重试 | Queue + DB |

---

### 3.7 安全与审计

| 任务 | 说明 | 关键组件 |
|------|------|----------|
| 速率限制 | 按 IP / user / API Key 限流 | `pkg/ratelimit` + Redis |
| 审计日志 | 记录关键管理操作 | `internal/infra/audit` |
| API Key 轮换 | 支持 secret 重新生成 | `/v1/server/api-keys/{id}/rotate` |
| 会话限制 | 最大并发会话数、异地登录提醒 | Account use-case |
| 密码字典/历史 | 密码策略落地 | Project policies |

---

## 4. 长期（Long-term，未来 6-12 个月）

**目标**：构建完整 BaaS 生态，支持多租户、站点托管、CI/CD 集成、GraphQL 与高级扩展。

### 4.1 Sites（静态/SSR 站点托管）

| 任务 | 说明 | 关键端点 |
|------|------|----------|
| Sites CRUD | 创建/列表/获取/更新/删除站点 | `/v1/server/sites` |
| Frameworks 列表 | 支持的框架模板 | `/v1/server/sites/frameworks` |
| Deployments | 上传构建产物、激活 deployment | `/v1/server/sites/{id}/deployments` |
| 静态文件托管 | 从 Storage 或专用 bucket serve | Storage adapter 扩展 |
| SSR 运行时 | 边缘/容器 SSR 执行 | Functions executor 扩展 |

---

### 4.2 Proxy（域名与路由）

| 任务 | 说明 | 关键端点 |
|------|------|----------|
| Rules CRUD | API rule、site rule、function rule、redirect rule | `/v1/server/proxy/rules` |
| 自定义域名 | CNAME 校验、TLS 证书自动申请 | Certificate worker |
| 路由分发 | 根据域名/路径分发到对应服务 | Reverse proxy layer |

---

### 4.3 VCS（Git 集成）

| 任务 | 说明 | 关键端点 |
|------|------|----------|
| GitHub OAuth | 授权、callback、installation | `/v1/server/vcs/github/authorize` |
| Repositories | 列出仓库、分支、文件内容 | `/v1/server/vcs/github/repositories` |
| 自动部署 | GitHub webhook 触发 Functions/Sites 构建 | Webhook handler |

---

### 4.4 GraphQL

| 任务 | 说明 | 关键组件 |
|------|------|----------|
| Schema 生成 | 从 collection/attribute 自动生成 GraphQL schema | `internal/api/graphql/schema.go` |
| Query/Mutation | 复用 REST 用例层 | `internal/api/graphql/resolver.go` |
| 订阅 | 基于 Realtime 的事件订阅 | GraphQL subscriptions |

---

### 4.5 周边服务

| 任务 | 说明 | 关键端点 |
|------|------|----------|
| Avatars | 头像、浏览器图标、favicon、国旗、QR 码、首字母头像 | `/v1/avatars/*` |
| Locale | 国家、货币、语言、电话代码等静态数据 | `/v1/locale/*` |
| Advisor | 项目诊断报告与建议 | `/v1/server/advisor/*` |

---

### 4.6 扩展性与平台化

| 任务 | 说明 | 关键组件 |
|------|------|----------|
| 水平扩展 | gRPC 服务多实例、负载均衡 | Deployment / Helm chart |
| 多区域存储 | S3 跨区域复制、就近读取 | Storage adapter |
| 只读副本 | 查询路由到 PostgreSQL 只读副本 | `internal/infra/clients/database.go` |
| SDK 生成 | 根据 proto 生成 Go/JS/Flutter/Python SDK | `cmd/gensdk` |
| 计费/用量 | 按 API 调用、存储、函数执行时长计费 | Usage aggregator worker |
| 高级可观测性 | OpenTelemetry、分布式追踪、告警 | `telemetry` config |

---

## 5. 里程碑与验收标准

### M1：P1 MVP 可用（短期结束）✅ 全部完成（2026-08-10）

- [x] Client Account 核心会话与 prefs（Refresh / Sessions / UpdateAccount / Prefs）。
- [x] Client Account 完整能力（密码重置、邮箱验证、OAuth、MFA、Magic URL、JWT、账号日志、匿名登录）。
- [x] Server Teams / Memberships 管理可用。
- [x] Server Users 创建与会话/令牌管理。
- [x] Databases Documents CRUD、Client API 权限可用。
- [x] Databases 批量操作、attribute/index 删除。
- [x] Storage preview、公开 bucket、file token 可用。
- [x] Functions 可上传代码、构建、同步/异步执行。
- [x] Admin Console 覆盖 Database 文档编辑、Teams 页面。
- [x] Admin Console 覆盖系统管理员管理（Admins 页面，owner 权限保护）。
- [x] Admin Console 覆盖 Functions 页面。
- [x] Admin Console 覆盖 Settings 页面（项目基本信息编辑、OAuth Providers、Messaging 只读说明）。
- [x] CI 绿，集成测试覆盖核心流程。

### M2：P2 生产就绪（中期结束）

- [ ] Realtime 可订阅数据库变更。
- [ ] Webhooks 可创建并成功投递事件。
- [ ] Messaging 可发送邮件/SMS/Push。
- [ ] Project settings（OAuth、SMTP、Policies）可用。
- [ ] Worker 框架运行 Functions / Webhooks / Messaging / Builds。
- [ ] 速率限制、审计日志、API Key 轮换上线。
- [ ] 通过负载与混沌测试。

### M3：P3 生态完整（长期结束）

- [ ] Sites / Proxy / VCS / GraphQL 上线。
- [ ] 多区域部署与水平扩展方案稳定运行。
- [ ] 官方 SDK 发布到包管理器。
- [ ] 完整的运营仪表盘与 SLA 监控。

---

## 6. 风险与依赖

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| Docker executor 安全隔离复杂 | Functions 执行可能威胁主机 | 使用 gVisor/Firecracker 或限制容器资源与网络 |
| Realtime 水平扩展 | 多实例间事件广播需要共享状态 | 优先 Redis Pub/Sub，后期评估 NATS |
| OAuth provider 数量庞大 | 30+ 提供商配置维护成本高 | 先实现 Google/GitHub/Apple，其余按需 |
| 文件预览性能 | 大图缩放消耗 CPU/内存 | 限制最大尺寸、异步生成、可选外部 CDN |
| 动态 schema 迁移 | attribute/index 变更可能影响大数据量表 | 使用 `ALTER TABLE` 时加锁评估、提供异步迁移 |
| pgvector / PostGIS 依赖 | 增加部署复杂度 | 在 Docker Compose 中预装扩展，文档说明 |

---

## 7. 参考

- `docs/archived/appwrite-go-migration-modules.md`：Appwrite 功能迁移全景（已归档）。
- `docs/archived/p0-foundation-design.md`：P0 底座设计（已归档）。
- `docs/archived/completed-tasks.md`：已完成任务清单（已归档）。
- `docs/tech-decision.md`：技术选型决策。
- `README.md`：快速开始。
- `AGENTS.md`：开发约定。
