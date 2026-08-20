# Torchwood 代码生成与工具链

> 本文描述 Torchwood 的代码生成体系：Task 工作流执行器、Buf 驱动的 proto 生成（`proto/` → `genproto/`）、配置生成、Wire 依赖注入，以及推荐的生成流程顺序。
> 相关文件：`Taskfile.yml`、`buf.yaml`、`buf.gen.yaml`、`proto/`、`cmd/server/provides.go`、`cmd/server/wire.go`、`cmd/server/wire_gen.go`、`cmd/worker/`。

---

## 1. Task 工作流执行器

Torchwood 使用 **Task**（`Taskfile.yml`，version 3）作为主要工作流执行器。`Taskfile.yml` 顶部 `dotenv: ['.env']`，因此任务运行时自动把 `.env` 中的变量加载进环境（集成测试 DSN、迁移 DSN 等依赖此行为）。

### 1.1 常用任务总览

| 任务 | 命令内容 | 用途 |
|------|---------|------|
| `install-tools` | `go install` protoc-gen-go / migrate / buf / wire | 安装代码生成与迁移工具链 |
| `generate-proto` | `buf lint` + `buf generate` | Buf 生成 gRPC stub / gateway / Swagger（见 §2） |
| `generate-config` | `protoc` 在 `internal/pkg/config` 内生成 Go 代码（见 §3） |
| `wire-server` | 在 `cmd/server` 运行 `go run github.com/google/wire/cmd/wire` | 重新生成 server 的 `wire_gen.go` |
| `wire-worker` | 在 `cmd/worker` 运行 wire | 重新生成 worker 的 `wire_gen.go` |
| `wire-all` | `wire-server` + `wire-worker` | 两个入口的 Wire 生成 |
| `generate-all` | `generate-proto` → `generate-config` → `wire-all` | 一键全量生成（见 §5） |
| `up` / `down` / `clean` | `docker compose up -d` / `down` / `down -v` | 本地基础设施（Postgres/Redis/MinIO） |
| `migrate` | `migrate -path ./db/migrations -database <DSN> up` | 应用数据库迁移；DSN 来自 `TORCHWOOD_DATA_DATABASE_SOURCE` 或 `POSTGRES_*` 默认值 |
| `dev-server` | `go run ./cmd/server` | 本地起 server |
| `worker` | `go run ./cmd/worker` | 本地起 worker |
| `console-install` / `console-build` / `console-dev` | pnpm 命令 | Console 前端安装 / 构建 / 开发 |
| `sdk-install` / `sdk-build` / `sdk-demo` / `sdk-demo-build` | npm 命令 | TypeScript SDK 安装 / 构建 / demo |
| `build` | `console-build` + `go build -ldflags "..." ./cmd/server ./cmd/worker ./cmd/client` | 全量构建（注入 version/commit/date，产出 server / worker / CLI 三个二进制） |
| `test` | `test-sdk-go` + `test-sdk-ts` + `go test -v ./... -cover` | 全部测试（SDK 测试 + 仓库测试，自动加载 `.env`） |
| `lint-go` / `lint-sdk-go` / `lint-console` / `lint` | `go vet` + `gofmt -l`；sdk/go 同款检查；Console 侧 `pnpm lint` | 代码静态检查 |
| `build-docker` | `docker build -t torchwood:<ver>` | 构建 Docker 镜像 |

> 常用组合：开发前 `task up` + `task migrate`；改动 proto/config/provider 后 `task generate-all`；修改 Console 后需先 `task console-build` 再 `task build`（Go embed 只会打进已构建的 `dist/`）。

---

## 2. Buf 驱动的 proto 生成

### 2.1 Buf 配置

**`buf.yaml`**（Buf v2）：

```
version: v2
deps:
  - buf.build/googleapis/googleapis
  - buf.build/bufbuild/protovalidate
  - buf.build/grpc-ecosystem/grpc-gateway
modules:
  - path: proto
lint:
  use:
    - STANDARD
  except:                 # 与项目既有契约不符的规则显式豁免
    - PACKAGE_DIRECTORY_MATCH
    - RPC_REQUEST_RESPONSE_UNIQUE
    - RPC_REQUEST_STANDARD_NAME
    - RPC_RESPONSE_STANDARD_NAME
    - ENUM_VALUE_PREFIX
breaking:
  use:
    - FILE
```

- `modules.path: proto` 声明 `proto/` 为模块根；
- `deps` 引入 googleapis（`google.api.http`）、protovalidate、grpc-gateway 的公共依赖；
- `lint: STANDARD` 与 `breaking: FILE` 是 Buf 的 lint / breaking 规则集（`buf lint` / `buf breaking`）；
- 五项 lint 豁免均有理由注释（包名与目录有意分离、复用 `shared.v1.ListRequest`/`Empty`、
  `ACCESS_*` 枚举语义命名），改动前先读 `buf.yaml` 注释。

**`buf.gen.yaml`**（Buf v2，四个插件全部输出到 `genproto/`）：

| 插件 | 版本 | 输出 | 产物 |
|------|------|------|------|
| `protocolbuffers/go` | v1.36.10 | `genproto`（`paths=source_relative`） | `*.pb.go`（message / service 定义） |
| `grpc-ecosystem/gateway` | v2.27.4 | `genproto`（`paths=source_relative`） | `*.pb.gw.go`（grpc-gateway REST handler） |
| `grpc/go` | v1.6.0 | `genproto`（`paths=source_relative`） | `*_grpc.pb.go`（gRPC stub） |
| `grpc-ecosystem/openapiv2` | v2.27.3 | `genproto` | `*.swagger.json`（OpenAPI 规范） |

### 2.2 proto/ 四组 → genproto/ 输出

| 组 | 目录 | 用途 | genproto 输出目录 |
|----|------|------|-------------------|
| Client API | `proto/client/v1/` | 终端用户直接调用（Account、Databases、Groups 等） | `genproto/client/v1/` |
| Server API | `proto/server/v1/` | Agent / 自动化通过 scoped API Key 调用管理面（Projects、Users、Storage、Databases、Functions、APIKeys、Groups、Health、OAuthProviders） | `genproto/server/v1/` |
| Console API | `proto/console/v1/` | Admin Console 后台（ConsoleAuth、Admins） | `genproto/console/v1/` |
| Shared | `proto/shared/v1/` | 跨组共享：`authz.proto`（鉴权注解）、`common.proto`（列表/响应元数据） | `genproto/shared/v1/` |

每个 service proto 生成的产物示例：`<svc>.pb.go`、`<svc>_grpc.pb.go`、`<svc>.pb.gw.go`、`<svc>.swagger.json`。

> **禁止手工编辑 `genproto/` 中生成的 `*.pb.go`（以及 `*_grpc.pb.go`、`*.pb.gw.go`）**：所有改动必须回到 `proto/` 源文件并重新 `buf generate`。`generate-proto` 任务即执行 `buf generate`。

---

## 3. task generate-config

配置 schema（`internal/pkg/config/config.proto`）的 Go 代码**不经过 Buf**，而是由 `task generate-config` 直接用 `protoc` 在 `internal/pkg/config` 目录内生成：

```
protoc -I. --go_out=. --go_opt=paths=source_relative ./config.proto
```

- 工作目录固定在 `internal/pkg/config`；
- 产出 `internal/pkg/config/config.pb.go`；
- 生成代码仅含 message 结构与 getter（`GetSecurity()`、`GetJwt()` 等），供 `bind.go` 的反射（`collectKeys` / `collectLeaves`，依赖 json tag）与各层 `NewAppConfig` 校验使用。

---

## 4. Wire 依赖注入

### 4.1 三个文件的分工

| 文件 | 角色 |
|------|------|
| `cmd/server/provides.go` | 手写 provider 声明：`var ProviderSet = wire.NewSet(boot.New, api.ProviderSet, app.ProviderSet, infra.ProviderSet, domain.ProviderSet, NewLogger, NewComponents, ...)`，以及各构造器（`NewLogger`、`NewAppConfig`、`NewBuildInfo` 等） |
| `cmd/server/wire.go` | 生成入口：`//go:generate wire` + `wire.Build(ProviderSet, ...)` |
| `cmd/server/wire_gen.go` | **由 wire 生成的装配代码**，不要手工编辑 |

`cmd/worker/` 结构与 server 完全同构：`provides.go`、`wire.go`、`wire_gen.go`（worker 的 `NewAppConfig` 校验 `data.database.source`，`NewWorker` 装配后台任务）。

### 4.2 变更后必须 wire-all

**provider 变更后必须执行 `task wire-all`**（或分别 `wire-server` / `wire-worker`）。wire 依赖编译时类型推导，`wire_gen.go` 与 `provides.go` 不同步会导致启动失败——这与 `AGENTS.md` 的约定一致。

---

## 5. 生成流程顺序

`task generate-all` 的依赖顺序固定为：

```
generate-all
  ├─ generate-proto      # buf generate：proto/ → genproto/
  ├─ generate-config     # protoc：config.proto → config.pb.go
  └─ wire-all
       ├─ wire-server    # cmd/server → wire_gen.go
       └─ wire-worker    # cmd/worker → wire_gen.go
```

推荐在以下场景运行：

| 场景 | 命令 |
|------|------|
| 修改 `proto/**/*.proto` | `task generate-proto` |
| 修改 `internal/pkg/config/config.proto` | `task generate-config` |
| 修改任何 `provides.go` / provider 构造器签名 | `task wire-all` |
| 以上全部 / 首次拉取代码后 | `task generate-all` |

> 生成完成后应 `task build` 验证编译，原型变更过大时同时留意 `collectMethodsByAccess` 对新增 gRPC 方法的 authz 注解要求（见 `docs/developer/05-authentication.md` §5）。

---

## 6. 参考

- `Taskfile.yml`：全部任务定义与命令。
- `buf.yaml` / `buf.gen.yaml`：Buf 版本、模块、依赖与生成规则。
- `proto/client/v1/`、`proto/server/v1/`、`proto/console/v1/`、`proto/shared/v1/`：API 单一事实来源。
- `genproto/`：生成的 Go 代码与 Swagger（禁止手工编辑）。
- `cmd/server/provides.go` / `cmd/server/wire.go` / `cmd/server/wire_gen.go`、`cmd/worker/`：Wire 装配。
- `internal/pkg/config/config.proto`：配置 schema（另见 `docs/developer/03-configuration.md`）。
- `AGENTS.md`：生成任务与编辑约定。
