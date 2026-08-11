# 执行者 Prompt：sdk/go 重构（Server/Client API Client 拆分 + CLI 切换）

> 本文件是交给实现 agent 的完整任务书。实现 agent 可使用子代理并行，但必须**先复核、后实现**，完成后由发任务方严格审查。

---

你是资深 Go 工程师，负责在 `D:/Codes/qiulin/torchwood` 仓库实施 sdk/go 重构。你可以使用子代理并行执行相互独立的任务。

## 必读材料（先读完再动手）

1. **设计文档（spec）**：`docs/superpowers/specs/2026-08-11-sdk-go-restructure-design.md`
2. **实现计划（plan）**：`docs/superpowers/plans/2026-08-11-sdk-go-restructure.md` —— 包含 16 个 Task，每个 Task 有文件清单、接口约定、测试代码、实现代码、验证命令、提交命令

## 阶段 0：复核（必须先做，不写实现代码）

逐条核对计划与代码库现状是否一致，至少包括：

- `sdk/go` 现有文件（`torchwood.go`、`account.go`、`teams.go`、`databases.go`、`server.go` 及测试）是否仍存在、方法签名是否与计划 Task 4/8 的迁移来源一致
- `cmd/client` 各命令文件的 helper 用法（`jsonStringList`/`jsonInt64Map`/`structData`/`changedBoolPtr`/`changedInt32Ptr`/`changedStringPtr`/`buildListRequest`/`mergeData`）是否与计划 Task 11-14 的映射一致
- `proto/server/v1` 9 个 service 的方法清单、`proto/client/v1` AccountService 的 SignUp/SignIn/RefreshToken/Me/SignOut 与 `TokenBundle` 字段
- 根 `go.mod` 当前状态（是否已有 sdk/go 的 require/replace）
- **注意：工作区可能有并发提交**（本任务书生成时 main 上已有 console 相关提交），复核以当前 HEAD 为准

输出一份简短复核结论：逐项「一致 / 有出入（说明）」。若计划与现状有出入：
- 事实性错误（路径、方法名、字段名）→ 以代码现状为准修正计划相应段落，并在复核结论中记录
- 设计性分歧（不该改的地方）→ 停止并向发任务方澄清，不要自行变更设计

复核无误或修正完毕后，才进入实现。

## 阶段 1：实现（严格按 plan 的 Task 顺序与 TDD 步骤）

执行规则（全部硬性）：

- 按 Task 1 → 16 顺序执行；每个 Task 内按「写失败测试 → 确认失败 → 实现 → 确认通过 → commit」步骤走
- **git 纪律**：每个 commit 只 `git add` 该 Task 文件清单中列出的具体路径；**严禁** `git add -A` / `git add .`；工作区可能存在与本任务无关的在途改动，一概不碰、不提交、不还原
- commit message 直接用计划中给出的文案
- 不修改 `proto/`、`genproto/`、`internal/`、`cmd/server`、`cmd/worker`、`console/`
- 环境：Windows + Git Bash；sdk/go 测试在 `sdk/go` 目录运行（`cd sdk/go && go test ./...`），CLI 测试在仓库根运行（`go test ./cmd/client/...`）
- Task 10–14 中间状态 `cmd/client` 编译不过是**预期**（helpers 与部分命令未迁移完），不要为此写临时桩；编译验证集中在 Task 14 Step 4
- Task 10 需要先在 `sdk/go/server` 新增 `errors.go`（`IsPermissionDenied`），单独一个 SDK commit 后再改 CLI
- 可以用子代理并行：Task 12/13/14 的各命令文件迁移在 Task 11 helpers 落地后相互独立，适合并行；SDK 的 Task 4 与 Task 5 也可并行。但有依赖关系的 Task 必须串行
- 计划中的代码块是「最小可接受实现」：可以改进细节（如补 import、修正笔误），但不得改变已声明的接口签名与行为语义（Global Constraints + Interfaces 块）
- 每个 Task 结束时实际运行计划中给出的验证命令，输出必须真实看到 PASS；失败就修，不许跳过

## 阶段 2：交付报告

全部 Task 完成后，输出：

1. 复核结论（阶段 0 内容，含对计划的修正记录）
2. 变更摘要：每个 Task 一段——改了哪些文件、关键决策、与计划的偏差及理由
3. 验证证据：`cd sdk/go && go test ./... -cover`、`go test ./cmd/client/... -cover`、`go build ./cmd/client/` 的真实输出（尾部即可，但必须含 PASS/ok 行与覆盖率数字）
4. commit 清单：`git log --oneline` 中属于本任务的 commit
5. 遗留问题：任何未解决项、跳过的可选项（如 Task 15 Step 3 手工冒烟若无本地服务端可跳过并注明）

## 关键设计约束（摘自 spec，违反即为审查不通过）

- SDK 不封装自定义请求/响应类型，直接用 genproto 类型
- `InvokeJSON` 用 protoregistry + dynamicpb 动态分发，仅允许 `torchwood.server.v1.*` 且排除 `APIKeysService`；响应编码 `protojson.MarshalOptions{Multiline: true, Indent: "  "}`
- client 包自动刷新：提前 30s 主动刷新 + 401 刷新重试一次 + mutex 去重 double-check；刷新失败仅 `Unauthenticated` 才清 token；SignOut 不刷新，成功或 Unauthenticated 都清本地 token；SignIn/SignUp 仅在非 MFA 且 access_token 非空时落 token
- CLI 源码不得 import `genproto` / `google.golang.org/grpc` / `google.golang.org/protobuf`（import_guard_test 兜底）；JSON 解码一律 `UseNumber()`；`--code` 文件 flag 保留读文件体验（base64 入 map）
- 根 `go.mod` 新增 sdk/go 的 require + `replace => ./sdk/go`；sdk/go 自身 go.mod 不动
