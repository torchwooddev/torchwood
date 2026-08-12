# 接力修复总控任务（Round-2）：Torchwood 修复后复审问题修复

## 角色

你是本轮修复的**总控 agent（tech lead）**。你的职责不是亲自写大部分代码，而是：

1. 精读修复方案，按依赖顺序把 10 个修复批次（G1–G10）分派给子 agent 执行；
2. 对每个批次的产出做验收（审查 diff + 运行验证命令），不合格打回重做；
3. 全部完成后做整体验证并输出汇总报告。

## 必读输入

- **`docs/review/round2/fix-plan.md`（全文，唯一事实来源）**：§0 批次总览、§1–§10 各批次修复清单（含位置/方案/验证）、§11 文件冲突矩阵、§12 回归验证清单。
- `AGENTS.md`（项目约定：分层、generate 命令、测试纪律）。
- 背景（按需查阅）：`docs/review/round2/reports/` 12 份复审报告，每条修复项在 fix-plan 中标注了来源编号（如 R05-P1-2）。

## 当前状态与硬性纪律

- Round-2 复审已完成；本任务执行其中的修复方案。
- 工作区可能已有**用户的未提交改动**（如 `internal/api/consolegrpc/auth.go` 等），**不得回滚或覆盖**不属于你任务的改动。
- **不做任何 git 提交/分支/合并操作**；所有改动留在工作区，由上级统一审核。
- 不引入新依赖；遵循 AGENTS.md 分层约定（端口在 domain、适配器在 infra、列表查询复用 `pkg/crud`、gRPC 方法必须带 proto authz 注解）。
- 本地无 Postgres/Redis/MinIO/Docker，**不运行需要基础设施的集成测试**；以 `go vet` + `go test -short` + 纯单元测试为主，需 CI/Docker 验证的项明确标注。

## 执行计划（依赖图，严格遵守）

| 阶段 | 批次 | 说明 |
|------|------|------|
| 阶段 0 | **G1**（CI 接入） | 最先做，解锁后续验证 |
| 阶段 1（可并行） | **G2**（权限收口，唯一 P0）、**G3**（认证账户域）、**G4**（serverhttp）、**G5**（documentdb）、**G7**（基础设施）、**G8**（Console 前端）、**G9**（SDK/CLI） | 文件集两两无交集（见 fix-plan §11） |
| 阶段 2 | **G6**（Functions/Storage/Worker） | **必须等 G2 完成**：使用 G2-3 提供的 `internal/app/shared` RequirePlatformAdmin |
| 阶段 3 | **G10**（Proto/契约） | **必须等 G3 完成**（同文件 `internal/api/clientgrpc/account.go`）；需 `task generate-proto` |

- 同一工作区并行多个批次前，先核对 fix-plan §11 冲突矩阵确认文件集无交集；有疑虑就串行。
- G2 批次内**先完成 G2-3（共享 authz helper）并编译通过**，再让其余 G2 项与 G6 使用它。
- G3 改 `config.proto` 后须执行 `task generate-config`；G10 改 `proto/` 后须执行 `task generate-proto` 并 `go build ./...` 验证。

## 分派方式

对每个批次，用 Agent 工具（coder 类型）派一个子 agent。子 agent 的 prompt 必须包含：

1. **角色与边界**：资深工程师（对应领域）；只修 fix-plan §N 列出的问题，**逐条完成含 P2/P3 小项，不得遗漏，也不得做清单外的"顺手优化"**；
2. **必读**：`AGENTS.md`、`docs/review/round2/fix-plan.md` 的 §N 与 §11；
3. **约束**：fix-plan §11 中归其他批次的文件清单（明确"不要碰"）；不引入新依赖；不跑需基础设施的集成测试；不做 git 操作；
4. **验证**：该批次的 `go vet`/`go test -short`（Go）、`npx tsc --noEmit`/`npm test`/`task console-build`（前端）、或 CI 语法检查（G1）；新增测试必须断言真实行为，禁止假断言；
5. **输出**：逐项汇报「改动文件:位置 + 改动摘要 + 验证结果」，标注需 CI 验证的项。

## 批次关键提醒（分派时原样传达）

- **G1**：`.github/workflows/ci.yml` 加 buf lint、`sdk/typescript` npm test、`sdk-demo-build`；`Taskfile.yml` test 任务补 TS 依赖。
- **G2**：唯一 P0。`adminRoleMethodRules` 补登时**逐一核对 `proto/server/v1/functions.proto` 的 RPC 清单**，不留遗漏；角色模型对齐 Console `useAdminRole`（viewer 只读 / member 业务写 / 平台 admin 不受限）；G2-3 共享 helper 优先落地。
- **G3**：不碰 `proto/`（DeleteFactor 契约归 G10）；一次性 JWT 消费注意不影响普通 access token；G3-2 改邮箱 staging 若需 proto 变更则按 fix-plan 执行 B 档最小缓解并留 backlog 备注。
- **G4**：抽取 `internal/api/serverhttp/auth.go` 公共 httpAuth；多凭证并存返回 401，与 gRPC `extractCredential` 语义一致。
- **G5**：事务化对齐既有 `RunInTx` 模式；顺手排查 postgres.go 其他裸 `p.db.New*` 调用点。
- **G6**：先确认 G2 已合入且 `appshared.RequirePlatformAdmin` 可用；zip bomb 修复按**实际写入字节**计数，不信任 zip 头声明。
- **G7**：连接池零值落安全默认并 Warn；SQL 脱敏补 INSERT 场景与 `setup_token` 列。
- **G8**：G8-2 变量页掩码覆写——按 fix-plan 推荐「掩码值 = 保持不变」约定，需要时小改 `internal/app/functions/variables.go` 配合；改完跑 `task console-build`。
- **G9**：DeleteFactor 的 SDK 同步**不做**（归 G10）。
- **G10**：最后做；`task generate-proto` 后 `go build ./...`；DeleteFactorRequest 加 `code` 字段并同步 handler 与 TS SDK；启动期 scope 一致性断言 fail-closed。

## 总控验收（每个批次完成后）

1. 审查子 agent 汇报是否逐项覆盖 fix-plan §N 全部条目；有遗漏直接打回。
2. 抽查实际 diff（`git diff --stat` 与关键文件），确认未越界改其他批次的文件、未混入无关重构。
3. 亲自运行该批次验证命令；失败则打回修复。
4. 验收通过后再启动下一依赖它的批次。

## 全部批次完成后的整体验证

按 fix-plan §12 执行本地可做的部分：

1. `task generate-all` 无异常 diff；
2. `go vet ./...`、`go build ./...`；
3. `go test -short ./...`（跳过需基础设施的）；
4. `task console-build`、`task sdk-demo-build`；`cd sdk/typescript && npx tsc --noEmit && npm run test`；
5. fix-plan §12 手工冒烟清单中**本地可做**的项（HTTP 多凭证 401、Console 变量页、恶意 zip 等可用单元/集成方式验证的）；
6. 需要真实基础设施/CI 的项（docker 集成、>50 会话级联等）明确标注「待 CI 验证」。

## 最终输出

写汇总报告到 `docs/review/round2/fix-report.md`，并在回复中给出摘要：

- 每批次：状态（完成/部分/失败）、改动文件清单、验证结果；
- fix-plan 全部修复项的对照表（已修复 ✅ / 未修 ❌ / 缓修并注明原因）；
- 遗留风险与需 CI 验证项清单；
- 你作为总控发现的新问题（若有）。
