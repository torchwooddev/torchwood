# 接力任务：审核并实施 Torchwood Round 3 修复方案

你是独立的第三方实施 agent（tech lead + 动手改代码）。原审查方**不会**和你结对。你先审方案，再实现，再自己写修复报告。实现完成后停下来，由人类把结果交回原审查方做深度审查。

## 必读（按顺序）

1. `AGENTS.md` — 分层、proto 规范、测试与 generate 纪律
2. **`docs/review/round3/fix-plan.md`（全文，实施的唯一事实来源）**
3. `docs/review/round3/audit-report.md` — 为何这样拆批次、哪些被降级/排除
4. 需要定位根因时再读 `docs/review/round3/reports/01`–`12`；不要把分报告里已降级的项擅自升回 P1

仓库根目录：你拿到的工作区（Windows / pwsh 常见）。对话与你写的文档用简体中文。

## 硬性纪律

- **先审后做**：未写出 `docs/review/round3/plan-review.md` 之前，不得改业务代码。
- **只做 fix-plan 列出的 H1–H6**。不得顺手重构、不得把分报告里的 P3 / 已排除项做进来、不得改 `proto/`、不得 `task generate-proto`。
- **产品决策已拍板**：H3 放行 API Key 做 Databases DDL；H1 viewer 只读且写方法必须全部入表。不得自行改口。
- **不要 git commit / push / checkout / 改 remote**。改动留在工作区。不要碰用户未提交的、与本任务无关的文件。
- 不引入新依赖。遵循 Clean Architecture：传输层不写业务；Teams 写方法不要套 `RequireServerWriteActor`（Client API 复用）。
- 本地可能没有 Postgres/Redis/MinIO/Docker：**只跑 `go test -short` 与纯单元测试**。集成测试不要强行跑。
- 新增测试必须断言真实行为（viewer 真的 403、错 secret 真的不删 Redis 键、CreateDeployment 的 ctx 真有 Principal）。禁止 `require.True(t, true)` 或只断言「函数能调用」。
- 改 `console/src` 后必须 `task console-build`，否则 Go embed 打旧前端。
- 行号以你读到的当前代码为准，fix-plan 里的行号只是锚点。

## 阶段 0 — 审核方案（必须先完成）

对照**当前工作区源码**审核 `fix-plan.md`，写入 `docs/review/round3/plan-review.md`，结构：

```
# Round 3 方案审核

## 缺陷是否仍在
（H1–H6 逐项：仍在 / 已消失 / 描述不准确，带当前文件:行号）

## 误伤风险评估
（H1 member 业务写、H3 API Key DDL、Client Teams、公开 Confirm 等）

## 方案问题与偏差
（写错、漏登记的写 RPC、测试抓不住根因、与 G12/B1 冲突）

## 将采用的实施口径
（完全按方案 / 下列偏差：…）
```

若方案与代码不符：在「将采用的实施口径」写清替代做法，**之后按替代做法实现**，并在 fix-report 再记一次。

## 阶段 1 — 按依赖实施

| 阶段 | 批次 | 说明 |
|------|------|------|
| 1a 可并行 | H1、H2、H4、H5 | 文件集见 fix-plan §8 |
| 1b | H3 | 只改 `databases.go` DDL 守卫 + 测试；与 H1 都动 `internal/app/server` 时避免抢同一测试文件 |
| 1c | H6 | 最后做 |

同一工作区建议串行：H1 → H2 → H3 → H4 → H5 → H6，每批做完立刻跑该批验收命令，失败先修再往下。

每批做完自检：

1. fix-plan 该节每一条都有着落（含「顺手」的 H4-3 / H5-2 / H5-3）。
2. 未写入 §8「不要碰」的文件。
3. 该批验证命令 exit 0。

### 批次要点（实施时按 fix-plan 正文，这里只防漏）

- **H1**：按 `apiKeyScopeRules` 里所有 `write` 补 `adminRoleMethodRules`，不要只补 `DeleteAPIKey`/`UpdateUser`。启动期 `AssertAdminRoleWriteCoverage` fail-closed。`Teams` 不要加 `RequireServerWriteActor`。
- **H2**：`contexts.WithPrincipal` 注入后调用 `CreateDeployment`。禁止削弱 `RequireServerWriteActor`。测试必须读到 ctx 里的 Principal。
- **H3**：DDL 全部改为 `RequireServerWriteActor`。系统集合拒绝 / default 库保护保持。API Key 与 admin 过守卫，端用户/匿名拒绝。
- **H4**：`Torchwood.server.functions` 必须挂上。`noRefreshMethods` / TS `auth: "none"` 覆盖 Account 全部 `ACCESS_PUBLIC`（grep proto，不要只改 ConfirmEmailChange）。
- **H5**：预览走 axios blob + revoke；不要改 `file_handler` 的 cookie-优先语义。邀请查重在 `CreateDocument` 之前；空 `user_id`/`email` 不要建会撞车的 unique。
- **H6**：只修 fix-plan 点名的 List RPC 和三处 Redis；不要给 ListFunctions 等「从未分页」的接口新做仓储分页。

## 阶段 2 — 整体验证与报告

本地能跑的都跑：

```
go vet ./...
go build ./...
go test -short ./...
task console-build
cd sdk/typescript && npm test
go test ./sdk/go/client/... ./sdk/go/server/...
```

写 `docs/review/round3/fix-report.md`：

```
# Round 3 修复报告

## 方案审核摘要
（链到 plan-review.md，列出实际采用的偏差）

## 逐项对照
| ID | 项 | 状态 | 证据（文件:行） | 验证命令 |
H1-1 … H6-4，状态仅允许 ✅ / ❌ / ⚠️

## 验证
（上述命令的 exit code；失败的包名）

## 未做与原因
## 需原审查方深度审查时关注的点
```

## 最终回复给人类的摘要

- 方案审核结论（通过 / 有偏差）
- H1–H6 完成度
- 验证命令结果
- 交付路径：`plan-review.md`、`fix-report.md`、工作区 diff（未提交）
- **到此停止**。不要邀请原审查方继续改代码，不要开下一轮重构。
