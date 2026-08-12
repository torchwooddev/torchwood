# 复审任务（Round 2）：06 - Server/Console 用例层（internal/app/server、internal/app/console）

## 背景

- Round 1 全模块审查已完成，产出 `docs/review/fix-plan.md`（F1–F11 修复批次，提交 1288705）。
- 修复已陆续合入：`git log --oneline 1288705..HEAD` 可见各 fix 提交；当前工作区可能还有未提交改动，审查以当前工作区代码为准。
- 本任务为**只读复审**：不修改任何代码，只输出复审报告。

## 角色

你是资深 Go 后端代码审查专家。对 Torchwood 项目（Appwrite 风格的 AI/Agent-Native BaaS 平台）的「Server/Console 用例层」做一次**只读**审查。**不得修改任何代码**，只输出审查报告。同时你是修复验证者，需对照 `fix-plan.md` 逐条核实。

## 第一步：建立基线

- 读 `docs/review/prompts/06-server-console-use-cases.md`：其「审查范围」「审查重点」「通用检查项」「输出要求」全部沿用于本轮。
- 读 `docs/review/fix-plan.md` 的 **F2-2、F4 全部（注意 §12：F4-2 并入 F3、F4-6 并入 F2，标注归属即可）、F5-4（variables 脱敏的 use-case 侧）、F7-1（console setup）** 章节：这是本模块 Round 1 结论与修复方案。
- 可用 `git log --oneline 1288705..HEAD -- internal/app/server/ internal/app/console/ internal/app/shared/ internal/app/functions/variables.go internal/api/servergrpc/functions.go internal/api/servergrpc/projects.go` 与 `git show <commit>` 查看修复的实际改动。

## 必读上下文

- 仓库根目录：`D:\Codes\qiulin\torchwood`
- 先读 `AGENTS.md`（开发约定）与 `docs/roadmap.md` §2.2/§2.3（Server Users/Teams 管理验收标准）
- 架构：`internal/app/server` 是管理面用例层；`internal/app/console` 是 Console 用例层（Admins、bootstrap）；`internal/app/shared` 为共享用例逻辑
- 鉴权：API Key（scope）或 Console admin 会话；admin 通过 `X-Torchwood-Project` 指定项目
- 约定：列表查询复用 `pkg/crud`；动态文档操作委托 `internal/infra/documentdb`；元数据表用 bun repo

## 复审重点 A：修复验证（逐条核实）

1. **F2-2 🔴 Console 受限 admin（viewer/member）经 Server API 全面提权（P0）**
   - 锚点：`pkg/grpc/interceptor/jwt.go:110-137` + `internal/app/server/*` 各用例
   - 核实：拦截器是否按 admin 角色约束 viewer/member 仅放行 List/Get/Count；`CreateAPIKey`、`CreateUserToken`、`UpdateUserPassword`、`DeleteUser`、databases 写方法等是否在校验 `principal.IsPlatformAdmin`；viewer 调写方法是否返回 PermissionDenied 测试。

2. **F4-1 🔴 用户/团队级联删除被 50 条截断（P0）**
   - 锚点：`internal/app/server/users.go:287-326`、`internal/app/server/teams.go:455-463`
   - 核实：`DeleteUser` 的 sessions/identities/memberships 文档级联、`DeleteTeam` 的 memberships 级联是否改为 `PageSize: 1000` 循环直至 `NextPageToken` 为空；是否补了 >50 条集成测试。

3. **F4-2 🟠 DeleteDatabase/DeleteCollection 不清理元数据（P1）** — 已并入 F3-5
   - 锚点：`internal/infra/documentdb/postgres.go:143-154`、`:272-287`
   - 说明：本项归属 F3 动态文档层，本模块不逐条验证，但需知悉其是否已完成，避免重复记录。

4. **F4-3 🟠 团队 last-owner 保护缺失（P1）**
   - 锚点：`internal/app/server/teams.go:280-300,357-368`、`internal/app/client/teams.go:140-161`
   - 核实：删除/降级 owner membership 前是否统计 accepted 且含 owner 角色的成员数，≤1 时拒绝（FailedPrecondition）；client 自退路径是否同步校验。

5. **F4-4 🟠 UpdateUser 改邮箱不查重（P1）**
   - 锚点：`internal/app/server/users.go:141-153`
   - 核实：email 分支是否先按新邮箱查重（排除自身 userID），重复返回 AlreadyExists；`MapDocumentDBError` 是否将 23505 映射为 AlreadyExists 作为并发兜底。

6. **F4-5 🟠 GetProject 返回 nil,nil（P1）**
   - 锚点：`internal/api/servergrpc/projects.go:67-71`
   - 核实：`p == nil` 时是否返回 `codes.NotFound`（对齐 users.go 处理）。

7. **F4-6 🟡 补强项（P2）** — 已并入 F2
   - 锚点：`internal/app/server/users.go:272-282`（级联包事务）、`:249-270`（CreateUserToken 审计/生命周期）、`internal/app/console/admins.go`（角色下沉）
   - 说明：按 F2 提交归属验证。确认 `DeleteUser`/`DeleteTeam` 级联是否整体包入事务；`CreateUserToken` 是否加了审计标记与生命周期限制；admins use-case 角色校验是否下沉。

8. **F5-4 🟠 GetVariables 明文返回全部 secret（P1）**
   - 锚点：`internal/app/functions/variables.go:30-35`、`internal/api/servergrpc/functions.go:237-247`
   - 核实：Get 返回的变量值是否已脱敏（空串或掩码）；值是否仅在 SetVariables 请求/响应中可见一次；Console/前端/SDK 是否已适配该变化。

9. **F7-1 🔴 Console 首个管理员引导可被抢占（P0）**
   - 锚点：`internal/app/console/setup.go:98-107`、`proto/console/v1/auth.proto`（SignUp PUBLIC）
   - 核实：use-case 是否读取 `security.setup_token`（env `TORCHWOOD_SECURITY_SETUP_TOKEN`）并校验请求 token；未设置或校验失败是否拒绝；首次创建 admin 是否用 `pg_advisory_xact_lock` 串行化。

## 复审重点 B：回归与新问题排查

- 修复触动的文件及其上下游：F2-2 几乎触及全部 server 写用例，确认调用方（grpc/gateway）无签名/行为不兼容；F4-1 改动级联删除循环，确认不破坏事务边界；F7-1 改动 bootstrap，确认不会导致正常启动后首个 admin 无法创建。
- Round 1 报告中的 P2/P3 未修项：确认仍存在则原级保留，被修复波及的标注变化。
- 按 round-1「通用检查项」重扫本模块：安全（注入/越权/信息泄露/凭据处理）、正确性（错误处理/并发/事务边界）、一致性（与 AGENTS.md 约定、proto 注解、domain 端口签名）、测试质量。
- **本模块修复后特有风险点**：
  1. F2-2 收口 admin 权限后，需重查合法非 platform admin 是否被误伤，以及各写方法守卫是否加在同一层（不能部分在 interceptor、部分在 use-case）。
  2. F4-1 级联删除改成大页循环后，需确认外层事务不会超时或死锁，且删除条件仍带 `projectID` 过滤，避免跨项目误删。
  3. F4-3 last-owner 保护在 server 与 client 两条路径均需生效；修复后需确认 self-leave 路径也加了同样统计，防止只剩 owner 时通过客户端退队绕过。
  4. F7-1 setup token 依赖新增配置字段与 wire；需确认 config 已生成、绑定成功，否则 setup.go 可能静默拒绝或绕过。
  5. F5-4 变量脱敏后，需确认 Console 前端不再依赖 GetVariables 明文回显，且 worker/执行环境仍能通过内部渠道读取真实值。

## 输出要求

简体中文复审报告，三节结构：
1. **修复验证结论表**：每个修复项一行——✅已修复 / ⚠️部分修复 / ❌未修复 / 🔴引入回归，附证据（`文件路径:行号`）与一句话说明；
2. **新发现问题**：按 🔴P0 / 🟠P1 / 🟡P2 / 🟢P3 分级，每条给 `文件路径:行号` + 问题描述 + 影响 + 修复建议；
3. **模块总体结论**：修复完成度百分比估计、剩余风险 Top 3、是否建议关闭本模块审查。

## 约束

- 只读，不修改任何文件；不运行需要 Postgres/Redis/MinIO/Docker 的集成测试；
- 可运行 `go vet ./internal/app/server/... ./internal/app/console/... ./internal/app/shared/... ./internal/app/functions/...` 与无外部依赖的纯单元测试辅助验证。
