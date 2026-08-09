# Torchwood 函数执行（Functions）

> 本文描述 Torchwood 的 Serverless 函数子系统：Docker 真实执行器（构建/运行）、异步 worker（cmd/worker）、函数/部署/变量/执行 CRUD 与生命周期。
> 相关代码：`internal/domain/functions/`、`internal/infra/functions/`、`internal/app/functions/`、`internal/infra/queue/`、`cmd/worker/`、`proto/server/v1/functions.proto`、`db/migrations/000010_functions.*.sql`。

---

## 1. 架构总览

```
gRPC FunctionsService ──→ app/functions (use-case)
                            ├─ FunctionRepo（bun 静态表：functions / function_deployments / function_variables / function_executions）
                            ├─ Executor（Docker：Build / Execute / RemoveImage）
                            └─ Queue（Redis List）──→ cmd/worker（BRPOP 消费）──→ app/functions.ProcessExecution
HTTP multipart (serverhttp.FunctionsHandler) ← deployment 代码包上传
```

- **执行器为真实 Docker**（`internal/infra/functions/docker.go`）：`Build` 将 zip 代码包解压校验后按运行时生成 Dockerfile 构建镜像；`Execute` 运行容器并收集 stdout/stderr；`RemoveImage` 幂等删除镜像。**非 stub**（git 提交 `bc170ad`「Implement functions executor: Docker build/run, async worker, CRUD, console UI」已交付；`docs/implementation-functions-executor.md` 头部「待实现」标注为旧态，以代码为准）。
- **持久化**：四张静态表（`000010_functions.up.sql`），bun + golang-migrate，与动态文档层无关。
- **MVP 部署假设**：单机部署——server 与 worker 共享文件系统，zip 代码包存本地 `os.TempDir()/torchwood-functions/<projectID>/<functionID>/<deploymentID>.zip`。

---

## 2. 函数生命周期

```
创建函数 → 上传代码包（zip）→ 同步构建镜像（deployment: pending → building → ready|failed）
        → 触发执行（同步 in-request / 异步入队）→ execution: queued → building? → running → completed|failed
        → 查看结果（stdout / stderr / response / status_code / duration_ms）
```

### 2.1 函数 CRUD（`/v1/server/functions`）

| 方法 | 路径 | 说明 |
|------|------|------|
| `ListRuntimes` | `GET /v1/server/functions/runtimes` | `node-18.0`（入口 `index.js` 的 `main`）、`python-3.11`（入口 `main.py` 的 `main`） |
| `ListSpecifications` | `GET /v1/server/functions/specifications` | `shared-1x`（0.5 CPU / 256MB）、`shared-2x`（1 CPU / 512MB） |
| `CreateFunction` | `POST /v1/server/functions` | 校验 runtime/spec 存在、`timeout_seconds` ∈ [1, 300]；缺省 spec `shared-1x`、entrypoint 按 runtime 缺省 |
| `ListFunctions` / `GetFunction` | `GET /v1/server/functions`、`GET /v1/server/functions/{function_id}` | |
| `UpdateFunction` | `PATCH /v1/server/functions/{function_id}` | name / entrypoint / timeout_seconds / spec / enabled |
| `DeleteFunction` | `DELETE /v1/server/functions/{function_id}` | DB 级联删除 → 逐 deployment `RemoveImage` + 删本地 zip（幂等，失败仅记日志） |

### 2.2 部署（Deployments）

| 方法 | 路径 | 说明 |
|------|------|------|
| `CreateDeployment` | `POST /v1/server/functions/{function_id}/deployments` | gRPC body 携带 `code`（zip 字节） |
| 上传代码包 | `POST /v1/server/functions/{functionId}/deployments/code` | **multipart**（`functions_handler.go`，字段 `code`，≤ 50 MiB） |
| `ListDeployments` / `GetDeployment` / `DeleteDeployment` | `GET .../deployments`、`GET .../deployments/{deployment_id}`、`DELETE ...` | |

构建流程（`app/functions/deployments.go`）：

1. 校验 zip 魔数（`PK\x03\x04`，空 zip 一并拒绝）与 50 MiB 上限；
2. 落库 `status=pending`，写本地 zip；
3. **同步构建**（MVP 定案：请求内完成，独立构建队列为 roadmap 偏离项）：占构建信号量（并发上限 4）→ `status=building` → `executor.Build` → `ready`（失败 → `failed` + `error`，并清理 zip 与残留镜像）。

### 2.3 执行（Executions）

| 方法 | 路径 | 说明 |
|------|------|------|
| `CreateExecution` | `POST /v1/server/functions/{function_id}/executions` | `async` 布尔；`data` ≤ 64 KB 且必须合法 JSON；`deployment_id` 缺省用最新 `ready` 部署 |
| `ListExecutions` | `GET /v1/server/functions/{function_id}/executions` | 保留策略：每函数最多最近 100 条（`PruneOldExecutions`） |
| `GetExecution` | `GET /v1/server/functions/{function_id}/executions/{execution_id}` | |

- **同步**：`async=false`（默认）。`timeout_seconds` 超过 30s（`maxSyncTimeoutSeconds`，grpc-gateway WriteTimeout 余量）拒绝同步执行；占用运行信号量（并发上限 16，超限 `ResourceExhausted`）后请求内 `executor.Execute` 并写回结果。
- **异步**：落库 `status=queued` → JSON payload（`execution_id` / `function_id` / `project_id` / `data`）入队 `torchwood:queue:functions-executions`（Redis List，LPUSH）→ worker 消费。
- **状态机**：`queued → building（deployment 非 ready 时补构建）→ running → completed | failed`；任何失败（超时 / 非零退出码 / 执行错误）写回 `failed` + `error`；记录级截断：stdout/stderr/response 各 ≤ 64 KB（`maxOutputBytes`，含截断标志位）。
- **执行安全基线**（docker.go `Execute`）：`CapDrop: ALL`、`no-new-privileges`、只读根文件系统 + `/tmp` tmpfs、内存/CPU 按 spec、pids 上限 512、网络缺省 `none`（配置 `functions.docker.network` 时自动创建/接入 bridge 网络）；数据经 `TW_DATA` 环境变量传入（禁止拼进命令）；超时（默认 15s）→ 停止并强制删除容器，无残留。

### 2.4 运行时 Dockerfile（`dockerfileFor`）

```dockerfile
# node-18.0
FROM node:18-alpine
WORKDIR /app
COPY . .
USER node
CMD ["node","-e","const {main}=require('./index');Promise.resolve(main(JSON.parse(process.env.TW_DATA||'{}'))).then(r=>console.log(JSON.stringify(r))).catch(e=>{console.error(e);process.exit(1)})"]
```

```dockerfile
# python-3.11
FROM python:3.11-alpine
WORKDIR /app
COPY . .
USER 1000
CMD ["python","-c","import json,os,main;r=main.main(json.loads(os.environ.get('TW_DATA','{}')));print(json.dumps(r))"]
```

镜像名：`<registry>/func-<functionID>-<deploymentID>`（registry 缺省 `torchwood-funcs`）；`executor.Build` 自动按代码包内容识别运行时（含 `index.js` → node，含 `main.py` → python，否则 `InvalidArgument`）。

### 2.5 代码包安全（zip 解压）

| 限制 | 值 |
|------|-----|
| 条目数 | ≤ 1000 |
| 单条解压后 | ≤ 100 MiB |
| 总解压量 | ≤ 200 MiB |
| 符号链接 | 一律拒绝 |
| 路径穿越 | `zip slip` 双重校验（Clean 后必须位于解压根目录内） |
| 构建日志 | 截断 64 KB |
| 容器输出 | 缓冲上限 1 MiB（结果在 app 层再截断 64 KB） |

---

## 3. 异步 worker（cmd/worker）

`cmd/worker` 是独立的 lynx 二进制，消费函数异步执行队列：

| 项 | 值 |
|----|-----|
| 队列 | `torchwood:queue:functions-executions`（`internal/domain/shared/ports.go`，Redis List：Enqueue = LPUSH，Dequeue = BRPOP） |
| 并发 | 4 个消费 goroutine（`workerConcurrency`），BRPOP 轮询超时 1s（配合优雅退出） |
| 启动对账 | `RecoverOrphanExecutions(1h)`：停留 `queued/building/running` 超过 1 小时的记录标记 `failed`（兜底 Redis 重启丢任务、worker 崩溃孤儿） |
| 消费逻辑 | 加载 execution/function/deployment/variables → deployment 非 `ready` 先补构建 → `running` → 执行（超时 = `timeout_seconds`）→ 写回结果；单任务失败不影响消费循环 |
| 优雅退出 | `Stop` 取消 BRPOP 上下文并等待 goroutine 收敛 |

### 3.1 worker 与 server 的 Wire 注入差异

| 维度 | `cmd/server/provides.go` | `cmd/worker/provides.go` |
|------|-------------------------|--------------------------|
| ProviderSet | `api.ProviderSet` + `app` + `infra` + `domain` | `app` + `infra` + `domain`（**无 api 层**） |
| 组件（lynx.Service） | gRPC server、GRPCGatewayServer、MetricsServer | 仅 `Worker` |
| 配置校验 | `security.jwt.secret` 必须设置（`TORCHWOOD_SECURITY_JWT_SECRET`） | `data.database.source` 必须设置（`TORCHWOOD_DATA_DATABASE_SOURCE`） |
| 版本信息 | `NewBuildInfo`（ldflags 注入） | 无（wire 剪枝自动省略） |

两者共用 `app.ProviderSet` / `infra.ProviderSet`，因此 **server 与 worker 都注入同一个 Docker executor 与 Redis queue**；server 负责入队，worker 负责消费。`task worker` 运行 `go run ./cmd/worker`，`task build` 同时产出 server 与 worker 二进制。

---

## 4. 配置项（`internal/pkg/config/config.proto`）

| 配置路径 | 环境变量 | 默认值 | 说明 |
|----------|----------|--------|------|
| `functions.executor` | — | `docker` | 执行器类型（当前仅 docker） |
| `functions.docker.host` | `TORCHWOOD_FUNCTIONS_DOCKER_HOST` | `unix:///var/run/docker.sock` | Docker daemon 地址（client 构造失败延迟到首次调用暴露） |
| `functions.docker.network` | `TORCHWOOD_FUNCTIONS_DOCKER_NETWORK` | 空（= `none`） | 容器 bridge 网络，不存在时执行器自动创建 |
| `functions.docker.registry` | `TORCHWOOD_FUNCTIONS_DOCKER_REGISTRY` | `torchwood-funcs` | 镜像 registry 前缀（必须小写） |

---

## 5. 变量与结果

- **Variables**：`PUT /v1/server/functions/{function_id}/variables` / `GET ...`（`SetVariables` 整体替换）。MVP **明文存储**（`function_variables.value`，无 secretbox 加密）；执行时合并总量 ≤ 32 KB（`maxEnvBytes`），键含 `\n\r\0` 的条目被丢弃（`sanitizeEnv`）。
- **返回值约定**：`parseResponse` 取 stdout 末行，若为合法 JSON 则作为 `response` 返回（`console.log(JSON.stringify(...))` 协议）；非零退出码 → `failed`（`error` 取 stderr）。
- **日志**：stdout/stderr 完整保存（64 KB 截断 + `*_truncated` 标志），Console Functions 页面可查看执行状态与日志。

---

## 6. 测试

- `internal/infra/functions/docker_integration_test.go`：Docker 构建/执行集成测试。
- `internal/app/functions/functions_test.go`、`executions_test.go`、`mocks_test.go`：use-case 单元测试（stubRepo + 信号量/超时/截断路径）。
- `internal/infra/queue/redis_queue_test.go`：队列适配器测试。

---

## 7. 已知边界

- 独立构建队列未实现：`CreateDeployment` 同步构建（roadmap §2.6 的偏离，worker 消费前对非 ready 部署补构建兜底）；
- 无任务重试 / 死信队列 / ack 语义：失败即 `failed`，worker 崩溃由启动对账兜底；
- 变量明文存储；entrypoint 字段 MVP 仅占位（执行入口固定为 `index.js` / `main.py` 的 `main`）；
- 单机文件系统假设：server 与 worker 必须共享 `os.TempDir()/torchwood-functions`（多机部署需对象存储承载代码包）。
