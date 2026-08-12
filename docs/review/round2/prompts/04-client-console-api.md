# 复审任务（Round 2）：04 - Client/Console API 传输层（clientgrpc + consolegrpc）

## 背景
- Round 1 全模块审查已完成，产出 `docs/review/fix-plan.md`（F1–F11 修复批次，提交 1288705）。
- 修复已陆续合入：`git log --oneline 1288705..HEAD` 可见各 fix 提交；当前工作区可能还有未提交改动，审查以当前工作区代码为准。
- 本任务为**只读复审**：不修改任何代码，只输出复审报告。

## 角色
你是资深 Go 后端代码审查专家，对 Torchwood 项目（Appwrite 风格的 AI/Agent-Native BaaS 平台）的「Client API 与 Console API 传输层」做一次只读审查。同时你是修复验证者，需对照 fix-plan 逐条核实。

## 第一步：建立基线
- 读 `docs/review/prompts/04-client-console-api.md`：其「审查范围」「审查重点」「通用检查项」「输出要求」全部沿用于本轮。
- 读 `docs/review/fix-plan.md` 的 F1-1、F2-1、F7-1、F8-2 章节：这是本模块 Round 1 结论与修复方案。
- 可用 `git log --oneline 1288705..HEAD -- internal/api/clientgrpc/ internal/api/consolegrpc/ proto/client/v1/account.proto proto/console/v1/auth.proto` 与 `git show <commit>` 查看修复的实际改动。

## 必读上下文
- 仓库根目录：`D:\Codes\qiulin\torchwood`
- 架构：`internal/api/clientgrpc` 是终端用户 API；`internal/api/consolegrpc` 是 Admin Console API；两者经 grpc-gateway 暴露为 REST
- 鉴权：用户 JWT / session cookie；Console admin 会话（HttpOnly cookie `TORCHWOOD_session_console`）；API Key 也可访问部分端点（需带 `X-Torchwood-Project` header）
- 约定：gRPC 方法必须带 proto authz 注解；handler 从 context 取 Principal，不自行解析凭据；列表复用 `pkg/crud`
- 本轮修复涉及文件：`internal/api/clientgrpc/account.go`、`internal/api/consolegrpc/admins.go`、`proto/client/v1/account.proto`、`proto/console/v1/auth.proto` 及相关用例层

## 复审重点 A：修复验证（逐条核实）
对 fix-plan 中本模块的每一个修复项逐条核实：

1. **F1-1 Magic URL 登录 secret 回传响应体**
   - 文件锚点：`internal/api/clientgrpc/account.go:403-416`、`internal/app/client/magic_url.go:77-87`
   - 核实：`CreateMagicURLSession` 响应是否只返回不透明 challengeID，不再透传可登录 secret；Challenge 结构是否支持区分 secret 与不透明 ID；handler 是否仅做透传未自行泄露；`POST /v1/account/sessions/magic-url` 响应测试是否断言无 secret。

2. **F2-1 API Key 全量 scope 越权 console AdminsService**
   - 文件锚点：`pkg/grpc/interceptor/jwt.go:110-144`、`internal/api/consolegrpc/admins.go:38-80`
   - 核实：拦截器 `permissionMethods` 分支是否对 `CredentialTypeAPIKey` 直接拒绝；`AdminsService` handler/use-case 是否增加 `ActorKind == Admin` 纵深防御守卫；`*`/`all` scope API Key 调 `CreateAdmin/ListAdmins/UpdateAdmin/DeleteAdmin` 的测试是否断言 PermissionDenied。

3. **F7-1 Console 首个管理员引导 SignUp 入口**
   - 文件锚点：`internal/app/console/setup.go:98-107`、`proto/console/v1/auth.proto`（SignUp PUBLIC）
   - 核实：`security.setup_token` 配置与 `TORCHWOOD_SECURITY_SETUP_TOKEN` 是否生效；未设置 setup token 时 `SignUp` 是否拒绝（403/404）；请求是否校验 setup token；并发首次性检查是否使用 `pg_advisory_xact_lock` 或唯一约束兜底；proto `SignUp` 注解是否仍保持 PUBLIC 或已按需调整。

4. **F8-2 DeleteSessions keepCurrent 无法传递**
   - 文件锚点：`proto/client/v1/account.proto:56-57`、`src/client/account.ts:92-96`、`internal/api/clientgrpc/account.go`（DeleteSessions handler）
   - 核实：proto `DeleteSessions` 是否已加 `body: "*"`（或改为 query 绑定）并重新生成；gateway 是否成功把 `keepCurrent` 映射到请求体；handler 是否正确读取新字段并透传给用例；TS SDK 是否同步支持 `keepCurrent`。

每条按以下维度检查：
1. 修复是否已落地（代码中能否找到对应改动）；
2. 修复是否正确完整——有无绕过路径、边界遗漏（例如只改了入口 A 没改入口 B、校验可加在错误层、并发场景仍可乘）；
3. 修复是否引入新问题（接口/行为变化是否同步到全部调用方与前端/SDK）；
4. 承诺的测试是否真实存在且断言的是真实行为（不是恰好通过的假断言）。

## 复审重点 B：回归与新问题排查
- 修复触动的文件及其上下游：行为变化是否破坏既有功能（功能完整性回归）。
- Round 1 报告中的 P2/P3 未修项：确认仍存在则原级保留，被修复波及的标注变化。
- 按 round-1「通用检查项」重扫本模块：安全（注入/越权/信息泄露/凭据处理）、正确性（错误处理/并发/事务边界）、一致性（与 AGENTS.md 约定、proto 注解、domain 端口签名）、测试质量。
- 本模块修复后特有风险点：
  - F1-1 修改 Challenge 结构或响应字段后，需确认前端/SDK 对 Magic URL 响应的解析未断裂，旧 secret 字段未在日志/错误信息中残留。
  - F2-1 在 consolegrpc/admins.go 增加 `ActorKind == Admin` 守卫后，需确认合法 admin 会话未被误伤，且 API Key 在 console 其他非 admin 方法上的权限边界与拦截器保持一致。
  - F7-1 SignUp 从 PUBLIC 改为需 setup token 后，需确认所有入口（gRPC、grpc-gateway、console 前端登录页）均同步拒绝无 token 请求，未因缓存路由或旧客户端跳过校验。
  - F8-2 修改 proto body 绑定并重新生成后，需确认 REST 路由、`openapiv2` 产物、TS/Go SDK 均同步更新，避免 `keepCurrent` 在 gateway 层仍被忽略。

## 输出要求
简体中文复审报告，三节结构：
1. **修复验证结论表**：每个修复项一行——✅已修复 / ⚠️部分修复 / ❌未修复 / 🔴引入回归，附证据（`文件路径:行号`）与一句话说明；
2. **新发现问题**：按 🔴P0 / 🟠P1 / 🟡P2 / 🟢P3 分级，每条给 `文件路径:行号` + 问题描述 + 影响 + 修复建议；
3. **模块总体结论**：修复完成度百分比估计、剩余风险 Top 3、是否建议关闭本模块审查。

## 约束
- 只读，不修改任何文件；不运行需要 Postgres/Redis/MinIO/Docker 的集成测试；
- 可运行 `go vet ./internal/api/clientgrpc/... ./internal/api/consolegrpc/...` 与无外部依赖的纯单元测试辅助验证。
