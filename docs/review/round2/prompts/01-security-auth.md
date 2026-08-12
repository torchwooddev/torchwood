# 复审任务（Round 2）：01 - 安全与认证（拦截器 / 会话 / OTP / OAuth / MFA）

## 背景

- Round 1 全模块审查已完成，产出 `docs/review/fix-plan.md`（F1–F11 修复批次，提交 1288705）。
- 修复已陆续合入：`git log --oneline 1288705..HEAD` 可见各 fix 提交；当前工作区可能还有未提交改动，审查以当前工作区代码为准。
- 本任务为**只读复审**：不修改任何代码，只输出复审报告。
- 本轮关注修复批次：F1（infra/auth 部分：F1-2、F1-3、F1-4）、F2（拦截器部分：F2-1、F2-4）、F7-5、F7-6（审计项）。

## 角色

你是资深 Go 安全代码审查专家，对 Torchwood 项目的「认证与授权安全」模块执行只读审查。同时你是修复验证者，需对照 `fix-plan.md` 逐条核实修复是否落地、是否完整、是否引入回归，并验证承诺的测试是否真实存在。

## 第一步：建立基线

- 读 `docs/review/prompts/01-security-auth.md`：其「审查范围」「审查重点」「通用检查项」「输出要求」全部沿用于本轮。
- 读 `docs/review/fix-plan.md` 的 F1（infra/auth 部分：F1-2、F1-3、F1-4）、F2（拦截器部分：F2-1、F2-4）、F7-5、F7-6（审计项）章节：这是本模块 Round 1 结论与修复方案。
- 同步阅读对应 `*_test.go`：判断「补测试」承诺是否兑现，断言是否验证真实行为而非恒真。
- 可用 `git log --oneline 1288705..HEAD -- pkg/grpc/interceptor/ internal/infra/auth/ pkg/jwtparser/ pkg/secretbox/ pkg/password/ internal/pkg/config/ cmd/server/` 与 `git show <commit>` 查看修复的实际改动。

## 必读上下文

- 仓库根目录：`D:\Codes\qiulin\torchwood`
- 架构：`internal/api`（传输）→ `internal/app`（用例）→ `internal/domain`（端口）→ `internal/infra`（适配器）；`pkg/` 为跨层工具
- 鉴权模型：API Key（带 scope）、用户 JWT（access/refresh）、Console admin 会话（HttpOnly cookie `TORCHWOOD_session_console`）、匿名会话、OTP、OAuth2、TOTP MFA
- 关键约定：API Key 参与 `_perms` 的 `keys` 角色，不默认 bypass 文档权限；`X-Torchwood-Project` 头用于 admin 指定目标项目；JWT claims 与 `pkg/jwtparser` 的映射必须兼容；反向代理恢复真实 IP 需显式配置 `security.trusted_proxies`
- 本模块核心文件：`pkg/grpc/interceptor/*`、`internal/infra/auth/*`、`pkg/jwtparser/*`、`pkg/secretbox/*`、`pkg/password/*`

## 复审重点 A：修复验证（逐条核实）

对 fix-plan 中本模块的每一个修复项，逐条核实修复是否落地、是否完整、是否引入新问题：

1. `internal/infra/auth/account_token_redis.go:93-116` **F1-2 account token 校验非原子**
   - 是否已改用 `GETDEL` 实现校验+删除原子化；
   - 是否有并发双消费测试断言仅一次成功；
   - 失败或 key 不存在时的错误返回是否明确。

2. `internal/infra/auth/totp.go:98-110` / `internal/app/client/mfa.go:274` **F1-3 MFA 登录无防重放/锁定**
   - `ValidateTOTP` 是否复用 `claimUsedCode`（60s 防重放）与 `checkFactorLock`/`recordFactorFailure`（15min/5 次锁定）；
   - `CreateMFASession` 是否增加频控；
   - 锁定计数是否与注册/登录路径共享同一状态。

3. `internal/infra/auth/totp.go:42-45,60` / `pkg/secretbox/secretbox.go:16-19` **F1-4 TOTP secret 与 JWT 共用主密钥**
   - 是否已派生独立 purpose key（或 HKDF 域分离）；
   - 若变更密钥域，是否提供双密钥解密窗口以避免存量 TOTP 失效；
   - OTP/cookie 是否同步分离密钥域。

4. `pkg/grpc/interceptor/jwt.go:110-144` / `internal/api/consolegrpc/admins.go:38-80` **F2-1 API Key 全量 scope 越权 console AdminsService**
   - `jwt.go` 的 `permissionMethods` 分支是否对 `CredentialTypeAPIKey` 直接拒绝；
   - `AdminsService` handler/use-case 是否增加 `ActorKind == Admin` 纵深防御；
   - 是否补测试断言 `*`/`all` scope 调 `CreateAdmin/ListAdmins/UpdateAdmin/DeleteAdmin` 返回 PermissionDenied。

5. `pkg/grpc/interceptor/jwt.go:150-165` **F2-4 extractCredential 多凭证并存时拒绝**
   - 当请求同时携带 API Key 与 JWT/cookie 时是否直接拒绝；
   - 是否覆盖 metadata 中多 `authorization` 值、cookie + header 并存的场景；
   - 拒绝状态码是否为 `Unauthenticated` 或 `PermissionDenied`。

6. `internal/api/serverhttp/file_handler.go:700-767` / `internal/api/serverhttp/functions_handler.go:173-232` **F2-4 HTTP 鉴权三处重复抽公共辅助**
   - 是否已抽取公共 `httpAuth` 辅助统一鉴权；
   - 新辅助是否保持与原逻辑一致的凭证优先级和错误映射；
   - 若 F2-4 后置未做需标注为计划外，并检查是否引入行为漂移。

7. `cmd/server/provides.go:48-50` / `.env.example:8` **F7-5 JWT 弱默认被启动校验接受**
   - 启动时是否拒绝已知弱值（如 `change-me-in-production`）与 <32 字符密钥；
   - 是报错退出还是仅打印告警；
   - `.env.example` 是否同步更新为强随机占位值。

8. `pkg/grpc/interceptor/audit.go:60-62` **F7-6 审计落库无超时 + 错误静默**
   - 审计写入是否带上下文超时；
   - 失败是否记录错误而非静默吞掉；
   - 是否因超时阻塞 gRPC 响应返回。

## 复审重点 B：回归与新问题排查

- 修复触动的文件及其上下游：行为变化是否破坏既有功能（功能完整性回归）。
- Round 1 报告中的 P2/P3 未修项：确认仍存在则原级保留，被修复波及的标注变化。
- 按 round-1「通用检查项」重扫本模块：安全（注入/越权/信息泄露/凭据处理）、正确性（错误处理/并发/事务边界）、一致性（与 AGENTS.md 约定、proto 注解、domain 端口签名）、测试质量。
- 修复后特有风险点：
  - F1-2 改为 `GETDEL` 后，需确认 Redis 集群模式下对同一 key 的原子性是否仍然成立，以及失败时是否返回明确错误而非 nil 误认无效。
  - F1-3 的锁定/重放逻辑若与注册路径共用函数，需确认注册时不会因为登录失败次数而被误锁定。
  - F1-4 密钥域分离后，需确认既有加密 TOTP secret 的迁移/降级读取路径，避免已注册 MFA 用户全部无法登录。
  - F2-1 在 `permissionMethods` 拒绝 API Key 后，需验证合法 Server API 调用方（`apiKeyMethods` 路径）未被误伤，尤其是 admin console 通过 session cookie 的调用是否仍走 `permissionMethods` 分支。
  - F2-4 多凭证拒绝若过于严格，可能误伤 SDK 同时携带 API Key 与匿名会话 token 的合法流程，需核对 SDK/CLI 调用模式。
  - F7-5 弱密钥校验加入启动路径后，需确认开发/测试环境配置不会因默认占位值导致启动失败或把告警误报为 P0。
  - F7-6 审计超时后若未降级为异步，可能拖慢所有受保护 RPC 的 p99 延迟。

## 输出要求

简体中文复审报告，三节结构：

1. **修复验证结论表**：每个修复项一行——✅已修复 / ⚠️部分修复 / ❌未修复 / 🔴引入回归，附证据（`文件路径:行号`）与一句话说明；若某项未找到对应改动，请明确标注「未落地」。
2. **新发现问题**：按 🔴P0 / 🟠P1 / 🟡P2 / 🟢P3 分级，每条给 `文件路径:行号` + 问题描述 + 影响 + 修复建议；若未发现新问题则写「未发现」。
3. **模块总体结论**：修复完成度百分比估计、剩余风险 Top 3、是否建议关闭本模块审查。

## 约束

- 只读，不修改任何文件；不运行需要 Postgres/Redis/MinIO/Docker 的集成测试；
- 可运行 `go vet ./pkg/grpc/interceptor/... ./internal/infra/auth/... ./pkg/jwtparser/... ./pkg/secretbox/... ./pkg/password/...` 与无外部依赖的纯单元测试辅助验证；
- 所有路径/行号以当前工作区代码为准，若修复提交未覆盖锚点行，请在报告中指出实际所在位置。
