# Torchwood 环境搭建与快速开始

> 6 步本地启动，以代码为源：`Taskfile.yml`、`docker/local/docker-compose.yml`、`configs/config.yaml.template`、`.env.example`、`cmd/server/main.go`。
> 最新更新：2026-08-23

---

## 1. 前置条件

| 依赖 | 版本 | 说明 |
|------|------|------|
| Go | 1.26.5 | `go.mod` 要求 |
| Node.js + pnpm | 22 + pnpm 11.20 | Console 构建 |
| Docker + Compose | 新版 | PG/Redis/MinIO |
| Task | 最新 | `go install github.com/go-task/task/v3/cmd/task@latest` |

代码生成工具（`protoc-gen-go`/`migrate`/`buf@v1.65.0`/`wire`/`golangci-lint@v2.12.2`）由 `task install-tools` 统一安装。

---

## 2. 本地基础设施

`docker/local/docker-compose.yml`（端口可被 `.env` 覆盖）：

| 服务 | 镜像 | 默认端口 | 容器 |
|------|------|----------|------|
| PostgreSQL | `postgres:18-alpine` | 5432 | `torchwood-postgres` |
| Redis | `redis:7-alpine` | 6379 | `torchwood-redis` |
| MinIO | `minio/minio:RELEASE.2024-11-07T00-52-20Z` | 9000/9001 | `torchwood-minio` |

`.env` 覆盖键：`POSTGRES_USER`/`POSTGRES_PASSWORD`/`POSTGRES_DB`/`POSTGRES_PORT`/`REDIS_PORT`/`MINIO_API_PORT`/`MINIO_CONSOLE_PORT` 等。

应用侧连接走 `TORCHWOOD_` 前缀（`internal/pkg/config/config.proto:8` + `bind.go:14`）：

```env
TORCHWOOD_DATA_DATABASE_SOURCE=postgres://torchwood:torchwood@127.0.0.1:5432/torchwood?sslmode=disable
TORCHWOOD_DATA_REDIS_PASSWORD=
TORCHWOOD_SECURITY_JWT_SECRET=dev-only-0123456789abcdef-0123456789abcdef  # ≥32 字符，含弱子串拒绝启动（cmd/server/provides.go:85）
TORCHWOOD_SECURITY_SETUP_TOKEN=dev-setup-0123456789abcdef0123456789abcdef # 首个管理员引导令牌，未配则注册被拒
TORCHWOOD_STORAGE_S3_ENDPOINT=http://127.0.0.1:9000
TORCHWOOD_STORAGE_S3_ACCESS_KEY_ID=minioadmin
TORCHWOOD_STORAGE_S3_SECRET_ACCESS_KEY=minioadmin
TORCHWOOD_TEST_DATABASE_SOURCE=postgres://torchwood:torchwood@127.0.0.1:5432/TORCHWOOD_test?sslmode=disable
TORCHWOOD_TEST_ADMIN_DATABASE_SOURCE=postgres://torchwood:torchwood@127.0.0.1:5432/postgres?sslmode=disable
```

> `TORCHWOOD_STORAGE_S3_*` 键名由 `bind.go:envNameForKey` 按 proto json tag 推导；`configs/config.yaml.template:82` 与 `AGENTS.md` 一致。

---

## 3. 6 步本地启动

### 步骤 0 — 复制环境模板

```bash
cp .env.example .env
# 生产需替换 JWT/SETUP 为强随机：openssl rand -hex 32
```

### 步骤 1 — 启动基础设施

```bash
task up          # docker compose up -d（docker/local）
docker ps        # 三容器 healthy
```

### 步骤 2 — 数据库迁移

```bash
task migrate     # migrate -path ./db/migrations -database <DSN> up
```

DSN 优先 `TORCHWOOD_DATA_DATABASE_SOURCE`，否则由 `POSTGRES_*` 拼接（`Taskfile.yml:49`）。跨 `Taskfile` 的 `.env` 自动加载（`dotenv: ['.env']`）。

### 步骤 3 — 安装工具与依赖

```bash
task install-tools    # protoc-gen-go / migrate / buf@v1.65.0 / wire / golangci-lint
task console-install  # pnpm install（console/）
```

### 步骤 4 — 生成代码

```bash
task generate-all  # generate-proto → generate-config → wire-all
```

| 任务 | 产物 |
|------|------|
| `generate-proto` | `buf lint` + `buf generate` → `genproto/` |
| `generate-config` | `protoc -I. --go_out=.` 在 `internal/pkg/config` 内产出 `config.pb.go` |
| `wire-all` | `cmd/server/wire_gen.go` + `cmd/worker/wire_gen.go` |

全量零漂移校验见 `04-codegen.md §5`。

### 步骤 5 — 构建并启动

```bash
task build          # console-build → go build server/worker/CLI → ./bin/
./bin/server        # Windows 为 ./bin/server.exe
# 或开发态：
task dev-server     # go run ./cmd/server
task worker         # go run ./cmd/worker（独立进程）
```

修改 `console/src/` 后须 `task console-build && task build`（`console/embed.go:go:embed dist`）。

### 步骤 6 — 首次引导（bootstrap）

全新库上打开 `http://127.0.0.1:9080/console/`，登录页自动切换为「初始化设置」表单（`internal/app/console/setup.go`）。前提：已配 `TORCHWOOD_SECURITY_SETUP_TOKEN`，否则 `SignUp` 直接 `FailedPrecondition`。

- 首个管理员固定 `owner`，仅 `admins` 为空时可用；
- 同时创建指定 `project_id`（自动含系统 `default` 库），若 `database_id != default` 再建该业务库；
- 注册成功后写入 `TORCHWOOD_session_console` HttpOnly cookie（`SameSite=Lax`，refresh 限 `/v1/console/auth`）；
- API Key 不在注册时生成，登录后到 **API Keys** 页面创建，再以 `x-api-key` 调 Server API。

---

## 4. 端点（`configs/config.yaml.template` 默认）

| 表面 | 地址 |
|------|------|
| Admin Console | `http://127.0.0.1:9080/console/` |
| HTTP / grpc-gateway | `http://127.0.0.1:9080/v1/...`（如 `/v1/server/users`） |
| gRPC（仅回环） | `127.0.0.1:9060`（`server.grpc.addr`） |
| Metrics | `http://127.0.0.1:9040/metrics`（`server.metrics.addr` 为空回退同值，`internal/infra/server/metrics.go:18`） |
| 健康检查 | `http://127.0.0.1:9080/healthz/liveness`、`/healthz/readiness` |

HTTP/Metrics 端口由 `server.http.addr` / `server.metrics.addr` 决定，非硬编码；`task console-dev` 的 Vite 代理指向同源 `/v1`。

---

## 5. 常用任务速查

| 任务 | 用途 |
|------|------|
| `install-tools` | 安装 buf/wire/migrate 等 |
| `up`/`down`/`clean` | 启动/停止/删卷（`docker compose down -v`） |
| `migrate` | 执行 `db/migrations` |
| `generate-proto`/`generate-config`/`wire-all`/`generate-all` | Buf / config proto / Wire |
| `lint-proto` | `buf lint` + `buf breaking --against '.git#branch=origin/main'` |
| `console-install`/`console-build`/`console-dev` | 前端 pnpm |
| `dev-server`/`worker` | 直跑 server/worker |
| `build` | `console-build` + 三二进制 |
| `test` | `lint-go` + `test-sdk-go` + `test-sdk-ts` + `go test -v ./... -cover` |
| `lint` | `lint-go` + `lint-golangci` + `lint-sdk-go` + `lint-console` |
| `build-docker` | `docker build -t torchwood:<ver>` |

`task test` 自动从 `.env` 加载 `TORCHWOOD_TEST_*`；`lint-golangci` 为 `--new-from-rev=origin/main` 棘轮（`Taskfile.yml:172`）。

---

## 6. 常见问题

- **端口占用**：改 `.env` 中 `POSTGRES_PORT`/`REDIS_PORT`/`MINIO_*_PORT` 后重 `task up`；应用端口改 `configs/config.yaml`。
- **`task migrate` 失败**：确认 `docker ps` healthy；检查 DSN 与 `POSTGRES_*` 一致；需重置用 `task clean`。
- **Console 未更新**：`embed dist` 需重 `console-build` 再 `build`；调试用 `task console-dev`。
- **鉴权失败**：检查 `JWT_SECRET` 长度/弱子串；`SETUP_TOKEN` 是否配置；API Key 用 `x-api-key`，多项目需 `X-Torchwood-Project`。
- **反代后 IP 不准**：默认不信 `X-Forwarded-For`，需配 `security.trusted_proxies`（`TORCHWOOD_SECURITY_TRUSTED_PROXIES=127.0.0.1/32`）。
- **直接 `go test` 报错**：需导出 `TORCHWOOD_TEST_*`，或用 `task test`。

---

## 7. CLI（`cmd/client`）

`bin/torchwood` 经 gRPC 直连 Server API（`sdk/go/server.InvokeJSON` 动态分发，新增 RPC 无需登记；`import_guard_test.go` 兜底禁直连 genproto）：

```bash
./bin/torchwood health get
./bin/torchwood uuid
./bin/torchwood users list --api-key <secret>
./bin/torchwood databases documents create app notes --data '{"title":"hi"}' --document-id doc1
./bin/torchwood rpc /torchwood.server.v1.UsersService/ListUsers --data '{"pageSize":10}' --api-key <secret>
```

全局 flag：`--endpoint`（`TORCHWOOD_CLI_ENDPOINT=127.0.0.1:9060`）、`--api-key`、`--timeout`、`--output`。
