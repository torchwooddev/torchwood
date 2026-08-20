# Torchwood 架构总览

> 面向新开发者与贡献者的架构说明文档，描述 Torchwood 的产品定位、技术栈、分层结构、目录组织、调用链与设计原则。
> 最新更新：2026-08-12

---

## 1. 产品定位

Torchwood 是一个 **Appwrite-inspired、AI/Agent-Native 的 Backend-as-a-Service（BaaS）平台**，用 Go 编写，基于 PostgreSQL，API 层采用 gRPC + grpc-gateway。

与一般 BaaS 的区别在于：Torchwood 的后端不仅服务人类用户，也**从第一天起就面向 LLM Agent、自动化脚本与 MCP Tool Server 设计**：

| 能力 | 说明 |
|------|------|
| Protobuf 单一事实来源 | API 定义在 `proto/`，`buf generate` 自动产出 gRPC stub、grpc-gateway handler 与 OpenAPI/Swagger 规范，Agent 可直接消费 |
| 细粒度 Scope 的 API Key | Agent/自动化通过 scoped API Key 调用 Server API（管理面），按 `api_keys.scopes` 限权 |
| 结构化鉴权注解 | 每个 gRPC 方法声明 `method_auth`（`proto/shared/v1/authz.proto`），便于生成工具 schema 与权限校验 |
| 可预测的 REST 表面 | JSON REST + 结构化错误，便于 LLM 工具调用与代码生成 |
| 动态文档层 | Agent 可运行时创建数据库/集合/文档，无需手工迁移 |
| 官方 SDK | `sdk/typescript`（HTTP）与 `sdk/go`（gRPC 直连）两个官方 SDK，适配 Agent 工作流 |

核心功能域：

- **项目管理**：多项目隔离；每个 `(project.id, database.id)` 对应一个 PostgreSQL schema（`tw_<projectID>_<databaseID>`）；
- **用户认证**：邮箱注册/登录、JWT access/refresh、会话 cookie、API Key 认证、Email/Phone OTP、OAuth2、匿名会话、Magic URL、一次性 JWT、TOTP MFA；
- **动态文档数据库**：schema-per-database，集合即真实表，`_tenant` 隔离项目，`_perms` 实现文档级权限；
- **文件存储**：S3/MinIO 兼容对象存储，multipart 上传/下载、分片上传/断点续传、预览缩略图、公开 bucket、HMAC File Token；
- **函数执行**：Docker 真实执行器（构建/运行）与异步 worker（`cmd/worker`），含执行历史与保留策略；
- **Admin Console**：React SPA，嵌入 Go 二进制，通过 `/console/` 提供管理界面；
- **Server API**：Projects、API Keys、Users、Groups、Storage、Databases、Collections、Attributes、Indexes、Functions、OAuth Providers 等管理面 CRUD。

---

## 2. 技术栈总览

### 后端

| 组件 | 用途 |
|------|------|
| Go 1.26.5 | 语言与运行时（`go.mod` 要求） |
| [Lynx](https://github.com/lynx-go/lynx) | 服务框架：Runner、生命周期、配置绑定、HTTP/gRPC server 基元 |
| gRPC + grpc-gateway | RPC 与 JSON REST 双表面 |
| [Wire](https://github.com/google/wire) | 编译期依赖注入，`cmd/server/provides.go` 声明 provider |
| [bun](https://github.com/uptrace/bun) | 元数据静态表 ORM（projects、api_keys、document_*、admins） |
| [cobra](https://github.com/spf13/cobra) | CLI 框架（`cmd/client`） |
| PostgreSQL 18 | 元数据 + 动态文档层（本地 docker-compose 镜像） |
| Redis 7 | 队列/上传会话/ID 生成等 |
| MinIO / S3 | 对象存储 |

### 前端

| 组件 | 用途 |
|------|------|
| React 19 + TypeScript 6 | UI 框架 |
| Vite 8 | 构建工具，dev 下代理 `/v1` 到本地 Go server |
| React Router 7 | 路由 |
| TanStack Query 5 | 服务端状态 |
| Tailwind CSS 3 + shadcn/ui 风格组件 | 样式 |
| sonner / lucide-react | 通知与图标 |

### 基础设施

- **Buf**：`buf.yaml` + `buf.gen.yaml` 驱动 proto 生成；
- **golang-migrate**：`db/migrations/` SQL 迁移；
- **Docker Compose**：`docker/local/docker-compose.yml` 本地开发基础设施；
- **Task**：`Taskfile.yml` 定义全部开发任务。

---

## 3. Clean Architecture 分层

Torchwood 采用 Clean Architecture / DDD 分层：**依赖方向始终从外向内**，传输层依赖用例层，用例层依赖领域端口，适配器实现端口。

| 层 | 目录 | 职责 | 依赖方向 |
|----|------|------|----------|
| 传输层 | `internal/api` | gRPC handler、自定义 HTTP handler：请求校验、Principal/参数映射、错误转换 | → `internal/app` |
| 用例层 | `internal/app` | use case：业务规则、事务编排、跨领域协调，不感知 gRPC/HTTP | → `internal/domain` |
| 领域层 | `internal/domain` | 领域模型与端口（`*Repo` 接口）、领域服务定义 | ← 无外部依赖 |
| 适配器层 | `internal/infra` | 端口实现：bun 仓储、documentdb 动态文档、MinIO 存储、server 组件装配 | 实现 `internal/domain` |
| 内部公共包 | `internal/pkg` | 进程内共享：config schema/绑定、database、contexts、buildinfo | 可被上四层引用 |
| 外部公共库 | `pkg/` | 可复用库：query DSL、crud、grpc 拦截器、jwtparser、password、idgen、secretbox | 可被任意包引用 |

关键点：

- **端口在 domain，实现在 infra**：例如 `internal/domain/users` 定义 `UserRepo` 接口，`internal/infra/bun/bunrepo` 提供 bun 实现，`internal/infra/documentdb` 提供动态文档实现。app 层只依赖接口，因此可以替换实现或注入 mock 做单元测试。
- **传输层不写业务**：`internal/api/*` 只做协议层工作（解析请求、组装响应），业务逻辑一律下沉到 `internal/app/*`。
- **配置 schema 用 protobuf 定义**（`internal/pkg/config/config.proto`），避免 YAML/结构体两处维护。

---

## 4. 完整目录树

```
torchwood/
├── cmd/                                # 可执行入口
│   ├── server/                         # 服务器入口（main.go + provides.go + wire.go + wire_gen.go）
│   ├── worker/                         # Worker 入口（后台任务，Wire 独立装配）
│   └── client/                         # Torchwood CLI（cobra，无 Wire：main.go + cmd/ 子包）
├── console/                            # Admin Console React SPA
│   ├── embed.go                        # go:embed dist，编译进 Go 二进制
│   ├── vite.config.ts                  # dev 代理 /v1 → localhost:9099
│   └── src/
├── configs/                            # 配置模板与本地配置
│   ├── config.yaml.template            # 配置模板（env 覆盖说明见注释）
│   └── config.yaml                     # 本地实际配置（gitignore）
├── db/migrations/                      # golang-migrate SQL 迁移
├── docker/local/                       # 本地 Docker Compose（Postgres + Redis + MinIO）
├── docs/                               # 设计文档（roadmap、tech-decision、implementation-*、archived/）
├── genproto/                           # 生成的 protobuf 代码
│   ├── client/v1/ server/v1/ console/v1/ shared/v1/
│   └── ...                             # *.pb.go / *_grpc.pb.go / *.pb.gw.go / *.swagger.json
├── internal/
│   ├── api/                            # 传输层
│   │   ├── clientgrpc/                 # Client API：Account、Databases、Groups
│   │   ├── consolegrpc/                # Console API：ConsoleAuth、Admins
│   │   ├── servergrpc/                 # Server API：Projects、APIKeys、Users、Storage、Databases、Functions、Groups、Health、OAuthProviders
│   │   └── serverhttp/                 # 自定义 HTTP handler：文件 multipart 上传下载、OAuth 回调、Functions 代码包
│   ├── app/                            # 用例层
│   │   ├── client/                     # Account 注册/登录/OTP/MFA/会话/邮箱变更确认
│   │   ├── console/                    # Console 认证（含 Setup 引导）
│   │   ├── functions/                  # Functions use case（部署/执行/变量）
│   │   ├── server/                     # Projects / API keys / users / databases / groups
│   │   ├── shared/                     # 跨用例共享逻辑（错误映射等）
│   │   └── storage/                    # 文件 / bucket 元数据 / 分片上传会话
│   ├── domain/                         # 领域层：模型 + 端口
│   │   ├── audit/                      # 审计
│   │   ├── auth/                       # 认证领域（Principal、SessionService 端口等）
│   │   ├── databases/                  # 数据库/集合/文档/属性/索引
│   │   ├── functions/
│   │   ├── idgen/                      # ID 生成端口
│   │   ├── messaging/                  # 邮件/SMS 端口
│   │   ├── projects/
│   │   ├── shared/                     # 共享领域类型（Principal、Queue 端口等）
│   │   ├── storage/
│   │   ├── groups/
│   │   └── users/
│   ├── infra/                          # 适配器层
│   │   ├── auth/                       # Principal / Validator / session cookie / TOTP
│   │   ├── bun/                        # 元数据仓储（bunrepo/）+ 模型（model/）
│   │   ├── clients/                    # PG / Redis / S3 客户端
│   │   ├── documentdb/                 # PostgreSQL 动态文档适配器（schema-per-database、_tenant、_perms）
│   │   ├── functions/                  # Docker 执行器（构建/运行）
│   │   ├── health/                     # /healthz 检查器
│   │   ├── idgen/                      # UUID/ULID/雪花/随机 ID 实现
│   │   ├── messaging/                  # SMTP/SMS 实现
│   │   ├── queue/                      # Redis List 队列
│   │   ├── server/                     # gRPC / grpc-gateway / metrics / console SPA / CORS / 错误映射
│   │   └── storage/                    # MinIO 对象存储
│   ├── pkg/                            # 进程内公共包
│   │   ├── buildinfo/                  # 构建版本信息
│   │   ├── config/                     # config.proto schema + bind.go 环境变量绑定
│   │   ├── contexts/                   # 上下文工具（Principal、AuditResource 注入）
│   │   └── database/                   # 数据库连接辅助（SourceFromEnv 等）
│   └── testutil/                       # 集成测试数据库辅助工具
├── pkg/                                # 外部公共库
│   ├── crud/                           # 列表/分页/排序抽象（AIP-132/158/160）
│   ├── grpc/interceptor/               # 认证拦截器：JWT、API Key scope、Principal 注入
│   ├── idgen/                          # ID 生成
│   ├── jwtparser/                      # JWT 签发/解析
│   ├── password/                       # 密码哈希（Argon2id）
│   ├── query/                          # Appwrite 风格查询 DSL 解析器
│   └── secretbox/                      # AES-256-GCM 加密（敏感字段保护）
├── proto/                              # protobuf 源文件（单一事实来源）
│   ├── client/v1/  server/v1/  console/v1/  shared/v1/
├── sdk/                                # 官方 SDK
│   ├── typescript/                     # TypeScript SDK（@torchwood/sdk）
│   ├── go/                             # Go SDK（sdk/go/client + sdk/go/server）
│   └── demo/                           # SDK Web 演示（端口 5174）
├── buf.yaml / buf.gen.yaml             # Buf 配置与生成规则
├── go.mod
├── Taskfile.yml                        # Task 任务定义
└── README.md / README_ZH.md
```

---

## 5. 典型调用链

以「Server API 创建用户」为例，完整链路如下：

```
HTTP 客户端 / LLM Agent
   │  POST /v1/server/users  (x-api-key / Authorization)
   ▼
grpc-gateway（internal/infra/server/grpc_gateway.go）
   │  JSON ↔ protobuf 转换、CORS、header 透传
   ▼
gRPC 服务端 + 认证拦截器（pkg/grpc/interceptor）
   │  JWT / API Key 校验、scope 检查、Principal 注入
   ▼
internal/api/servergrpc.UsersService.CreateUser        【传输层】
   │  请求校验、Principal → 上下文、调用 use case
   ▼
internal/app/server.Users 用例                         【用例层】
   │  业务规则、事务编排（创建用户、签发会话等）
   ▼
internal/domain/users.UserRepo（端口接口）              【领域层】
   │  只定义接口，不关心实现
   ▼
internal/infra/bun/bunrepo（或 documentdb 适配器）      【适配器层】
   │  bun ORM / SQL 访问元数据表
   ▼
PostgreSQL
```

对应代码路径示例（创建用户）：

| 环节 | 位置 |
|------|------|
| gRPC handler | `internal/api/servergrpc/users.go` → `CreateUser` |
| 用例 | `internal/app/server/users.go`（对应 use case） |
| 端口 | `internal/domain/users`（`UserRepo` 接口） |
| 适配器 | `internal/infra/bun/bunrepo`（元数据仓储） |
| 传输注册 | `internal/infra/server/grpc.go` + `grpc_gateway.go` |

另一个典型链路是**动态文档**：`internal/api/servergrpc/databases.go` → `internal/app/server` → `internal/domain/databases` 端口 → `internal/infra/documentdb`（schema-per-database、`_tenant` 隔离、`_perms` 权限过滤，查询使用 `pkg/query` DSL）。

---

## 6. 设计原则

1. **端口在 domain，适配器在 infra**：领域层只声明接口，实现放 infra；新增存储后端（如纯 SQL vs documentdb）不需要改动 app 层。
2. **Protobuf 是 API 单一事实来源**：`proto/` 定义 → `buf generate` 生成 `genproto/`（gRPC stub、gateway、Swagger）；不要手工编辑 `*.pb.go`。
3. **运行时 Wire 注入**：`cmd/server/provides.go`（及 `cmd/worker/provides.go`）声明全部 provider，`wire_gen.go` 由 `task wire-all` 生成；provider 变更后必须重新生成。
4. **用例层不感知协议**：app 层不 import gRPC/HTTP 相关类型，便于单测与协议演进。
5. **gRPC 方法必须带 proto authz 注解**：否则 `collectMethodsByAccess` 收集方法时会报错，无法通过注册校验。
6. **列表查询复用 `pkg/crud` 或 `pkg/query`**：不手拼 SQL filter/order；动态文档查询优先使用 Appwrite 风格 DSL。
7. **JWT claims 与 `pkg/jwtparser` 映射保持一致**；Principal 由 `pkg/grpc/interceptor` 统一注入。
8. **配置单一入口**：`internal/pkg/config/config.proto` 定义 schema，`bind.go` 负责环境变量绑定（`TORCHWOOD_` 前缀、点号路径映射）。
9. **元数据与动态文档分层存储**：静态表（projects、api_keys、document_*、admins）用 bun + migrate；用户资源与动态集合走 PostgreSQL 动态文档 adapter。

---

## 7. 三大运行入口

| 入口 | 说明 |
|------|------|
| `cmd/server` | **主服务器**。Lynx Runner 启动：gRPC（默认 `127.0.0.1:9060`）、grpc-gateway + Console SPA（`server.http.addr`）、独立 HTTP handler（multipart 上传下载、OAuth 回调）、Metrics。Wire 装配见 `provides.go` |
| `cmd/worker` | **Worker**。后台任务进程（函数异步执行队列消费者），独立 Wire 装配（`cmd/worker/provides.go`），与 server 共享 app/domain/infra 代码 |
| `cmd/client` | **Torchwood CLI**（`task build` 产出 `bin/torchwood[.exe]`）。面向 Agent / 自动化 / 运维，用 cobra 实现（不依赖 Wire），通过 API Key 调用 Server API。`health` 公开可调用、`uuid` 本地生成无需 key，其余命令需 API key；含 `health`、`uuid`、`projects`、`users`、`databases`、`groups`、`storage`、`functions`、`oauth-providers` 具名命令与覆盖全部 Server API 方法的 `rpc` 逃生舱命令 |

两个服务入口都通过 `godotenv` 加载 `.env`，并从 `./configs` 绑定配置；统一使用 `config.NewBindConfigFunc()` 完成配置绑定。

> CLI 的动态分发基于 `sdk/go/server` 的 `InvokeJSON`（按 full method name 从 `protoregistry.GlobalFiles` 查找），**新增 Server API RPC 无需在 CLI 登记**；`cmd/client/import_guard_test.go` 兜底禁止 CLI 源码直接 import genproto/grpc。使用示例见 `docs/developer/02-quickstart.md` §7。

> 首个管理员与默认 project/API Key 不再由离线脚本引导：全新数据库上打开 `/console/` 后按「初始化设置」表单注册第一个管理员（自动成为 owner，并创建默认 project 与默认 API Key），详见 `docs/implementation-bootstrap-and-cli.md` §3。注意：bootstrap 要求配置 `security.setup_token`（`TORCHWOOD_SECURITY_SETUP_TOKEN`），未配置时注册会被拒绝。
