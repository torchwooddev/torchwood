# Torchwood 环境搭建与快速开始

> 本文档指导从零搭建 Torchwood 本地开发环境并启动完整服务。
> 最新更新：2026-08-09

---

## 1. 前置条件

| 依赖 | 版本要求 | 说明 |
|------|----------|------|
| Go | 1.25+ | 后端语言与运行时 |
| Node.js | 22+ | Console 前端构建 |
| pnpm | 任意较新版本 | Console 依赖管理（`task console-install` 使用 `pnpm install`） |
| Docker + Docker Compose | 任意较新版本 | 本地基础设施（Postgres/Redis/MinIO） |
| [Task](https://taskfile.dev/) | 最新 | 任务执行器，安装：`go install github.com/go-task/task/v3/cmd/task@latest` |

> 代码生成工具（protoc-gen-go、migrate、buf、wire）不需要手工安装，由 `task install-tools` 统一安装。

---

## 2. 本地基础设施

`docker/local/docker-compose.yml` 定义三个服务，端口全部可由 `.env` 覆盖：

| 服务 | 镜像 | 默认端口 | 容器名 | 数据卷 |
|------|------|----------|--------|--------|
| PostgreSQL | `postgres:18-alpine` | 5432 | `torchwood-postgres` | `postgres_data` |
| Redis | `redis:7-alpine` | 6379 | `torchwood-redis` | `redis_data` |
| MinIO | `minio/minio:RELEASE.2024-11-07T00-52-20Z` | 9000（API）/ 9001（Console） | `torchwood-minio` | `minio_data` |

Compose 从 `.env` 读取覆盖变量（未设置时使用默认值）：

```env
POSTGRES_USER=torchwood
POSTGRES_PASSWORD=torchwood
POSTGRES_DB=torchwood
POSTGRES_PORT=5432
REDIS_PORT=6379
MINIO_API_PORT=9000
MINIO_CONSOLE_PORT=9001
MINIO_ROOT_USER=minioadmin
MINIO_ROOT_PASSWORD=minioadmin
```

应用侧连接信息由 `TORCHWOOD_` 前缀环境变量控制（配置 schema 见 `internal/pkg/config/config.proto`，绑定逻辑见 `internal/pkg/config/bind.go`，键名从点号路径映射而来）：

```env
# Database
TORCHWOOD_DATA_DATABASE_SOURCE=postgres://torchwood:torchwood@127.0.0.1:5432/torchwood?sslmode=disable

# Redis（本仓库 .env.example 中只有 password，addr 走 configs/config.yaml 的 data.redis.addr）
TORCHWOOD_DATA_REDIS_PASSWORD=

# JWT / Session
TORCHWOOD_SECURITY_JWT_SECRET=change-me-in-production

# Object Storage (MinIO example)
TORCHWOOD_STORAGE_S3_ENDPOINT=http://127.0.0.1:9000
TORCHWOOD_STORAGE_S3_ACCESS_KEY_ID=minioadmin
TORCHWOOD_STORAGE_S3_SECRET_ACCESS_KEY=minioadmin

# Integration tests（task test 自动加载）
TORCHWOOD_TEST_DATABASE_SOURCE=postgres://torchwood:torchwood@127.0.0.1:5432/TORCHWOOD_test?sslmode=disable
TORCHWOOD_TEST_ADMIN_DATABASE_SOURCE=postgres://torchwood:torchwood@127.0.0.1:5432/postgres?sslmode=disable
```

---

## 3. 分步快速开始

### 步骤 0：复制环境变量模板

```bash
cp .env.example .env
```

> 建议在 `.env` 中修改 `TORCHWOOD_SECURITY_JWT_SECRET`（生产环境必须替换）。

### 步骤 1：启动本地基础设施

```bash
task up        # docker compose up -d（docker/local 目录）
```

启动完成后可用 `docker ps` 确认三个容器 healthy。

### 步骤 2：运行数据库迁移

```bash
task migrate
```

`task migrate` 优先使用 `TORCHWOOD_DATA_DATABASE_SOURCE`，否则由 `POSTGRES_*` 变量拼接 DSN，对 `db/migrations/` 执行 golang-migrate `up`。

### 步骤 3：安装工具与依赖

```bash
# 首次：安装代码生成工具（protoc-gen-go、migrate、buf@v1.63.0、wire）
task install-tools

# 安装 Console 前端依赖（pnpm install）
task console-install
```

### 步骤 4：生成代码

```bash
task generate-all    # = generate-proto（buf generate）+ generate-config（config.proto → go）+ wire-all
```

`generate-all` 依次执行：

| 任务 | 生成内容 |
|------|----------|
| `generate-proto` | `genproto/`：gRPC stub、gateway handler、Swagger JSON |
| `generate-config` | `internal/pkg/config/config.pb.go` |
| `wire-all` | `cmd/server/wire_gen.go` + `cmd/worker/wire_gen.go` |

### 步骤 5：引导默认数据

```bash
go run ./cmd/seed
```

幂等创建：

- 默认项目（id = `default`）；
- Console 管理员：**`admin@torchwood.local / Admin@123`**；
- 默认 API Key（scope 覆盖 projects/users/storage/databases/teams 的 read/write），**secret 仅在首次创建时打印**，重复运行不会重建；如需轮换，删除 `default-default-api-key` 行后重跑 seed。

### 步骤 6：构建并启动

```bash
task build         # 先 console-build，再编译 server 与 worker 到 ./bin/
./bin/server.exe   # Windows；Linux/macOS 为 ./bin/server
```

或开发模式直接运行：

```bash
task dev-server    # go run ./cmd/server
```

首次登录 Console：浏览器打开 Console 地址 → 使用 `admin@torchwood.local / Admin@123` 登录。会话凭证为 HttpOnly cookie（`TORCHWOOD_session_console`），前端不再使用 localStorage 存储 token。

---

## 4. 端点

以下为仓库 `configs/config.yaml.template` 的**默认值**（监听端口并非硬编码，见下方说明）：

| 端点 | 地址 |
|------|------|
| Admin Console | `http://127.0.0.1:9080/console/` |
| HTTP / grpc-gateway API | `http://127.0.0.1:9080/v1/...`（如 `/v1/server/users`、`/v1/account/...`） |
| Metrics | `http://127.0.0.1:9040/metrics` |
| gRPC（仅回环） | `127.0.0.1:9060` |
| 健康检查 | `http://127.0.0.1:9080/healthz/liveness`、`/healthz/readiness` |

> **端口说明**：HTTP/API 与 Metrics 端口并非硬编码。HTTP 监听地址由 `configs/config.yaml` 的 `server.http.addr` 决定（默认 `:9080`），Metrics 由 `server.metrics.addr` 决定（默认 `:9040`，配置为空时 Metrics 回退到 `:9100`，见 `internal/infra/server/metrics.go`）。
>
> **注意仓库内遗留的 9099 引用**：README 与部分文件仍沿用旧端口 —— CORS `allow_origins` 写的是 `http://torchwood.local:9099`，`console/vite.config.ts` 的 dev 代理也指向 `http://localhost:9099`，而当前配置模板默认是 `:9080`。若按默认端口运行：
> - 直接在浏览器访问 `http://127.0.0.1:9080/console/`（Console 与 API 同源，CORS 不生效）；
> - 使用 `task console-dev` 时请先把 `console/vite.config.ts` 的代理目标改为 `http://localhost:9080`；
> - 若坚持使用 `http://torchwood.local:9099`，需修改本地 `configs/config.yaml` 的 `server.http.addr` 与 `allow_origins`，并把 `torchwood.local` 在 hosts 中解析到 127.0.0.1。

---

## 5. 常用任务速查表

| 任务 | 命令 | 用途 |
|------|------|------|
| `install-tools` | `go install protoc-gen-go / migrate / buf / wire` | 安装代码生成与迁移工具（首次执行） |
| `up` | `docker compose up -d`（docker/local） | 启动 Postgres/Redis/MinIO |
| `down` | `docker compose down` | 停止基础设施（保留数据卷） |
| `clean` | `docker compose down -v` | 停止并**删除数据卷**（数据全清） |
| `migrate` | `migrate -path ./db/migrations up` | 执行 SQL 迁移 |
| `generate-proto` | `buf generate` | 生成 gRPC / gateway / Swagger 代码 |
| `generate-config` | `protoc config.proto` | 生成 Go 配置代码 |
| `wire-server` | `go run wire ./cmd/server` | 重新生成 server 的 Wire 注入代码 |
| `wire-worker` | `go run wire ./cmd/worker` | 重新生成 worker 的 Wire 注入代码 |
| `wire-all` | 上面两个 | 重新生成全部 Wire 代码 |
| `generate-all` | 上述三个生成任务 | 一键生成 proto + config + wire |
| `console-install` | `pnpm install`（console/） | 安装 Console 依赖 |
| `console-build` | `pnpm run build`（console/） | 构建 Console SPA 到 `console/dist` |
| `console-dev` | `pnpm run dev`（console/） | 启动 Vite dev server（代理 `/v1` 到本地 Go server） |
| `dev-server` | `go run ./cmd/server` | 开发模式启动服务器 |
| `worker` | `go run ./cmd/worker` | 启动 Worker 进程 |
| `build` | `console-build` + `go build ./cmd/server ./cmd/worker` | 构建完整二进制到 `./bin/`（含 Console embed） |
| `test` | `go test -v ./... -cover` | 运行全部测试（含集成测试，自动从 `.env` 加载测试 DSN） |
| `lint-go` | `go vet ./...` + `gofmt -l .` | Go 静态检查与格式检查 |
| `lint-console` | `pnpm lint`（console/） | Console lint |
| `lint` | 上面两个 | 全量 lint |
| `build-docker` | `docker build` | 构建 `torchwood:<version>-<git>-<ts>` 镜像 |
| `sdk-install` | `npm install`（sdk/typescript + demo） | 安装 TypeScript SDK 依赖 |
| `sdk-build` | `npm run build`（sdk/typescript） | 构建 SDK |
| `sdk-demo` | `npm run dev`（sdk/demo） | 启动 SDK demo（`http://localhost:5174`） |

---

## 6. 常见问题

### 端口被占用

- 修改 `.env` 中的 `POSTGRES_PORT` / `REDIS_PORT` / `MINIO_API_PORT` / `MINIO_CONSOLE_PORT` 后重新 `task up`；
- 应用侧端口（HTTP/gRPC/Metrics）在 `configs/config.yaml` 中调整；
- 排查占用：Windows `netstat -ano | findstr <port>`，Linux/macOS `lsof -i :<port>`。

### `task migrate` 失败

- 确认容器已启动且 healthy：`docker ps`；
- 确认 `.env` 中 DSN 与 `POSTGRES_*` 变量一致（用户名/密码/端口）；
- 迁移报「已是最新」或重复迁移时，可查看 `db/migrations/` 目录下的版本序列；需要重置数据时用 `task clean`（会删除数据卷）后重来。

### 修改了 Console 代码但页面没变化

Console 是 **embedded SPA**：Go 二进制通过 `console/embed.go` 的 `//go:embed dist` 打包静态资源。修改 `console/src/` 后**必须依次执行**：

```bash
task console-build   # 重新构建 dist
task build           # 重新编译 Go 二进制
```

否则运行的仍是旧版本（`task dev-server` 直接 `go run` 同样如此）。调试前端 UI 时建议使用 `task console-dev`（Vite dev server，热更新并代理 `/v1`）。

### 登录/API Key 鉴权失败

- 检查 `TORCHWOOD_SECURITY_JWT_SECRET` 是否已设置（.env）；
- API Key 需带 `x-api-key` header（或 `Authorization: Bearer`）；访问多项目数据时，API Key 指定目标项目需带 `X-Torchwood-Project` header；
- API Key 以 `keys` 角色参与 `_perms` 文档权限，不默认绕过文档级权限。

### 反向代理后客户端 IP 不准确

默认不信任 `X-Forwarded-For`/`X-Real-Ip`，一律使用 gRPC peer 地址。经代理部署时设置 `security.trusted_proxies`（如 `TORCHWOOD_SECURITY_TRUSTED_PROXIES=127.0.0.1/32`）；grpc-gateway 与 gRPC 同进程部署时需包含 `127.0.0.1/32`。

### 测试直接 `go test ./...` 报错

集成测试需要本地 Postgres 与测试 DSN 环境变量（`TORCHWOOD_TEST_DATABASE_SOURCE`、`TORCHWOOD_TEST_ADMIN_DATABASE_SOURCE`）。`task test` 会自动从 `.env` 加载；直接运行 `go test` 时请先导出这两个变量。测试会自动创建/删除 `TORCHWOOD_test` 数据库。
