# 修复任务 F1：认证域修复（account / infra-auth）

## 角色

你是资深 Go 后端工程师，负责修复 Torchwood 认证域的审查发现。修复方案详见
`docs/review/fix-plan.md` §1（F1 批次）。**只修复本任务列出的问题**，不得顺手改动其他逻辑。

## 工作目录与必读

- 仓库根目录：`D:\Codes\qiulin\torchwood`（Windows，pwsh）
- 必读：`AGENTS.md`、`docs/review/fix-plan.md` §1
- 审查报告（背景）：`docs/review/` 下的 01/04/05 报告

## 修复清单

1. **Magic URL secret 回传**（P0）：`internal/app/client/magic_url.go:77-87` 目前把登录
   secret 作为 `Challenge{ChallengeID: secret}` 返回，API 调用方可直接登录任意邮箱账号。
   修复：响应只返回**不透明 challengeID**（参考同包 email_otp.go 的模式），secret 仅存在于
   邮件链接中（`buildAccountActionURL` 保留）；clientgrpc handler（`internal/api/clientgrpc/account.go`
   CreateMagicURLSession 处）无需透传 secret。检查 Challenge 结构是否需要新增字段，保持 proto 契约不变
   （如果 proto 字段语义必须承载 secret，则在 use-case 返回不透明 ID 并在响应中删除 secret 值）。
2. **account token 校验非原子**（P1）：`internal/infra/auth/account_token_redis.go:93-116`
   `verifyToken` 是 GET→DEL 两步，并发可双消费（recovery 双重置/ magic URL 双会话）。
   修复：改用 `GETDEL` 原子消费（参考同目录 `oauth_state_redis.go`、`mfa_challenge_redis.go` 模式）。
3. **MFA 登录校验无防重放/锁定**（P1）：`internal/infra/auth/totp.go:98-110` `ValidateTOTP`
   不做 code 防重放与失败锁定（对比 `VerifyTOTPFactor` 有 claimUsedCode + checkFactorLock）。
   修复：`ValidateTOTP` 复用 `claimUsedCode`（60s 防重放）与 `checkFactorLock`/`recordFactorFailure`
   （15min/5 次锁定）；`internal/app/client/mfa.go` 的 `CompleteMFASession` 路径随之生效；
   对 `CreateMFASession` 增加频控（复用 RateLimiter，按 IP/账号）。
4. **TOTP secret 与 JWT 共用主密钥**（P1）：`internal/infra/auth/totp.go:42-45,60` 用
   `secretbox.Encrypt(key.Secret(), s.jwtSecret())` 加密 TOTP secret；`pkg/secretbox/secretbox.go`
   单次 SHA-256 派生无域分离。修复：为 TOTP 派生独立 purpose key（参考
   `pkg/jwtparser.DeriveKey` 模式，加域前缀如 "totp:"），保持 API 签名不变；
   **注意**：若直接换密钥会导致存量 TOTP secret 无法解密——评估双密钥（读旧写新）或文档化迁移。
5. **删除 MFA 因子无需二次验证**（P1）：`internal/app/client/mfa.go:200-232` `DeleteFactor`
   直接删除。修复：删除 verified 因子前要求提供有效 TOTP code（或当前密码）二次验证；
   删除时作废该用户未消费的 challenge。
6. **改邮箱无需再认证且不撤销会话**（P1）：`internal/app/client/account.go:391-404` 改邮箱
   无 old_password 校验（仅改密码才要求），且不撤销会话。修复：改邮箱要求旧密码
   （或已通过的二次验证）；变更后撤销该用户全部会话（复用 DeleteSessionsByUser）。
7. **密码修改/重置后会话残留**（P1）：`internal/infra/auth/session_service.go:157-170`
   `DeleteSessionsByUser` 与 `internal/app/client/account.go:478-496` `DeleteSessions` 只处理
   前 50 条（ListDocuments 默认分页）。修复：循环分页（PageSize=1000 直至 NextPageToken 空）。
8. **CreateJWT 一次性名不副实**（P1）：`internal/app/client/jwt.go:13-54` 无 jti、无消费记录、
   不含 SessionID（登出后仍有效、可重放 5min）。修复：生成随机 jti 并在 Redis 记录一次性消费
   （参考 OTP Lua / GETDEL 模式）；或在 claims 绑定会话纳入校验。保持 5min TTL 语义不变。
9. **P2 补强**（顺带处理）：
   - SignUp 无频控（`account.go:140-213`）→ 按 IP 限流（复用 RateLimiter，如 10/h）
   - 邮箱格式/长度校验（`account.go:144`、`email_otp.go:46`、`magic_url.go:39`）→ `net/mail.ParseAddress` + ≤254
   - SignIn 时序枚举（`account.go:263-270`）→ 用户不存在时对固定哑哈希执行一次 `password.Verify`
   - prefs 大小限制（`account.go:516-535`）→ ≤64KB + 嵌套深度 ≤20
   - 匿名用户升级（`anonymous.go:27-62` + `account.go:405-415`）→ `UpdateAccount` 在
     `password_hash` 为空（匿名）时允许直接设置密码（跳过 old_password）

## 约束

- 不修改 proto / 不重新生成代码（除非确认必要且必须说明）
- 保持现有代码风格与错误码约定（codes.InvalidArgument / AlreadyExists / Unauthenticated 等）
- 除修复必要外不新增注释；不引入新第三方依赖
- 不运行需要本地 Postgres/Redis 的集成测试；可运行纯单元测试

## 验证

- `go vet ./internal/app/client/... ./internal/infra/auth/... ./pkg/secretbox/...`
- `go test ./internal/infra/auth/... ./pkg/secretbox/...`（miniredis 单测无需外部服务）
- `go build ./...`
- 为修复项补必要的单元测试（magic URL 不返回 secret、token 并发消费、MFA 锁定等）

## 输出

最终汇报：按清单逐项给出「改动文件:位置 + 改动摘要 + 验证结果」；列出无法完成或需要
决策的事项（如 TOTP 密钥迁移策略）。
