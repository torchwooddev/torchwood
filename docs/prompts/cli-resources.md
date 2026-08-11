# 实施 Prompt C：Torchwood CLI 资源命令补全（databases/teams/storage/functions/oauth-providers）

> 将本文件整体作为任务说明分派给实施 agent。仓库路径：`D:/Codes/qiulin/torchwood`
> 完整设计文档：`docs/implementation-bootstrap-and-cli.md` §4.4、§6
> **前置依赖**：Prompt B（`docs/prompts/cli-framework.md`）已完成——`cmd/client` 框架、`rpc` 注册表、`health/projects/users` 命令已就绪。先阅读 Prompt B 完成时留下的模式说明与 `cmd/client/` 现有代码，严格沿用其模式。

---

## 任务目标

在 Prompt B 搭建的 CLI 框架上，补齐其余 Server API 资源的具名子命令，使命令树覆盖设计文档 §4.4（除明确排除项）。

## 覆盖范围（以 `proto/server/v1` 实际方法为准）

| 服务 | 方法数 | CLI 命令 |
|------|--------|----------|
| `DatabasesService` | 21 | `torchwood databases ...`、`databases collections ...`、`databases attributes/indexes ...`、`databases documents ...`（按 proto 语义分组，层级过深时允许扁平化并在 help 中注明） |
| `TeamsService` | 12 | `torchwood teams ...`、`teams memberships ...` |
| `StorageService` | 12 | `torchwood storage buckets ...`、`storage files list/get/update/delete`、`storage usage`；**不做**文件上传/下载（独立 HTTP handler，非 gRPC）与分片上传会话 |
| `FunctionsService` | 16 | `torchwood functions ...`、`functions deployments list/get/delete`、`functions variables ...`、`functions executions list/get/create`；**不做** deployment 上传（如上传走 gRPC 纯消息则可做，以 proto 为准） |
| `OAuthProvidersService` | 3 | `torchwood oauth-providers list/get/update` |

排除项（安全设计边界，与 Prompt B 一致）：`APIKeysService` 全部、`CreateProject`/`UpdateProject`。

## 参数与输出约定（沿用 Prompt B 模式）

- 标量参数用具名 flag；复杂结构（document `data`、查询 DSL `queries` 数组、permissions 数组等）接受 JSON 字符串 flag（如 `--data`、`--queries`），用 `protojson.Unmarshal` 合并进请求 message。
- 列表类命令统一支持 `--page-size`、`--page-token`（以各 proto 实际字段为准）。
- 输出、错误处理、退出码沿用 `cmd/client/output.go`。
- 新增方法必须同步 `rpc` 注册表（若 Prompt B 注册表已全量覆盖则仅需核对），注册表完整性测试保持通过。

## 实施步骤

1. 通读 `cmd/client/` 现有代码与 `proto/server/v1` 中上述 5 个 proto 文件。
2. 按资源一个文件（`cmd_databases.go`、`cmd_teams.go`、`cmd_storage.go`、`cmd_functions.go`、`cmd_oauth.go`）实现子命令，接入 root 命令树。
3. 为每个资源的「flag → request message」构造函数补 table-driven 单测；覆盖至少一条成功路径与一条参数校验失败路径。
4. 更新 `--help` 与 `docs/developer/` 中 CLI 用法小节（补齐命令树示例）。
5. `task build && task test` 通过。

## 约束

- 不改服务端代码与 proto；不引入新依赖。
- 命令命名、flag 风格、错误提示与 Prompt B 已建立的保持一致。
- 分片上传/文件传输相关方法一律不做，help 中也不出现。
- 最小改动：不重构 Prompt B 已有代码，发现其缺陷时在完成说明中记录而非顺手大改（阻塞性 bug 除外）。

## 完成验收

- `torchwood --help` 命令树覆盖上表全部资源；每个服务至少一条命令在本地栈（`task up && task migrate && task dev-server` + 有效 API Key）实测成功，输出 JSON 与 gateway REST 调用结果一致。
- 无对应 scope 的 key 调用写命令返回 `PermissionDenied` 提示。
- 注册表完整性测试、`task test`、`task build` 全部通过。
- 完成后输出：变更文件清单、新增命令树快照、逐服务实测结果、遗留问题。
