# 验收 Prompt：Torchwood CLI（cmd/client）框架 + 核心命令（严格验收）

> 将本文件整体作为验收任务分派给验收 agent。仓库路径：`D:/Codes/qiulin/torchwood`
> 完整设计文档：`docs/implementation-bootstrap-and-cli.md` §4、§6（**先通读再动手**，以代码现状为准）
> 验收对象：`docs/prompts/cli-framework.md` 声称已完成的实现（即当前工作区状态）

---

## 任务目标

对「cmd/client（二进制 `torchwood`）：CLI 框架 + health/projects/users 具名命令 + rpc 逃生舱」实现做**严格验收**：
逐项核对代码与设计文档 §4 的一致性，并实测全部验收项。

- **只读验收**：不得修改任何代码/文档文件（临时验证数据库除外）；发现偏差只报告，不修复。
- **证据要求**：每项结论必须附带可核查证据 —— 代码位置（`文件:行号`）、命令输出摘要、
  或可复现的 gRPC 请求与响应。禁止仅凭「读起来没问题」下结论。
- **严格程度**：存在设计文档要求但实现缺失/偏差即判「失败」；实现超出设计但行为等价的可判「通过」并注明。

## 验收前准备

```bash
task up            # Postgres / Redis / MinIO（本机引擎可能曾被并行任务重建，先确认三个 torchwood-* 容器 healthy）
task migrate       # 确保 schema 就位（migrate 输出 "no change" 也算就位，psql 核对 projects/api_keys 表存在即可）
task build         # 产出 bin/torchwood[.exe]；Windows 下必须是 torchwood.exe（无扩展名文件无法执行，判失败）
task dev-server    # 前台常驻正常（gRPC 127.0.0.1:9060 / HTTP :9080 / Metrics :9040）
```

**API Key 获取（二选一）**：
- 方案 A：Console 引导产出的默认 API Key secret（scope=`all`）；
- 方案 B：向本地库插入已知 secret 的测试 key（验收后删除）。哈希算法为 `sha256(secret)` 的 hex
  （`internal/infra/auth/validator.go:94`），表 `api_keys(project_id, name, secret_hash, scopes)`，
  project_id 取 `projects` 表中现有行（如 `default`）：
  ```bash
  # secret=cli-verify-full，scope=*
  docker exec torchwood-postgres psql -U torchwood -d torchwood -c \
    "INSERT INTO api_keys (id, project_id, name, secret_hash, scopes, enabled)
     VALUES ('cli-verify-full','default','verify','<sha256 hex>', ARRAY['*'], true);"
  ```

## 一、代码层面核对

### B1. CLI 框架（`cmd/client/main.go`、`conn.go`、`output.go`）

| 项 | 期望 |
|----|------|
| 全局 flag | `--endpoint`/`TORCHWOOD_CLI_ENDPOINT`（默认 `127.0.0.1:9060`）、`--api-key`/`TORCHWOOD_CLI_API_KEY`（默认空）、`--timeout`/`TORCHWOOD_CLI_TIMEOUT`（默认 `30s`）、`--output`/`TORCHWOOD_CLI_OUTPUT`（默认 `json`）；`--tls` 占位。env 提供默认值、flag 覆盖 env |
| api-key 必填校验 | 非豁免命令缺 key → 清晰错误（提示 `--api-key` 或 `TORCHWOOD_CLI_API_KEY`）；**仅 health 命令豁免**（检查豁免实现是注解 + 父链回溯，health 的两个子命令均生效） |
| `conn.go` | insecure 拨号（`insecure.NewCredentials()`，无 TLS）；unary client interceptor 向 outgoing metadata 写 `x-api-key`；**代码中不得出现 `X-Torchwood-Project` 写入**（该 header 仅对 admin console session 有效）；`--tls=true` → 返回「尚未支持」错误 |
| `output.go` | 成功响应用 `protojson.MarshalOptions{Multiline: true, Indent: "  "}` 输出到 **stdout**；gRPC status 错误打印 `code + message` 到 **stderr**、退出码非 0；`PermissionDenied` 附加 scope 提示（含 `users.read/users.write` 或 `* / all` 字样）；成功退出码 0 |
| 依赖 | `go.mod` 仅新增 `github.com/spf13/cobra`（+ 其 indirect `mousetrap`），不得有其他新依赖；cmd/client **不使用 Wire**（无 wire 文件） |
| 结构 | 按设计文档 §4.2 分文件：`main.go`/`conn.go`/`output.go`/`cmd_health.go`/`cmd_projects.go`/`cmd_users.go`/`cmd_rpc.go`（+`registry.go` 注册表） |

### B2. rpc 注册表（`cmd/client/registry.go`）

- 键使用**生成的** `XxxService_Method_FullMethodName` 常量，不得手写方法名字符串；
- 覆盖 `proto/server/v1` 全部方法（按描述符统计应含 APIKeys 共 84 个 RPC，**除 APIKeysService 4 个**，注册表应有 **80 条**）；
- `registry_test.go` 的完整性测试必须：遍历 `protoregistry.GlobalFiles` 中 `server/v1/` 全部文件的服务/方法（**排除 APIKeysService**）逐一比对注册表；核对每个条目的请求类型与方法描述符输入消息 **FullName 一致**；反向核对注册表无多余条目；
- 新注册的请求类型可被 `--data` 反序列化（protojson）+ 发起 unary 调用（构造器每次返回新实例，无共享可变状态）。

### B3. 具名命令

- `health`：`get`（Check）、`version`（GetVersion）；两子命令无 api-key 要求；
- `projects`：**只提供 `list [--page-size] [--page-token]` 与 `get <id>`**；代码中不得存在 `projects create/update` 命令（CreateProject/UpdateProject 限平台 admin，设计边界）；
- `users`：**覆盖 UsersService 全部 9 个方法**（list/get/create/update/update-password/delete、sessions list/delete、tokens create），子命令命名与 proto 方法对应；create 必填校验（--email/--password）；update 的 `--email-verified` 用 proto3 optional presence（未显式传 flag 不得设置字段）；`--data` JSON 合并（labels/prefs 等 Struct），冲突时 **`--data` 优先**、未知字段报错（检查合并实现：protojson 会重置目标消息，须解析到新消息后 `proto.Merge`，直接 Unmarshal 到已填 flag 的消息上判失败）；
- 全仓不得出现 `api-keys` 相关命令。

### B4. 构建接入（`Taskfile.yml`）

- `build` 任务包含 `-o ./bin/torchwood{{if eq .OS "Windows_NT"}}.exe{{end}} ./cmd/client`（或等价：Windows 产出 `torchwood.exe`、类 Unix 产出 `torchwood`），ldflags 变量与 server/worker 一致（`main.version/commit/date`）。

### B5. 文档

- `AGENTS.md` 项目结构含 cmd/client 一行（提及注册表维护与 `go test ./cmd/client/...` 完整性校验）；
- `docs/developer/01-overview.md`：目录树 cmd/ 下有 client/、后端技术栈表含 cobra、运行时入口段落提及 CLI；
- `docs/developer/02-quickstart.md` §7 与 `09-api-guide.md` §12：含 CLI 用法示例与全局参数表；09 的 §12 含注册表维护要点（新增 Server API 方法需登记注册表）。

### B6. 测试存在性（`cmd/client/*_test.go`）

- 注册表完整性测试（B2 所述，**必须真实遍历描述符**，不得硬编码方法列表）；存在性测试对 `rpcRegistry` 的每一条构造器可重复调用；
- flag → request 构造 table-driven：create（必填校验/全字段/--data 合并/非法 JSON/未知字段）、update（optional bool presence、未传 flag 不设字段）、list 分页参数；
- 错误路径：缺 api-key（豁免/非豁免）、`PermissionDenied` 格式化含 scope 提示、`--tls` 未支持、未知方法报错、`--output`/`--timeout` 校验；
- 以上测试 `go test ./cmd/client/...` 全部通过。

## 二、行为验证（实测）

前置：dev server 运行中；`bin/torchwood.exe` 已构建（Windows）或 `bin/torchwood`（类 Unix）。以下 `torchwood` 指该二进制。

1. **命令树**：`torchwood --help` 输出含 `health`/`projects`/`users`/`rpc` 四个子命令与全部全局 flag；`torchwood users --help` 列出 8 个直接子命令（sessions/tokens 为分组）。
2. **health 免 key**：`torchwood health get`（不带任何 key 参数）→ 退出码 0，stdout 为缩进 JSON（`status`/`dependencies`），**stderr 为空**。
3. **health version**：`torchwood health version` → 退出码 0（dev 构建可能输出 `{}` 或空字段，属正常，注明即可）。
4. **users list 有效 key**：`torchwood users list --api-key <有效secret>` → 退出码 0、stdout JSON 含 `users` 与 `meta`。
5. **无效 key**：`torchwood users list --api-key wrong` → 退出码 1，stderr 含 `Unauthenticated` 与错误信息，**stdout 为空**。
6. **缺 key**：`torchwood users list`（无 --api-key 且无环境变量）→ 退出码 1，stderr 含「缺少 API key」提示；`torchwood health get` 同样条件不受影响（对照 2）。
7. **scope 不足**：插入 scope=`users.read` 的测试 key，`torchwood users create --email x@y.z --password pw --api-key <read-only>` → 退出码 1，stderr 含 `PermissionDenied` **且** 含 scope 提示（`users.read/users.write` 或 `* / all` 字样）。
8. **rpc 与具名命令一致**：`torchwood rpc /torchwood.server.v1.UsersService/ListUsers --data '{}' --api-key <key>` 与 `torchwood users list --api-key <key>` 的 stdout **逐字节一致**。
9. **rpc --data 透传**：`rpc /torchwood.server.v1.UsersService/ListUsers --data '{"pageSize": 1}'` 响应 `meta.pageSize == 1`。
10. **rpc 未知方法**：`torchwood rpc /torchwood.server.v1.UsersService/Bogus --api-key <key>` → 退出码 1，stderr 含「未知方法」与完整方法名示例。
11. **projects**：`torchwood projects get default --api-key <key>` → 退出码 0 返回项目 JSON；`torchwood projects list --api-key <key>` → 退出码 0（**列表为空是服务端安全设计 M7**：非平台 admin 列表恒空，`internal/app/server/projects.go:114`，判通过并注明，不得视为 CLI 缺陷）。
12. **users create/update 全链路**：create 带 `--data '{"labels":{"group":"core"}}'` → 响应含新用户；update `--status inactive` → 响应 `status == "inactive"`；delete → 成功。完成后清理测试用户。
13. **环境变量**：仅设 `TORCHWOOD_CLI_API_KEY=<key>`（不传 flag）跑 `users list` → 成功；`TORCHWOOD_CLI_ENDPOINT` 指向错误端口 → 退出码 1 且错误信息可读（Unavailable）。
14. **占位参数**：`torchwood health get --tls` → 退出码 1、stderr 含「尚未支持」；`torchwood health get --output yaml` → 退出码 1、stderr 含「不支持的输出格式」。
15. **rpc 注册表抽查**：任选 Databases/Groups/Storage 服务的一个读方法与一个写方法（如 `ListGroups`、`CreateGroup`）走 `rpc` 调用，读方法返回 200 语义、写方法按 scope 返回对应结果（证明注册表非仅 health/users/projects 三服务）。

## 三、工程化

- `task test` 全部通过（含 cmd/client 与全仓，非 `-short`）；
- `task build` 通过且产出 `bin/torchwood[.exe]`；
- `gofmt -l cmd/client` 无输出；`go vet ./...` 通过。

## 四、输出要求

验收报告（markdown），包含：

1. **逐项结论表**：`# | 验收项 | 结论（通过/失败/警告） | 证据（文件:行号 或 命令输出摘要）`，覆盖 B1–B6、行为验证 1–15、工程化 3 项。
2. **失败项清单**：按严重程度排序（阻断/非阻断），每条附：期望 vs 实际、复现步骤、最小修复建议（建议即可，不修改代码）。
3. **与设计文档的偏差清单**：列出所有实现与设计文档的差异，并逐条标注「设计接受」（如 `projects list` 对 API key 为空列表属服务端 M7 设计；dev 构建 `health version` 空字段）或「需修复」。
4. **最终结论**：整体是否通过验收；若不通过，列出必须修复项（按优先级）。
