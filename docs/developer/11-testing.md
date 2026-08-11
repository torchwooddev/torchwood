# Torchwood 测试与质量保障

> 本文说明测试分层、集成测试数据库约定、CI 流水线、Lint 与质量观测能力。
> 目标读者：所有提交代码的开发者。
> 关联：`AGENTS.md`、`docs/implementation-health-observability.md`（健康/日志/慢查询实现细节）。
> 修订记录：2026-08-09 初版（testutil、CI、lint、可观测性按代码核实）。

---

## 1. 测试分层

| 层级 | 是否需要真实数据库 | 典型位置 | 示例 |
|------|-------------------|----------|------|
| 纯单元测试 | 否（stub/memstore） | `pkg/`、`internal/domain/`、`internal/api/*grpc/` | `pkg/crud/list_test.go`、`internal/domain/projects/idgen_settings_test.go`、`internal/api/servergrpc/projects_test.go` |
| 拦截器测试 | 部分需要（validator 依赖 repo） | `pkg/grpc/interceptor/` | `jwt_auth_test.go`、`apikey_scope_test.go` |
| 集成测试 | 是（testutil 建库） | `internal/infra/*/`、`internal/app/*/` | `internal/infra/documentdb/postgres_test.go`、`internal/app/client/account_test.go` |
| 端到端/真实监听 | 是 | `internal/infra/server/` | `grpc_gateway_test.go`、`healthz_test.go`（readiness 503 真实监听） |

通用约定：

- 集成测试开头必须 `if testing.Short() { t.Skip("skipping integration test") }`，
  与 `go test -short` 兼容（`postgres_test.go` 全文如此）；
- 断言统一使用 `github.com/stretchr/testify/require`；
- gRPC 错误断言用 `status.Code(err)` + `codes.X`（如 `account_test.go` 校验重复邮箱 →
  `codes.AlreadyExists`；`postgres_test.go` 校验越界 offset → `codes.InvalidArgument`）；
- api 层 handler 测试用最小 stub 端口 + `contexts.WithPrincipal` 注入身份
  （`internal/api/servergrpc/projects_test.go`：`stubProjectRepo` + `newTestProjectsService`），
  不需要数据库。

---

## 2. 集成测试数据库辅助（`internal/testutil/`）

### 2.1 环境变量约定（`.env.example`）

```dotenv
# Integration tests (task test loads this file automatically)
TORCHWOOD_TEST_DATABASE_SOURCE=postgres://torchwood:torchwood@127.0.0.1:5432/TORCHWOOD_test?sslmode=disable
TORCHWOOD_TEST_ADMIN_DATABASE_SOURCE=postgres://torchwood:torchwood@127.0.0.1:5432/postgres?sslmode=disable
```

- `TORCHWOOD_TEST_DATABASE_SOURCE`：测试 DSN 模板。**库名会被替换**：`SetupTestDB` 基于它派生一个
  唯一数据库名 `<库名>_<pid>_<seq>`（强制小写，如 `torchwood_test_1234_1`）后自动 `CREATE DATABASE`；
- `TORCHWOOD_TEST_ADMIN_DATABASE_SOURCE`：Postgres 维护库（`postgres`）DSN，用于建库/删库；
- 没有硬编码回退：两个变量缺失时 `SetupTestDB` 直接 `t.Fatal`（提示“run via `task test`”）。

### 2.2 `testutil.SetupTestDB(t)` 生命周期

```go
db := testutil.SetupTestDB(t)   // 建唯一测试库 → 执行 db/migrations/*.up.sql（按文件名排序）
defer db.Close()
```

- 自动注册 `t.Cleanup`：先 `pg_terminate_backend` 杀残留连接，再 `DROP DATABASE IF EXISTS`，
  测试结束不留垃圾库；
- 迁移执行的是 `db/migrations/*.up.sql`（无需 golang-migrate 实例）；
- 返回 `*clients.Database`（内嵌 bun.DB），可直接跑 bun 查询。

### 2.3 常用 fixture（均返回 cleanup func）

| 函数 | 用途 |
|------|------|
| `testutil.CreateTestProject(ctx, db)` | 插入测试项目，返回 `(projectID, internalID, cleanup)` |
| `testutil.CreateTestAdmin(ctx, db, role)` | 插入 console admin（默认密码 `Admin@123`），返回 `(model, cleanup)` |
| `testutil.SignAdminToken(cfg, admin)` | 签发与 `auth.Validator` 兼容的 admin JWT |
| `testutil.GrantAdminProject(ctx, db, adminID, projectID)` | 把非平台 admin 绑定到项目 |
| `testutil.CreateTestAPIKey(ctx, db, projectID, scopes)` | 插入 API key，返回 `(rawSecret, cleanup)` |
| `testutil.NewMemObjectStore` | 内存 ObjectStore（storage/health 测试用，实现 `ObjectStore` 端口含 `Ping`） |
| `testutil.NewInterceptorEnv(db, cfg, docDB)` | 按生产方式装配 auth + audit 拦截器，`InvokeUnary` 直接跑鉴权链 |

### 2.4 集成测试示例（`internal/infra/documentdb/postgres_test.go`）

```go
func TestPostgresDocumentDatabase_CRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))
	// ... CRUD / 权限 / 分页 / 输入限制断言
}
```

---

## 3. 直接 `go test` 会失败——用 `task test`

`Taskfile.yml`：

```yaml
version: '3'
dotenv: ['.env']        # 所有 task 自动加载根目录 .env

test:
  dir: '{{.USER_WORKING_DIR}}'
  cmds:
    - go test -v ./... -cover
```

- **必须用 `task test`（或 `task dev-server` 等任意 task）而非直接 `go test ./...`**：集成测试需要
  的环境变量由 `.env` 提供，Task 的 `dotenv` 指令负责加载；
- 直接 `go test ./...` 时，集成测试会以
  `TORCHWOOD_TEST_DATABASE_SOURCE is not set (run via `task test`, which loads .env, or export it manually)`
  fatal 退出；
- 手工方式：先 `export TORCHWOOD_TEST_DATABASE_SOURCE=... TORCHWOOD_TEST_ADMIN_DATABASE_SOURCE=...`
  再 `go test ./...`；
- 前置条件：本地 Postgres 已启动（`task up`，来自 `docker/local/docker-compose.yml`）。

---

## 4. CI 流水线（`.github/workflows/ci.yml`）

**触发时机**：push 到 `main` + 所有 pull request；`concurrency.cancel-in-progress: true`（同分支新提交
取消旧运行）。

### 4.1 backend job（lint + test + build）

- 环境：`ubuntu-latest`；services 起 `postgres:18-alpine`（`torchwood:torchwood`，暴露 5432）；
- 环境变量：

```yaml
TORCHWOOD_TEST_DATABASE_SOURCE: postgres://torchwood:torchwood@localhost:5432/TORCHWOOD_test?sslmode=disable
TORCHWOOD_TEST_ADMIN_DATABASE_SOURCE: postgres://torchwood:torchwood@localhost:5432/postgres?sslmode=disable
TORCHWOOD_RUN_DOCKER_TESTS: "1"
```

- 步骤顺序：
  1. checkout（actions/checkout@v4）；
  2. setup-go（`go-version-file: go.mod`，带缓存）；
  3. arduino/setup-task@v2（version 3.x）；
  4. 预拉函数运行时镜像（`node:18-alpine`、`python:3.11-alpine`，供 Functions 集成测试用）；
  5. 格式检查：`test -z "$(gofmt -l .)"`；
  6. 静态检查：`go vet ./...`；
  7. 测试：`go test ./...`（单元 + 集成，含 documentdb/app 集成测试）；
  8. 构建：`task build`（先 console-build 再 go build，验证 embed 链路）。

### 4.2 frontend job（lint + build）

- `working-directory: console`；pnpm 11.20.0（actions/pnpm 显式指定），node 22；
- 步骤：`pnpm install --frozen-lockfile` → `pnpm lint`（eslint）→ `pnpm build`（`tsc -b && vite build`）。

---

## 5. Lint

| 命令 | 内容 |
|------|------|
| `task lint-go` | `go vet ./...` + `test -z "$(gofmt -l .)"`（gofmt 必须零差异，Windows 下 Taskfile 用 `-l .`） |
| `task lint-console` | `pnpm lint`（eslint，`console/eslint.config.js`） |
| `task lint` | 依次执行 lint-go + lint-console |

提交前自检：`task lint && task test`（或至少 `task lint` + `go build ./...`）。

---

## 6. 质量观测

### 6.1 健康检查（`internal/infra/health/checks.go`）

- `DependencyChecker`（Name/Timeout/Check，实现 `lynx.Checker`；`CheckHealth()` 内部自带超时，
  默认 `DefaultTimeout = 2s`）；
- `NewCheckers(db, rdb, obj)` 注册三个依赖：postgres（`db.PingContext`）、redis（`rdb.Ping`）、
  minio（`obj.Ping`，`ObjectStore` 端口方法）；
- `Details(ctx)` 并行探测（goroutine + WaitGroup，panic recover 兜底为 unavailable），
  失败不影响其他依赖；
- 端点：
  - `GET /v1/health`（别名 `/v1/server/health`，`Check` RPC，ACCESS_PUBLIC）：返回
    `{status: ok|unavailable, dependencies: [{name, status, error?}]}`，gRPC 返回码保持 200；
  - `GET /healthz/liveness`：恒 200；`GET /healthz/readiness`：任一依赖失败 503
    （`lynxhttp.WithHealthCheckers` 驱动，gRPC 与 gateway 双端注册）；
  - `GET /v1/server/health/version`（`GetVersion` RPC）：返回 `{version, commit, date}`。

### 6.2 版本信息

- `cmd/server/main.go`：`var version, commit, date string`（全小写），由 Taskfile build 的 ldflags
  注入（`-X main.version={{.VERSION}} -X main.commit={{.COMMIT}} -X main.date={{.DATE}}`，
  `VERSION` 来自 `git describe --tags --always`）；
- 类型 `buildinfo.BuildInfo` 位于 `internal/pkg/buildinfo/`，经 `cmd/server/provides.go` 的
  `NewBuildInfo()` 注入 `servergrpc.NewHealthService`。

### 6.3 结构化日志

- 全链路 `*slog.Logger`（lynx + zap 后端），由 `cmd/server/provides.go` 的 `NewLogger` 暴露；
- gateway 请求日志：`lynxhttp.WithRequestLog(true)` + `lynxhttp.WithLogger(...)`
  （`internal/infra/server/grpc_gateway.go`），**级别为 Debug**，需 `--log-level debug` 可见；
- 认证拒绝日志：`pkg/grpc/interceptor/jwt.go` 的 `logAuthFailure` 输出 Warn（方法名/拒绝原因/凭证
  类型/IP/UA，**不记录 token**）。

### 6.4 慢查询日志（`internal/infra/clients/dbhook.go`）

- `SlowQueryHook` 实现 `bun.QueryHook`，挂在 `newDatabase`（`database.go`）内、`NewDataClients` 链上；
- 语义：阈值空字符串 → 默认 `500ms`（`DefaultSlowQueryThreshold`）；`"0"` → 禁用；解析失败 → Warn 并禁用；
- `data.database.debug=true` → `LogAll`（全量 SQL Debug 日志）；默认仅超阈值输出 Warn：

```text
slow query  operation=SELECT query=... duration=812ms error=
```

- 配置键：`data.database.slow_query_threshold`，环境变量
  `TORCHWOOD_DATA_DATABASE_SLOW_QUERY_THRESHOLD`；
- 注意：`e.Query` 是含内联参数的格式化 SQL（可能含 PII），文档注明；`testutil.SetupTestDB`
  不经过 `NewDataClients`，不受 hook 影响。

### 6.5 验证清单

```bash
task up            # 启动本地 Postgres/Redis/MinIO
task test          # 单元 + 集成（自动加载 .env）
task lint          # go vet + gofmt + eslint
task build         # console-build + go build（ldflags 注入版本）
```

构建后手动验证：`GET /v1/health`、`GET /v1/server/health/version`、`GET /healthz/readiness`
（停掉 MinIO 后应 503）。
