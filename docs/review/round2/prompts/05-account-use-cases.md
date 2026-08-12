# 复审任务（Round 2）：05 - Account 用例层（internal/app/client）

## 背景
- Round 1 全模块审查已完成，产出 `docs/review/fix-plan.md`（F1–F11 修复批次，提交 1288705）。
- 修复已陆续合入：`git log --oneline 1288705..HEAD` 可见各 fix 提交；当前工作区可能还有未提交改动，审查以当前工作区代码为准。
- 本任务为**只读复审**：不修改任何代码，只输出复审报告。

## 角色
你是资深 Go 后端代码审查专家（认证业务领域），对 Torchwood 项目的「Account 用例层」进行只读复审。**不得修改任何代码**。同时你是修复验证者，需对照 `docs/review/fix-plan.md` 逐条核实修复是否真实、完整、无回归。

## 第一步：建立基线
- 读 `docs/review/prompts/05-account-use-cases.md`：其「审查范围」「审查重点」「通用检查项」「输出要求」全部沿用于本轮。
- 读 `docs/review/fix-plan.md` 的 §1「F1 认证域修复」：这是本模块 Round 1 结论与修复方案。
- 可用 `git log --oneline 1288705..HEAD -- internal/app/client/ internal/infra/auth/ internal/pkg/contexts/` 与 `git show <commit>` 查看修复的实际改动。

## 必读上下文
- 仓库根目录：`D:\Codes\qiulin\torchwood`
- 架构分层：`internal/app/client` 是终端用户认证与账号业务用例层，被 `internal/api/clientgrpc` 调用；依赖 `internal/domain/*` 端口与 `internal/infra/*` 适配器（bun repo、Redis auth store、storage、messaging）
- 能力范围：注册/登录、refresh、会话管理、prefs、匿名登录、Email/Phone OTP、Magic URL、邮箱验证、密码找回/重置、一次性 JWT、OAuth2、TOTP MFA（创建/验证/删除 + 登录挑战）、账号日志、Teams 客户端 API
- 安全约定：JWT claims 与 `pkg/jwtparser` 映射兼容；密码哈希用 `pkg/password`；OTP/state/challenge 存 Redis（`internal/infra/auth`）
- 本轮涉及文件：`internal/app/client/*.go`（含 `account.go`、`mfa.go`、`jwt.go`、`magic_url.go`、`email_otp.go`、`anonymous.go` 等）、`internal/infra/auth/*.go`、`internal/pkg/contexts/*.go`

## 复审重点 A：修复验证（逐条核实）

### F1-1 Magic URL 登录 secret 回传响应体（P0）
- 锚点：`internal/app/client/magic_url.go:77-87`、`internal/api/clientgrpc/account.go:403-416`
- 核实：`CreateMagicURLSession` 响应是否只返回不透明 challengeID，secret 仅存在于邮件链接中；`clientgrpc` handler 是否未透传 secret；补测试是否断言响应不含可登录 secret。

### F1-3 MFA 登录校验无防重放/锁定（P1）
- 锚点：`internal/infra/auth/totp.go:98-110`、`internal/app/client/mfa.go:274`
- 核实：`ValidateTOTP` 是否复用注册路径的 `claimUsedCode`（60s 防重放）与 `checkFactorLock`/`recordFactorFailure`（15min/5 次锁定）；`CreateMFASession` 是否增加频控；登录路径是否因新增锁定逻辑误伤合法用户。

### F1-5 删除 MFA 因子无需二次验证（P1）
- 锚点：`internal/app/client/mfa.go:200-232`
- 核实：删除 verified 因子前是否要求有效 TOTP code（或密码）；删除时是否作废该用户未消费 challenge；未 verified 因子的删除路径是否也被覆盖。

### F1-6 PATCH /v1/account 改邮箱无需再认证且不撤销会话（P1）
- 锚点：`internal/app/client/account.go:391-404,436-440`
- 核实：改邮箱是否要求旧密码（或已过二次验证）；变更后是否撤销全部会话；新邮箱验证前是否不生效；proto/前端是否同步该行为变化。

### F1-7 密码修改/重置后会话残留（>50 条分页截断）（P1）
- 锚点：`internal/infra/auth/session_service.go:157-170`、`internal/app/client/account.go:449,478-496`
- 核实：删除会话是否改为循环分页（PageSize=1000 直至 NextPageToken 空）或按 user_id 批量删除；是否补 >50 会话集成测试；修改密码路径与重置密码路径是否都调用完整清理。

### F1-8 CreateJWT「一次性」名不副实（P1）
- 锚点：`internal/app/client/jwt.go:13-54`
- 核实：是否加随机 jti 并在 Redis 记录一次性消费（Lua GETDEL/SETNX），或在 claims 绑定会话纳入校验；消费后再次使用是否返回明确错误；SDK/前端是否知晓该 JWT 变为一次性。

### F1-9 补强项（P2 批次）

#### F1-9.1 SignUp 无频控
- 锚点：`internal/app/client/account.go:140-213`
- 核实：是否复用 `RateLimiter` 按 IP 限流；限流键是否与登录/OTP 键隔离，避免误伤。

#### F1-9.2 邮箱无格式/长度校验
- 锚点：`internal/app/client/account.go:144`、`internal/app/client/email_otp.go:46`、`internal/app/client/magic_url.go:39`
- 核实：是否统一使用 `net/mail.ParseAddress` + ≤254 长度校验；错误返回是否区分 InvalidArgument 与内部错误。

#### F1-9.3 SignIn 时序枚举
- 锚点：`internal/app/client/account.go:263-270`
- 核实：不存在用户时是否对固定哑哈希执行一次 `Verify`，使耗时与存在用户相近；是否避免引入新的错误信息泄露。

#### F1-9.4 prefs 无大小限制
- 锚点：`internal/app/client/account.go:516-535`
- 核实：是否限制 64KB/嵌套深度；超出时是否返回 InvalidArgument；是否同时校验 JSON 类型。

#### F1-9.5 匿名用户无法升级
- 锚点：`internal/app/client/anonymous.go:27-62`
- 核实：`UpdateAccount` 是否在 `password_hash` 为空时允许直接设置密码；升级后匿名会话/身份是否被正确迁移或撤销。

#### F1-9.6 登录节流按邮箱可被定向锁号
- 锚点：`internal/infra/auth/login_throttle_redis.go:30-42`
- 核实：未注册邮箱失败是否不计数；已注册邮箱的计数/锁定阈值是否与文档一致。

#### F1-9.7 会话数量无上限
- 锚点：`internal/infra/auth/session_service.go:45-70`
- 核实：是否配置化上限，超限是否淘汰最旧会话；配置项是否已落入 `config.proto`/`configs/config.yaml.template`。

## 复审重点 B：回归与新问题排查
- 修复触动的文件及其上下游：行为变化是否破坏既有功能（功能完整性回归）。特别关注 `CreateMagicURLSession` 响应结构变化对 SDK/CLI/Console 调用方、`UpdateAccount` 邮箱变更重认证流程对前端表单、一次性 JWT 消费对已有 token 使用模式的影响。
- Round 1 报告中的 P2/P3 未修项：确认仍存在则原级保留，被修复波及的标注变化。
- 按 round-1「通用检查项」重扫本模块：安全（注入/越权/信息泄露/凭据处理）、正确性（错误处理/并发/事务边界）、一致性（与 AGENTS.md 约定、proto 注解、domain 端口签名）、测试质量。
- 本模块修复后特有风险点：
  1. **Magic URL challenge 不透明化后 SDK 契约变化**：若 SDK/Console 仍读取旧 `secret` 字段，会导致前端登录链路崩溃；需确认 TS/Go SDK 与 Console 已同步。
  2. **TOTP 登录路径新增锁定/防重放**：防重放窗口与登录 challenge 过期时间耦合不当可能导致合法用户在 60s 内无法完成 MFA 登录，或高并发下出现 factor 被意外锁定。
  3. **邮箱变更后撤销全部会话**：匿名/刚注册用户的会话是否也被正确处理；撤销失败（Redis 部分不可用）时是否回滚邮箱变更，避免账号被改但旧会话仍有效。
  4. **SignUp 按 IP 限流 + 登录节流不计未注册邮箱**：共享 Redis key 前缀或限流器实现可能导致规则互相覆盖，需检查 key 设计与计数器边界。

## 输出要求
简体中文复审报告，三节结构：
1. **修复验证结论表**：每个修复项一行——✅已修复 / ⚠️部分修复 / ❌未修复 / 🔴引入回归，附证据（`文件路径:行号`）与一句话说明；
2. **新发现问题**：按 🔴P0 / 🟠P1 / 🟡P2 / 🟢P3 分级，每条给 `文件路径:行号` + 问题描述 + 影响 + 修复建议；
3. **模块总体结论**：修复完成度百分比估计、剩余风险 Top 3、是否建议关闭本模块审查。

## 约束
- 只读，不修改任何文件；不运行需要 Postgres/Redis/MinIO/Docker 的集成测试；
- 可运行 `go vet ./internal/app/client/...` 与无外部依赖的纯单元测试辅助验证。
