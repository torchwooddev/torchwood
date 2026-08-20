# Torchwood 环境搭建与快速开始

> 本文档指导从零搭建 Torchwood 本地开发环境并启动完整服务。
> 最新更新：2026-08-12

---

## 1. 前置条件

| 依赖 | 版本要求 | 说明 |
|------|----------|------|
| Go | 1.26.5+ | 后端语言与运行时（`go.mod` 要求 Go 1.26.5） |
| Node.js | 22+ | Console 前端构建 |
| pnpm | 任意较新版本 | Console 依赖管理（`console/package.json` 的 `packageManager` 锁定 pnpm@11.20.0，`task console-install` 使用 `pnpm install`） |
| Docker + Docker Compose | 任意较新版本 | 本地基础设施（Postgres/Redis/MinIO） |
| [Task](https://taskfile.dev/) | 最新 | 任务执行器，安装：`go install github.com/go-task/task/v3/cmd/task@latest` |

> 代码生成工具（protoc-gen-go、migrate、buf@v1.63.0、wire）不需要手工安装，由 `task install-tools` 统一安装。

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

# JWT / Session（≥32 字符，且不得包含弱子串，否则拒绝启动）
TORCHWOOD_SECURITY_JWT_SECRET=dev-only-0123456789abcdef-0123456789abcdef

# Console 首次引导令牌（未设置时注册第一个管理员被拒绝）
TORCHWOOD_SECURITY_SETUP_TOKEN=dev-setup-0123456789abcdef0123456789abcdef

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

> 建议在 `.env` 中修改 `TORCHWOOD_SECURITY_JWT_SECRET`（生产环境必须替换为强随机值）并生成 `TORCHWOOD_SECURITY_SETUP_TOKEN`（如 `openssl rand -hex 32`）。

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
task generate-all    # = generate-proto（buf lint + buf generate）+ generate-config（config.proto → go）+ wire-all
```

`generate-all` 依次执行：

| 任务 | 生成内容 |
|------|----------|
| `generate-proto` | `buf lint` + `buf generate` → `genproto/`：gRPC stub、gateway handler、Swagger JSON |
| `generate-config` | `internal/pkg/config/config.pb.go` |
| `wire-all` | `cmd/server/wire_gen.go` + `cmd/worker/wire_gen.go` |

### 步骤 5：构建并启动

```bash
task build         # 先 console-build，再编译 server / worker / CLI 到 ./bin/
./bin/server.exe   # Windows；Linux/macOS 为 ./bin/server
```

或开发模式直接运行：

```bash
task dev-server    # go run ./cmd/server
```

### 步骤 6：首次部署引导（bootstrap）

全新数据库上，浏览器打开 Console 地址 → 登录页会自动切换为「初始化设置」
表单。**前提：必须已配置 `TORCHWOOD_SECURITY_SETUP_TOKEN`**（未配置时注册
被拒绝，`internal/app/console/setup.go` 的 `SignUp` 直接返回 FailedPrecondition）。
注册第一个管理员时需同时填写 `project_id` 与 `database_id`，将自动：

- 创建 **owner** 管理员账户（首个管理员固定为超管，仅当 `admins` 表为空
  时可用；`POST /v1/console/auth/sign-up` 二次调用返回 `FailedPrecondition`）；
- 创建指定项目（随项目自动创建系统 `default` 库）；若 `database_id` 不是
  `default`，再额外创建该业务库；
- 注册成功后直接进入 Console（会话为 HttpOnly cookie
  `TORCHWOOD_session_console`，前端不存储 token）。

API Key **不在注册时生成**。登录后进入 Console 的 **API Keys** 页面创建，
再用该 secret 以 `x-api-key` metadata 调用 Server API（如
`UsersService/ListUsers`）。后续管理员由 Console 的「管理员」页面创建。

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

> **端口说明**：HTTP/API 与 Metrics 端口并非硬编码。HTTP 监听地址由 `configs/config.yaml` 的 `server.http.addr` 决定（默认 `:9080`），Metrics 由 `server.metrics.addr` 决定（默认 `127.0.0.1:9040`；配置为空时回退到 `127.0.0.1:9040`，见 `internal/infra/server/metrics.go` 的 `NewMetricsServer`）。
>
> **注意仓库内遗留的 9099 引用**：部分文件仍沿用旧端口 —— CORS `allow_origins` 写的是 `http://torchwood.local:9099`，`console/vite.config.ts` 的 dev 代理也指向 `http://localhost:9099`，而当前配置模板默认是 `:9080`。若按默认端口运行：
> - 直接在浏览器访问 `http://127.0.0.1:9080/console/`（Console 与 API 同源，CORS 不生效）；
> - 使用 `task console-dev` 时请先把 `console/vite.config.ts` 的代理目标改为 `http://localhost:9080`；
> - 若坚持使用 `http://torchwood.local:9099`，需修改本地 `configs/config.yaml` 的 `server.http.addr` 与 `allow_origins`，并把 `torchwood.local` 在 hosts 中解析到 127.0.0.1。

---

## 5. 常用任务速查表

| 任务 | 命令 | 用途 |
|------|------|------|
| `install-tools` | `go install protoc-gen-go / migrate / buf@v1.63.0 / wire` | 安装代码生成与迁移工具（首次执行） |
| `up` | `docker compose up -d`（docker/local） | 启动 Postgres/Redis/MinIO |
| `down` | `docker compose down` | 停止基础设施（保留数据卷） |
| `clean` | `docker compose down -v` | 停止并**删除数据卷**（数据全清） |
| `migrate` | `migrate -path ./db/migrations -database <DSN> up` | 执行 SQL 迁移（DSN 优先取 `TORCHWOOD_DATA_DATABASE_SOURCE`） |
| `generate-proto` | `buf lint` + `buf generate` | 生成 gRPC / gateway / Swagger 代码 |
| `generate-config` | `protoc config.proto` | 生成 Go 配置代码 |
| `wire-server` | `go run github.com/google/wire/cmd/wire`（cmd/server） | 重新生成 server 的 Wire 注入代码 |
| `wire-worker` | `go run github.com/google/wire/cmd/wire`（cmd/worker） | 重新生成 worker 的 Wire 注入代码 |
| `wire-all` | 上面两个 | 重新生成全部 Wire 代码 |
| `generate-all` | 上述三个生成任务 | 一键生成 proto + config + wire |
| `console-install` | `pnpm install`（console/） | 安装 Console 依赖 |
| `console-build` | `pnpm run build`（console/） | 构建 Console SPA 到 `console/dist` |
| `console-dev` | `pnpm run dev`（console/） | 启动 Vite dev server（代理 `/v1` 到本地 Go server） |
| `dev-server` | `go run ./cmd/server` | 开发模式启动服务器 |
| `worker` | `go run ./cmd/worker` | 启动 Worker 进程 |
| `build` | `console-build` + `go build` server / worker / CLI | 构建三个二进制到 `./bin/`（含 Console embed） |
| `test` | `test-sdk-go` + `test-sdk-ts` + `go test -v ./... -cover` | 运行全部测试（SDK 测试 + 集成测试，自动从 `.env` 加载测试 DSN） |
| `lint-go` | `go vet ./...` + `gofmt -l .` | Go 静态检查与格式检查 |
| `lint-sdk-go` | `go vet ./...` + `gofmt -l .`（sdk/go） | Go SDK 检查 |
| `lint-console` | `pnpm lint`（console/） | Console lint |
| `lint` | lint-go + lint-sdk-go + lint-console | 全量 lint |
| `build-docker` | `docker build` | 构建 `torchwood:<version>-<git>-<ts>` 镜像 |
| `sdk-install` | `npm install`（sdk/typescript + demo） | 安装 TypeScript SDK 依赖 |
| `sdk-build` | `npm run build`（sdk/typescript） | 构建 SDK |
| `sdk-demo` | `npm run dev`（sdk/demo，自动先 `sdk-build`） | 启动 SDK demo（`http://localhost:5174`） |

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

- 检查 `TORCHWOOD_SECURITY_JWT_SECRET` 是否已设置（.env，且 ≥32 字符、不含弱子串）；
- 首次引导失败：检查 `TORCHWOOD_SECURITY_SETUP_TOKEN` 是否已配置；
- API Key 需带 `x-api-key` header（或 `Authorization: Bearer`）；访问多项目数据时，API Key 指定目标项目需带 `X-Torchwood-Project` header；
- API Key 以 `keys` 角色参与 `_perms` 文档权限，不默认绕过文档级权限。

### 反向代理后客户端 IP 不准确

默认不信任 `X-Forwarded-For`/`X-Real-Ip`，一律使用 gRPC peer 地址。经代理部署时设置 `security.trusted_proxies`（如 `TORCHWOOD_SECURITY_TRUSTED_PROXIES=127.0.0.1/32`）；grpc-gateway 与 gRPC 同进程部署时需包含 `127.0.0.1/32`。

### 测试直接 `go test ./...` 报错

集成测试需要本地 Postgres 与测试 DSN 环境变量（`TORCHWOOD_TEST_DATABASE_SOURCE`、`TORCHWOOD_TEST_ADMIN_DATABASE_SOURCE`）。`task test` 会自动从 `.env` 加载；直接运行 `go test` 时请先导出这两个变量。测试会自动创建/删除 `TORCHWOOD_test` 数据库。

---

## 7. Torchwood CLI（cmd/client）

`task build` 产出 `bin/torchwood[.exe]`：通过 API Key 走 **gRPC**（非 HTTP gateway）调用 Server API，适合 Agent/自动化与运维场景。`health`、`uuid` 公开/本地可调用，其余命令需要 API key。

```bash
./bin/torchwood health get                          # 无需 key
./bin/torchwood uuid                                # 生成本地 UUID，无需 key
./bin/torchwood users list --api-key <secret>       # 列出用户
./bin/torchwood projects get default --api-key <secret>
./bin/torchwood rpc /torchwood.server.v1.UsersService/ListUsers --data '{"pageSize": 10}' --api-key <secret>
```

具名命令覆盖 `proto/server/v1` 全部资源（除 `APIKeysService` 与 `projects create/update`）：

```text
torchwood
├── health                 # 公开
├── uuid                   # 生成本地 UUID v4（无需 key）
├── projects               # list/get（create/update 限平台 admin）
├── users                  # 全部方法（含 sessions/tokens 子命令）
├── databases              # 库；collections/attributes/indexes/documents 子命令
├── groups                  # 用户组；prefs/memberships 子命令
├── storage                # buckets；files（元数据）；usage（不做上传/下载）
├── functions              # 函数；deployments/variables/executions 子命令
├── oauth-providers        # list/upsert/delete
└── rpc                    # 逃生舱：按完整 gRPC 方法名调用
```

命令示例（复杂结构一律 JSON 字符串 flag）：

```bash
./bin/torchwood databases create --id app --name '应用库'
./bin/torchwood databases collections create app --id notes --name '笔记' \
  --permissions '["read(\"users\")"]'
./bin/torchwood databases documents create app notes \
  --data '{"title":"hi"}' --document-id doc1
./bin/torchwood databases documents list app notes \
  --queries '["equal(\"status\",\"active\")"]' --page-size 20
./bin/torchwood groups create --name '核心组'
./bin/torchwood groups memberships create <group-id> --user-id <uid> --roles '["admin"]'
./bin/torchwood storage buckets create --name assets --public
./bin/torchwood storage usage
./bin/torchwood functions create --id hello --name hello --runtime nodejs18
./bin/torchwood functions deployments create hello --code ./code.zip
./bin/torchwood functions variables set hello --vars '{"FOO":"bar"}'
./bin/torchwood functions executions create hello --input '{"name":"world"}' --async
./bin/torchwood oauth-providers upsert google --client-id <id> --client-secret <sec>
```

全局参数（均可被同名 `TORCHWOOD_CLI_*` 环境变量覆盖）：

| Flag | 环境变量 | 默认值 | 说明 |
|------|----------|--------|------|
| `--endpoint` | `TORCHWOOD_CLI_ENDPOINT` | `127.0.0.1:9060` | gRPC 地址（服务端仅监听回环，远程走 SSH 隧道） |
| `--api-key` | `TORCHWOOD_CLI_API_KEY` | 无 | API Key secret（`health` / `uuid` 除外必填） |
| `--timeout` | `TORCHWOOD_CLI_TIMEOUT` | `30s` | 单次调用超时 |
| `--output` | `TORCHWOOD_CLI_OUTPUT` | `json` | 输出格式（MVP 仅 json） |

> **CLI 如何覆盖 Server API 方法**：`rpc` 逃生舱与全部具名命令最终都走
> `sdk/go/server` 的 `InvokeJSON`（按 full method name 从
> `protoregistry.GlobalFiles` 动态分发），**新增 Server API RPC 无需在 CLI
> 登记**；`cmd/client/import_guard_test.go` 兜底禁止 CLI 源码直接 import
> genproto/grpc。完整命令树见 `torchwood --help`。
