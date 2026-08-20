# 实施 Prompt：重写 README 与开发者文档（docs/developer）

> 将本文件整体作为任务说明分派给实施 agent。仓库路径：`D:/Codes/qiulin/torchwood`
> 唯一事实来源是**当前代码**（proto、go.mod、Taskfile.yml、configs、internal/、cmd/、sdk/、.github/workflows），不是旧文档。
> 本任务只改文档，**不改任何代码、proto、配置、生成文件**。

---

## 任务目标

以代码为准重写以下文档，使其准确反映仓库现状：

1. `README.md`（英文）与 `README_ZH.md`（简体中文），两者内容必须一一对应；
2. `docs/developer/README.md`（章节索引）及 `01-overview.md` ~ `13-operations.md` 共 13 章（简体中文）。

「重写」指逐节核对事实并重写失准内容；基本准确的章节（见下文清单）可做轻量润色，不要为改而改。

## 关键现状（已调研核实，实施时仍须复核）

2026-08-12 对全部目标文档做过一次文档 vs 代码审计，失准清单如下（按严重程度排序）。重写时必须逐条落实修正，并以代码再验证：

### 严重失准

- **`README_ZH.md` 功能列表大幅落后**（README.md 的 Features 节基本准确，以其为蓝本对齐中文版）：
  - 用户认证：实际支持 Email/Phone OTP、OAuth2（Google/GitHub/WeChat）、匿名会话、Magic URL、一次性 JWT、TOTP MFA、邮箱变更两阶段确认（`ConfirmEmailChange`，public RPC，改邮箱先写 `pending_email` 再邮件确认）。证据：`proto/client/v1/account.proto`、`internal/app/client/account.go`。
  - 文件存储：实际还有预览缩略图、公开 bucket、HMAC File Token、分片上传/断点续传。证据：`proto/server/v1/storage.proto`、`docs/developer/07-storage.md`。
  - 函数执行：实际还有执行历史（Executions）、保留策略、同步/异步执行。证据：`proto/server/v1/functions.proto`、`internal/app/functions/executions.go`。
  - Server API：实际还覆盖 Groups、Functions、OAuth Providers、Health/Version。证据：`proto/server/v1/*.proto`。
- **首次引导（bootstrap）必须配置 `security.setup_token`**：旧文档（README 两个版本、02-quickstart、13-operations）均未提及；`Setup.SignUp` 在 token 为空时直接拒绝。证据：`internal/app/console/setup.go:122-124`、`.env.example`、`internal/pkg/config/config.proto`。Quick Start、bootstrap 说明、生产必配项表都要补上 `TORCHWOOD_SECURITY_SETUP_TOKEN`。
- **CLI 登记流程描述已过时**：02-quickstart 与 09-api-guide 引用 `cmd/client/registry.go` / `registry_test.go`，并称"新增 Server API 方法需同步登记"——这两个文件不存在。现状：CLI 通过 `sdk/go/server` 的 `InvokeJSON` 动态分发，`cmd/client/import_guard_test.go` 禁止 CLI 直接 import genproto/grpc，**新增 RPC 无需在 CLI 登记**。证据：`cmd/client/cmd/rpc.go`、`cmd/client/import_guard_test.go`、`AGENTS.md`。
- **03-configuration.md**：
  - `security` 配置遗漏 `setup_token` 与 `sessions.max_per_user`（`internal/pkg/config/config.proto:56,65`）；
  - `data.database.debug` 默认值说反了，模板里是 `false`（`configs/config.yaml.template:40`）；
  - 环境变量映射表缺 `TORCHWOOD_SECURITY_SETUP_TOKEN`、`TORCHWOOD_SECURITY_SESSIONS_MAX_PER_USER`；
  - 启动校验行号引用过时：JWT secret 校验在 `cmd/server/provides.go:50`，worker 的 `data.database.source` 校验在 `cmd/worker/provides.go:46`。

### 中轻度失准

- **README.md / 01-overview.md / 02-quickstart.md**：Go 版本写 1.25/1.26，实际 `go.mod` 要求 **Go 1.26.5**（`go.mod:3`）。
- **README.md**：`console-install`/`console-dev` 注释写的是 npm，实际用 **pnpm**（`console/package.json` 的 `packageManager` 与 Taskfile）；`task build` 实际编译 server + worker + client 三个产物（`bin/torchwood` 即 CLI），不只 server（`Taskfile.yml` build 任务）。
- **目录树遗漏**（README 两个版本、01-overview.md）：`cmd/client`、`sdk/go`、`pkg/secretbox`、`internal/app/shared`、`internal/domain/{audit,auth,idgen,messaging,groups,users}`、`internal/infra/{health,idgen,messaging,queue}`、`internal/pkg/{buildinfo,contexts,database}`。01-overview 还把 `cmd/client` 文件组成写成 `conn.go/registry.go`，实际入口是 `main.go` + `cmd/` 子包（`root.go`、`output.go`、`helpers.go`、`rpc.go`）。
- **SDK 章节遗漏 Go SDK**（README 两个版本）：仓库同时有 `sdk/typescript` 与 `sdk/go` 两个官方 SDK（`sdk/README.md`、`sdk/go/go.mod`）。
- **08-functions.md**：`CreateExecution` 的 `data` 上限是 **32 KB**（`maxExecutionDataBytes = 32 << 10`），不是 64 KB；"已知边界"已过时——worker 对瞬时失败会重抛回队，最多 `maxProcessAttempts = 3` 次，重试计数持久化在队列 payload（`cmd/worker/worker.go`）。
- **09-api-guide.md**：错误映射表中 `ResourceExhausted` 实际映射为 `ERROR_CODE_QUOTA_EXCEEDED` + HTTP 429，不是 INTERNAL_ERROR（`internal/infra/server/errors.go:40-41`）。
- **10-console.md**：`task build` 产物描述遗漏 `cmd/client`。
- **11-testing.md**：CI backend job 步骤不完整，实际还有 `Buf lint`、`Prepare console embed stub`、`SDK Go tests`、`TS SDK test`、`SDK demo build` 等（`.github/workflows/ci.yml`）。
- **基本准确、只需轻润色**：`04-codegen.md`、`06-databases.md`、`07-storage.md`、`12-sdk.md`、`docs/developer/README.md`。

### 其他背景

- 近期 API 变更（确认文档已反映）：client documents/count 迁移为自定义动词 `:count`；保留字面路由迁移为自定义动词；functions 写方法允许 API Key（G12）；lynx 升级 v1.3.0。09-api-guide 的自定义动词约定部分审计认为与代码一致，重写时保持。
- 文档中的 `file:line` 引用极易腐烂：能避免的改成"文件 + 符号名"，必须保留的逐一核对。
- 文档标注"最新更新"日期的，重写后更新为实际完成日期。

## 写作规范

- **语言**：`README.md` 用英文；`README_ZH.md` 与 `docs/developer/**` 用简体中文（遵循 `AGENTS.md`「对话和文档优先使用简体中文」）。两个 README 章节结构、事实内容保持一致，仅语言不同。
- **保持现有行尾风格**：`README.md`/`README_ZH.md` 目前是 CRLF，`docs/developer/**` 是 LF，不要全仓库混改行尾。
- **风格**：延续现有文档风格——每章开头一段定位说明（面向谁、讲什么），表格用于版本/端口/配置矩阵，代码块给出可直接复制的命令。技术术语、标识符、路径保留英文原文。
- **README 定位**：项目门面，面向第一次接触的人——产品定位（Appwrite-inspired、AI/Agent-Native BaaS）、Features、技术栈、Quick Start（task up → .env → migrate → generate → build → bootstrap）、常用 task、目录结构、架构要点（一段式）、测试、SDK（TS + Go）、文档索引。
- **docs/developer 定位**：面向贡献者的深度文档，章节划分维持现有 01–13 结构（总览/快速开始/配置/代码生成/认证/数据库/存储/函数/API 指南/Console/测试/SDK/运维），索引表与推荐阅读路径同步更新。
- 所有命令必须存在于 `Taskfile.yml`；所有版本号以 `go.mod`、`console/package.json`、`docker/local/docker-compose.yml` 为准；所有端口/配置项以 `internal/pkg/config/config.proto` 与 `configs/config.yaml.template` 为准。

## 约束

- 只改上述文档文件；不改代码、proto、配置、`genproto/**`、`AGENTS.md`、`docs/roadmap.md`、`docs/implementation-*.md`、`docs/archived/**`。
- 不新增章节文件，不改变 01–13 编号体系。
- 每一处事实性陈述（版本、端口、命令、默认值、上限、文件路径）都要能在代码中找到出处；找不到的删掉或改写为不带具体值的描述。
- 最小改动原则不适用于"准确性"：宁可整节重写，也不要留下半新半旧的混合描述。

## 完成验收

- 上文"关键现状"清单中的每一条都在新文档中得到修正。
- 抽查核对：
  - 文档出现的每个 `task xxx` 命令在 `Taskfile.yml` 中存在；
  - 每个环境变量在 `.env.example` 或 `internal/pkg/config/bind.go` 中有对应；
  - 每个目录树条目在文件系统中存在；
  - `README.md` 与 `README_ZH.md` 逐节对应，无事实性出入。
- 文档内相对链接（如 `docs/developer/README.md`、`sdk/README.md`）目标存在。
- 完成后输出：修改文件清单、每处重大改动的依据（代码路径）、复核中发现的本清单之外的新偏差点、遗留问题。

## 分派建议（给调度者）

可按文档拆分并行实施，互不冲突：

- Agent A：`README.md` + `README_ZH.md`
- Agent B：`01-overview.md` + `02-quickstart.md` + `03-configuration.md`
- Agent C：`08-functions.md` + `09-api-guide.md` + `11-testing.md`
- Agent D：`10-console.md` + `13-operations.md` + `docs/developer/README.md`（索引与阅读路径需等其他章节完成后定稿，建议最后执行或由调度者收尾）
- `04/06/07/12` 基本准确，可不做或仅润色。
