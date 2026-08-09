# Torchwood

[English](README.md) | **简体中文**

Torchwood 是一个受 Appwrite 启发、**AI/Agent-Native** 的后端即服务（BaaS）平台，使用 Go + PostgreSQL + gRPC/grpc-gateway 构建，提供用户认证、动态文档数据库、文件存储、函数执行、Admin Console 等核心能力 —— API 与工具链从设计之初即面向 LLM Agent、自动化脚本与 MCP Tool Server。

## 功能特性

- **AI / Agent-Native**：Protobuf 定义 API，自动生成 OpenAPI/Swagger；细粒度 scope 的 API Key 供 Agent 与自动化调用 Server API；统一 JSON REST 面与结构化错误；TypeScript SDK 便于 Agent 工作流与 Tool 集成。
- **项目管理**：多项目隔离，每个项目拥有独立的数据库 schema。
- **用户认证**：邮箱注册/登录、JWT access/refresh token、会话 Cookie、API Key 认证。
- **动态文档数据库**：schema-per-database，支持 `_tenant`、`_perms`、动态属性/索引，查询语言兼容 Appwrite 风格 DSL。
- **文件存储**：S3/MinIO 兼容的对象存储，支持 multipart 上传/下载，文件元数据以动态文档管理。
- **函数执行**：Docker 执行器端口与 P0 stub。
- **Admin Console**：React + Vite + TanStack Query + shadcn/ui 管理后台，嵌入 Go 二进制，路径 `/console/`。
- **Server API**：Project / API Key / User / Storage / Database / Collection / Attribute / Index 的 CRUD。

## 技术栈

### 后端

- Go 1.25
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

- Go 1.25+
- Node.js 22+ + npm
- Docker + Docker Compose
- [Task](https://taskfile.dev/)（`go install github.com/go-task/task/v3/cmd/task@latest`）

### 1. 启动本地基础设施

```bash
task up
```

这会启动 PostgreSQL（5433）、Redis（6380）和 MinIO（9000/9001）。

### 2. 配置环境变量

复制模板并填写必要信息：

```bash
cp .env.example .env
```

关键变量：

```env
TORCHWOOD_DATA_DATABASE_SOURCE=postgres://torchwood:torchwood@127.0.0.1:5433/torchwood?sslmode=disable
TORCHWOOD_DATA_REDIS_ADDR=127.0.0.1:6380
TORCHWOOD_SECURITY_JWT_SECRET=change-me-in-production
TORCHWOOD_STORAGE_S3_ENDPOINT=http://127.0.0.1:9000
TORCHWOOD_STORAGE_S3_ACCESS_KEY_ID=minioadmin
TORCHWOOD_STORAGE_S3_SECRET_ACCESS_KEY=minioadmin
```

### 3. 运行数据库迁移

```bash
task migrate
```

### 4. 安装依赖并初始化数据

```bash
# 安装工具（首次）
task install-tools

# 安装 Console 依赖
task console-install

# 生成 protobuf、wire 等
task generate-all

# 创建默认项目和 Console 管理员
go run ./cmd/seed
```

默认管理员：`admin@torchwood.local / Admin@123`。

### 5. 构建并运行

```bash
task build      # 会先执行 console-build，再编译 Go server
./bin/server.exe
```

或直接开发模式：

```bash
task dev-server
```

访问：

- Admin Console：`http://torchwood.local:9099/console/`
- HTTP/gRPC-gateway API：`http://127.0.0.1:9099/v1/...`
- Metrics：`http://127.0.0.1:9100/metrics`

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
task console-install   # npm install
task console-build     # npm run build
task console-dev       # npm run dev

# 后端
task dev-server        # go run ./cmd/server
task test              # go test -v ./... -cover
task build             # 构建完整二进制（含 console）
```

## 项目结构

```
.
├── cmd/
│   ├── seed/              # 默认项目/管理员/API Key 初始化
│   └── server/            # 服务入口与 Wire 组装
├── console/               # Admin Console React SPA
│   ├── embed.go           # go:embed dist
│   └── src/
├── configs/               # 配置文件模板
├── db/migrations/         # golang-migrate SQL 迁移
├── docker/local/          # 本地 Docker Compose
├── docs/                  # 设计文档
├── genproto/              # 生成的 protobuf 代码
├── internal/
│   ├── api/               # gRPC handler / 自定义 HTTP handler
│   │   ├── clientgrpc/
│   │   ├── consolegrpc/
│   │   ├── servergrpc/
│   │   └── serverhttp/
│   ├── app/               # 用例层
│   │   ├── client/        # Account sign-up/sign-in
│   │   ├── console/       # Console auth
│   │   ├── functions/     # Functions use-case
│   │   ├── server/        # Projects / API keys / users / databases
│   │   └── storage/       # File / bucket metadata
│   ├── domain/            # 领域模型与端口
│   │   ├── databases/
│   │   ├── functions/
│   │   ├── projects/
│   │   ├── shared/
│   │   └── storage/
│   ├── infra/             # 适配器实现
│   │   ├── auth/          # Principal/Validator
│   │   ├── bun/           # 元数据 repositories
│   │   ├── clients/       # PG/Redis/S3 客户端
│   │   ├── documentdb/    # PostgreSQL 动态文档适配器
│   │   ├── functions/     # Docker executor stub
│   │   ├── server/        # gRPC/gateway/metrics/console 服务器
│   │   └── storage/       # MinIO 对象存储
│   ├── pkg/config/        # protobuf config schema
│   └── testutil/          # 集成测试工具
├── pkg/
│   ├── crud/              # 列表/分页/排序工具
│   ├── grpc/interceptor/  # 认证拦截器
│   ├── idgen/             # ID 生成
│   ├── jwtparser/         # JWT 签发/解析
│   ├── password/          # 密码哈希
│   └── query/             # Appwrite 风格查询 DSL
├── proto/                 # protobuf 源文件
├── sdk/                   # TypeScript SDK 与演示应用
├── buf.yaml / buf.gen.yaml
├── go.mod
├── Taskfile.yml
├── README.md
└── README_ZH.md
```

## 架构说明

- **Clean Architecture / DDD**：domain 定义端口，infra 提供实现，app 编排用例，api 负责传输。
- **AI / Agent-Native API 设计**：Protobuf 为单一事实来源；`buf generate` 产出 gRPC stub、grpc-gateway handler 及 `genproto/` 下的 OpenAPI 规范。**Server API**（`/v1/server/*`）面向程序化与 Agent 访问，通过 API Key 鉴权；**Client API**（`/v1/account/*`、`/v1/databases/*` 等）服务终端用户流程。详见 [`sdk/README.md`](sdk/README.md)。
- **动态文档数据库**：每个 database 对应一个 PostgreSQL schema；集合是真实表；`_tenant` 用于项目隔离；`_perms` 表实现基于角色的文档权限。
- **认证**：支持 end-user JWT、session Cookie、API Key、console admin JWT。API Key 不绕过 `_perms`，以 `keys` 角色参与权限检查；admin 可带 `X-Torchwood-Project` header 操作指定项目。
- **REST API**：gRPC 方法通过 grpc-gateway 暴露为 JSON REST；文件上传/下载使用自定义 HTTP handler。
- **Console**：React SPA 通过 `//go:embed dist` 打包进 Go 二进制，由 `/console/` 路径 serve。

## 测试

```bash
# 单元/集成测试（需要本地 Postgres）
task test
```

集成测试位于：

- `internal/infra/documentdb/postgres_test.go`
- `internal/app/client/account_test.go`

测试会自动创建并销毁 `TORCHWOOD_test` 数据库。

## TypeScript SDK

详见 [`sdk/README.md`](sdk/README.md) 中的 `@torchwood/sdk` 包与 Web 演示。

```bash
task sdk-install
task sdk-build
task sdk-demo   # 演示站点 http://localhost:5174
```

## 设计文档

- `docs/roadmap.md`：开发路线图（含 AI/Agent-Native 战略）
- `docs/tech-decision.md`：技术栈决策
- `docs/archived/`：已归档的历史设计文档（P0 设计、迁移清单、安全评审、修复方案）

## 许可证

MIT（待定）
