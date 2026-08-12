# Torchwood

[English](README.md) | **简体中文**

Torchwood 是一个受 Appwrite 启发、**AI/Agent-Native** 的后端即服务（BaaS）平台，使用 Go + PostgreSQL + gRPC/grpc-gateway 构建，提供用户认证、动态文档数据库、文件存储、函数执行、Admin Console 等核心能力 —— API 与工具链从设计之初即面向 LLM Agent、自动化脚本与 MCP Tool Server。

## 功能特性

- **AI / Agent-Native**：Protobuf 定义 API，自动生成 OpenAPI/Swagger；细粒度 scope 的 API Key 供 Agent 与自动化调用 Server API；统一 JSON REST 面与结构化错误；官方 TypeScript 与 Go SDK 便于 Agent 工作流与 Tool 集成。
- **项目管理**：多项目隔离，每个项目拥有独立的数据库 schema。
- **用户认证**：邮箱注册/登录、JWT access/refresh token（含轮换）、会话 Cookie、Email/Phone OTP、OAuth2（Google/GitHub/WeChat）、匿名会话、Magic URL、一次性 JWT、TOTP MFA 登录挑战、邮箱变更两阶段确认。
- **动态文档数据库**：schema-per-database，支持 `_tenant`、`_perms`、动态属性/索引，查询语言兼容 Appwrite 风格 DSL，支持批量操作与字段增量。
- **文件存储**：S3/MinIO 兼容的对象存储，支持上传/下载/在线查看、预览缩略图、公开 bucket、HMAC File Token、分片上传与断点续传。
- **函数执行**：基于 Docker 的真实执行器（构建/运行，含安全基线），支持同步/异步执行、异步 worker（`cmd/worker`）、执行历史与保留策略。
- **Admin Console**：React + Vite + TanStack Query + shadcn/ui 管理后台，嵌入 Go 二进制，路径 `/console/`。
- **Server API**：Project / API Key / User / Team / Storage / Database / Collection / Attribute / Index / Function / OAuth Provider 的 CRUD；健康与版本端点。
- **可观测性**：依赖健康检查、版本端点、结构化 slog 日志、慢查询日志、Prometheus metrics。

## 技术栈

### 后端

- Go 1.26.5
- [Lynx](https://github.com/lynx-go/lynx) 服务框架
- gRPC + grpc-gateway
- [Wire](https://github.com/google/wire) 依赖注入
- [bun](https://github.com/uptrace/bun) ORM（元数据表）
- PostgreSQL（动态文档层）
- Redis
- MinIO / S3（对象存储）

### 前端

- React 19 + TypeScript 6
- Vite 8
- React Router 7
- TanStack Query 5
- Tailwind CSS 3 + shadcn/ui 风格组件
- sonner（toast）
- lucide-react

## 快速开始

### 前置要求

- Go 1.26.5+
- Node.js 22+ + pnpm
- Docker + Docker Compose
- [Task](https://taskfile.dev/)（`go install github.com/go-task/task/v3/cmd/task@latest`）

### 1. 启动本地基础设施

```bash
task up
```

这会启动 PostgreSQL（5432）、Redis（6379）和 MinIO（9000/9001）。端口可通过 `.env` 中的 `POSTGRES_PORT`、`REDIS_PORT`、`MINIO_API_PORT`、`MINIO_CONSOLE_PORT` 变量调整。

### 2. 配置环境变量

复制模板并填写必要信息：

```bash
cp .env.example .env
```

关键变量：

```env
TORCHWOOD_DATA_DATABASE_SOURCE=postgres://torchwood:torchwood@127.0.0.1:5432/torchwood?sslmode=disable
TORCHWOOD_DATA_REDIS_PASSWORD=            # Redis 地址走 configs/config.yaml 的 data.redis.addr
TORCHWOOD_SECURITY_JWT_SECRET=dev-only-0123456789abcdef-0123456789abcdef
TORCHWOOD_SECURITY_SETUP_TOKEN=dev-setup-0123456789abcdef0123456789abcdef
TORCHWOOD_STORAGE_S3_ENDPOINT=http://127.0.0.1:9000
TORCHWOOD_STORAGE_S3_ACCESS_KEY_ID=minioadmin
TORCHWOOD_STORAGE_S3_SECRET_ACCESS_KEY=minioadmin
```

> `TORCHWOOD_SECURITY_JWT_SECRET` 至少 32 字符，且不得包含已知弱子串（`change-me`、`secret`、`password`、`torchwood`、`minioadmin` 等），否则服务拒绝启动。`TORCHWOOD_SECURITY_SETUP_TOKEN` 是首次引导的门禁：未设置时注册第一个管理员会被拒绝。生产环境请生成强随机值（如 `openssl rand -hex 32`）。

### 3. 运行数据库迁移

```bash
task migrate
```

### 4. 安装依赖

```bash
# 安装工具（首次）
task install-tools

# 安装 Console 依赖
task console-install

# 生成 protobuf、wire 等
task generate-all
```

### 5. 构建并运行

```bash
task build      # 会先执行 console-build，再编译 server、worker 与 CLI 到 ./bin/
./bin/server.exe
```

或直接开发模式：

```bash
task dev-server
```

### 6. 首次部署引导（bootstrap）

全新数据库上启动后，打开 Admin Console `http://127.0.0.1:9080/console/`，
登录页会自动切换为「初始化设置」表单。使用你配置的 `TORCHWOOD_SECURITY_SETUP_TOKEN`
注册第一个管理员将自动：

- 创建 owner 管理员账户（首个管理员固定为 `owner`）；
- 创建默认项目（id=`default`）与默认 API Key（scope=`all`）；
- 页面展示一次默认 API Key secret（请立即复制，此后无法再读取）。

使用该 secret 以 `x-api-key` metadata 调用 Server API 即可。

访问（默认取自 `configs/config.yaml.template`，并非硬编码）：

- Admin Console：`http://127.0.0.1:9080/console/`
- HTTP/gRPC-gateway API：`http://127.0.0.1:9080/v1/...`
- gRPC（仅回环）：`127.0.0.1:9060`
- Metrics：`http://127.0.0.1:9040/metrics`
- 健康检查：`http://127.0.0.1:9080/healthz/liveness`、`/healthz/readiness`

> 注意：`console/vite.config.ts` 与配置模板的 CORS `allow_origins` 仍引用旧端口 9099，使用 `task console-dev` 时需同步调整（或修改本地 `configs/config.yaml`）。

## 常用开发任务

```bash
# 基础设施
task up          # docker compose up
task down        # docker compose down
task migrate     # 运行数据库迁移

# 代码生成
task generate-proto    # buf generate
task generate-config   # 生成 Go config
task wire-all          # 重新生成 Wire
task generate-all      # 以上全部

# 前端
task console-install   # pnpm install
task console-build     # pnpm run build
task console-dev       # pnpm run dev

# 后端
task dev-server        # go run ./cmd/server
task test              # SDK Go/TS 测试 + go test -v ./... -cover
task build             # 构建 server、worker 与 CLI 二进制（含 console）
```

## 项目结构

```
.
├── cmd/
│   ├── server/            # 服务入口与 Wire 组装
│   ├── worker/            # 异步 worker（函数执行队列消费者）
│   └── client/            # Torchwood CLI（cobra；main.go + cmd/）
├── console/               # Admin Console React SPA
│   ├── embed.go           # go:embed dist
│   └── src/
├── configs/               # 配置文件模板
├── db/migrations/         # golang-migrate SQL 迁移
├── docker/local/          # 本地 Docker Compose
├── docs/                  # 设计文档
├── genproto/              # 生成的 protobuf 代码
├── internal/
│   ├── api/               # 传输层：gRPC handler + 自定义 HTTP handler
│   │   ├── clientgrpc/    # Client API（Account / Databases / Teams）
│   │   ├── consolegrpc/   # Console API（ConsoleAuth / Admins）
│   │   ├── servergrpc/    # Server API（Projects / APIKeys / Users / Databases / ...）
│   │   └── serverhttp/    # 自定义 HTTP：文件 multipart 上传、OAuth 回调、函数代码包
│   ├── app/               # 用例层（client / console / functions / server / shared / storage）
│   ├── domain/            # 领域模型与端口（audit / auth / databases / functions / idgen / messaging / projects / shared / storage / teams / users）
│   ├── infra/             # 适配器层（auth / bun / clients / documentdb / functions / health / idgen / messaging / queue / server / storage）
│   ├── pkg/               # 进程内共享包（buildinfo / config / contexts / database）
│   └── testutil/          # 集成测试工具
├── pkg/                   # 可复用库（crud / grpc / idgen / jwtparser / password / query / secretbox）
├── proto/                 # protobuf 源文件
├── sdk/                   # 官方 SDK：typescript/ + go/ + demo/
├── buf.yaml / buf.gen.yaml
├── go.mod
├── Taskfile.yml
└── README.md
```

## 架构说明

- **Clean Architecture / DDD**：domain 定义端口，infra 提供实现，app 编排用例，api 负责传输。
- **AI / Agent-Native API 设计**：Protobuf 为单一事实来源；`buf generate` 产出 gRPC stub、grpc-gateway handler 及 `genproto/` 下的 OpenAPI 规范。**Server API**（`/v1/server/*`）面向程序化与 Agent 访问，通过 API Key 鉴权；**Client API**（`/v1/account/*`、`/v1/databases/*` 等）服务终端用户流程。详见 [`sdk/README.md`](sdk/README.md)（官方 TypeScript 与 Go SDK）。
- **动态文档数据库**：每个 database 对应一个 PostgreSQL schema；集合是真实表；`_tenant` 用于项目隔离；`_perms` 表实现基于角色的文档权限。
- **认证**：支持 end-user JWT、session Cookie、API Key、console admin JWT。API Key 不绕过 `_perms`，以 `keys` 角色参与权限检查；admin 可带 `X-Torchwood-Project` header 操作指定项目。
- **REST API**：gRPC 方法通过 grpc-gateway 暴露为 JSON REST；文件上传/下载使用自定义 HTTP handler。
- **Console**：React SPA 通过 `//go:embed dist` 打包进 Go 二进制，由 `/console/` 路径 serve。

## 测试

```bash
# 单元/集成测试（需要本地 Postgres）
task test
```

`task test` 依次执行 Go SDK 测试（`sdk/go`）、TypeScript SDK 测试套件（`sdk/typescript`），再对整个仓库运行 `go test -v ./... -cover`。

集成测试位于：

- `internal/infra/documentdb/postgres_test.go`
- `internal/app/client/account_test.go`

测试会自动创建并销毁 `TORCHWOOD_test` 数据库。

测试数据库 DSN 从 `TORCHWOOD_TEST_DATABASE_SOURCE` 与 `TORCHWOOD_TEST_ADMIN_DATABASE_SOURCE` 环境变量读取（已包含在 `.env.example` 中）；`task test` 会自动从 `.env` 加载。直接运行 `go test` 时需先导出：

```bash
# go test ./...               # 未设置变量时快速失败
task test                     # 加载 .env 后运行全部测试
```

## SDK

详见 [`sdk/README.md`](sdk/README.md)，仓库提供两个官方 SDK：

- **TypeScript SDK**（`sdk/typescript`，包名 `@torchwood/sdk`）—— 基于 HTTP（grpc-gateway）封装 Client API 与 Server API，附 Web 演示。
- **Go SDK**（`sdk/go`，模块 `github.com/torchwooddev/torchwood/sdk/go`）—— gRPC 直连薄封装：`client`（终端用户认证，自动刷新 token）与 `server`（API Key 认证，含 `InvokeJSON` 动态分发）。CLI（`cmd/client`）即构建于 Go SDK 的 `server` 包之上。

```bash
task sdk-install
task sdk-build
task sdk-demo   # 演示站点 http://localhost:5174
```

## 开发者文档

完整的开发者文档（架构、配置、认证授权、数据库、存储、函数、API 开发指南、Console、测试、SDK、部署运维）位于 [`docs/developer/`](docs/developer/README.md)。

## 设计文档

- `docs/roadmap.md`：开发路线图（含 AI/Agent-Native 战略）
- `docs/tech-decision.md`：技术栈决策
- `docs/developer/`：开发者文档（见上方索引）
- `docs/archived/`：已归档的历史设计文档（P0 设计、迁移清单、安全评审、修复方案）

## 许可证

MIT（待定）
