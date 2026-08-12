# 复审报告（Round 2）：05 - Account 用例层（internal/app/client）

> 审查基准：当前工作区 HEAD；对比修复方案 `docs/review/fix-plan.md` §1「F1 认证域修复」。
> 涉及路径：`internal/app/client/*.go`、`internal/infra/auth/*.go`、`internal/api/clientgrpc/account.go`、`proto/client/v1/account.proto`、`internal/pkg/config/config.proto`、`configs/config.yaml.template`。

---

## 1. 修复验证结论表

| 修复项 | 结论 | 证据（文件路径:行号） | 一句话说明 |
|--------|------|----------------------|------------|
| F1-1 Magic URL secret 不回传响应体 | ✅ 已修复 | `internal/app/client/magic_url.go:80-91` 返回 `Challenge{ChallengeID, ExpireAt}`；`internal/api/clientgrpc/account.go:415-418` 只映射 `ChallengeId/ExpireAt`；`proto/client/v1/account.proto:474-477` 的 `ChallengeResponse` 已无 secret 字段；`internal/app/client/magic_url_test.go:128-129` 断言响应 challengeID ≠ 邮件 secret | API 仅返回不透明 challengeID，secret 仅存于邮件链接；TS SDK `createMagicURLSession` 已同步读取 `challenge_id` |
| F1-3 MFA 登录校验增加防重放/锁定 | ✅ 已修复 | `internal/infra/auth/totp.go:111-130` `verifyCode` 复用 `checkFactorLock`/`recordFactorFailure`/`claimUsedCode`；`totp.go:100-106` `ValidateTOTP` 走同一校验路径；`internal/app/client/mfa.go:275-277` `CompleteMFASession` 增加账号/IP 双维度频控 | 登录与注册共用 60s 防重放、15min/5 次锁定；`CompleteMFASession` 已加 `checkMFACompleteRateLimit` |
| F1-5 删除 MFA 因子需二次验证 | ⚠️ 部分修复 | `internal/app/client/mfa.go:209-263` use-case 对 verified 因子要求 code，并在删除后 `RevokeByUser` 作废未消费挑战；**但** `internal/api/clientgrpc/account.go:376-386` 因 `DeleteFactorRequest` 无 code 字段，永远传空字符串；`proto/client/v1/account.proto:590` 尚未新增 `code` 字段 | Use-case 实现符合方案，gRPC/Console 调用 verified 因子删除会固定返回 `InvalidArgument`，功能被阻断 |
| F1-6 PATCH /v1/account 改邮箱需再认证并撤销会话 | ⚠️ 部分修复 | `internal/app/client/account.go:432-469` 改邮箱/密码要求旧密码（匿名用户 `password_hash==""` 跳过），变更后调用 `DeleteSessionsByUser`；`internal/app/client/account_security_test.go:58-105` 覆盖该行为；`proto/client/v1/account.proto:419-425` 已新增 `old_password` | 再认证与撤销会话已实现，但**新邮箱未经验证即生效**，与方案「新邮箱验证前不生效」不符 |
| F1-7 密码修改/重置后会话完整清理 | ✅ 已修复 | `internal/infra/auth/session_service.go:156-179` `DeleteSessionsByUser` 循环分页（PageSize=1000）直至 `NextPageToken==""`；`internal/app/client/account.go:484-489` 与 `internal/app/client/recovery.go:135-137` 均调用该函数 | 默认 50 条截断问题已解决；缺少 >50 条会话的专项集成测试 |
| F1-8 CreateJWT「一次性」名不副实 | ⚠️ 部分修复 | `internal/app/client/jwt.go:41-67` 已生成随机 jti 并通过 `oneTimeTokens.Register` 在 Redis 登记；`internal/infra/auth/one_time_token_redis.go:22-48` 提供原子 `Register/Consume`；**但** `internal/infra/auth/validator.go:62-67` 校验 access token 时未调用 `Consume` | 创建侧已做一次性标记，验证侧未消费，JWT 仍可被重复用于任意受保护端点；SDK/前端也未被告知该 JWT 为一次性 |
| F1-9.1 SignUp 无频控 | ✅ 已修复 | `internal/app/client/account.go:145-167` 按 IP 限流（`signup:ip:<ip>`，10/小时）；`internal/app/client/account_security_test.go:180-209` 覆盖限流与 IP 隔离 | 限流键与登录/OTP 键隔离，不误伤 |
| F1-9.2 邮箱无格式/长度校验 | ✅ 已修复 | `internal/app/client/email_otp.go:205-215` `validateEmail` 使用 `net/mail.ParseAddress` + ≤254 长度；`account.go:177`、`email_otp.go:54`、`magic_url.go:46` 均调用；`internal/app/client/account_security_test.go:211-232` 覆盖 | 错误统一返回 `InvalidArgument`，与内部错误区分 |
| F1-9.3 SignIn 时序枚举 | ✅ 已修复 | `internal/app/client/account.go:151-159` 固定哑哈希 `dummyPasswordHash`；`account.go:300-303` 用户不存在时执行一次 `password.Verify` 后返回统一错误 | 两条失败路径错误信息一致（`invalid credentials`），且都执行一次 Verify |
| F1-9.4 prefs 无大小限制 | ✅ 已修复 | `internal/app/client/account.go:599-617` `validatePrefs` 限制 64KB / 嵌套深度 20；`UpdatePrefs` 在 `account.go:583` 调用；`internal/app/client/account_security_test.go:138-178` 覆盖 | 超出限制返回 `InvalidArgument`，同时通过 JSON Marshal 校验类型 |
| F1-9.5 匿名用户无法升级 | ✅ 已修复 | `internal/app/client/anonymous.go:27-61` 创建匿名会话；`internal/app/client/account.go:460-469` 在 `password_hash==""` 时跳过 `old_password`；`internal/app/client/account_security_test.go:107-136` 覆盖升级流程 | 匿名用户可直接设邮箱+密码，升级后会话被撤销 |
| F1-9.6 登录节流按邮箱可被定向锁号 | ❌ 未修复 | `internal/app/client/account.go:277-280` 只要密码失败即调用 `recordLoginFailure`；`internal/infra/auth/login_throttle_redis.go:44-49` 对未注册邮箱同样计数 | 方案要求「未注册邮箱失败不计数」，当前实现仍可被用于锁定任意邮箱 |
| F1-9.7 会话数量无上限 | ❌ 未修复 | `internal/infra/auth/session_service.go:45-70` `CreateSessionAndTokens` 直接创建会话，未检查数量；`internal/pkg/config/config.proto` 与 `configs/config.yaml.template` 均无会话上限/淘汰配置 | 方案要求的「配置化上限，超限淘汰最旧」未实现 |

**统计**：✅ 已修复 8 项、⚠️ 部分修复 4 项、❌ 未修复 2 项、🔴 引入回归 0 项。

---

## 2. 新发现问题

### 🟠 P1

1. **CreateJWT 一次性 JWT 可被重放使用**  
   - 位置：`internal/app/client/jwt.go:60-67` 仅登记，`internal/infra/auth/validator.go:62-67` 未消费  
   - 描述：虽然生成 token 时写入 Redis 一次性标记，但所有受保护端点的 JWT 校验仅验签 + 查会话，不调用 `OneTimeTokenStore.Consume`。攻击者截获该 token 后可在 5min TTL 内反复使用。  
   - 影响：一次性 JWT 名不副实，违反安全预期，可能导致越权重放。  
   - 建议：在 `Validator.principalFromJWT` 中识别一次性 JWT（如 claim 加 `jti_one_time=true` 或固定 scope），并在首次校验成功后 `GETDEL` 消费；二次使用返回 `Unauthenticated`。

2. **UpdateAccount 改邮箱未经验证即生效**  
   - 位置：`internal/app/client/account.go:446-448` 直接写入 `email` 并将 `email_verified` 置 false  
   - 描述：修复方案要求「新邮箱验证后才生效」。当前实现中，只要提供旧密码即可把账号邮箱改为任意地址并立即用于登录/找回密码流程。  
   - 影响：攻击者若获得旧密码，可立即将账号迁移到控制邮箱，再经 `CreateRecovery` 重置密码完成接管（虽然已需密码，但仍绕过邮箱验证这一二次安全边界）。  
   - 建议：引入 `pending_email`/`email_change_token` 暂存机制；验证 token 后才把 `pending_email` 写入 `email`。

3. **邮箱/密码变更后撤销会话失败无回滚，旧会话仍有效**  
   - 位置：`internal/app/client/account.go:474-489`、`internal/app/client/recovery.go:129-136`  
   - 描述：先 `UpdateDocument` 更新用户资料，再调用 `DeleteSessionsByUser`。若 Redis/文档层部分不可用导致删除失败，函数返回错误，但用户资料变更已提交。  
   - 影响：账号邮箱/密码已被修改，而旧会话 token 仍可继续使用，形成会话残留。  
   - 建议：将用户资料更新与全部会话撤销纳入同一失败语义；或在撤销失败后重试、告警并标记账号需强制重新登录。

4. **DeleteFactor gRPC 接口无法删除已验证因子（功能阻断）**  
   - 位置：`internal/api/clientgrpc/account.go:376-386`、`proto/client/v1/account.proto:590`  
   - 描述：use-case 要求 verified 因子删除必须提供有效 TOTP code，但 `DeleteFactorRequest` 没有 `code` 字段，handler 永远传 `""`，导致请求固定返回 `InvalidArgument`。  
   - 影响：Console/前端/SDK 无法删除已启用的 MFA 因子，用户无法换绑/关闭 MFA。  
   - 建议：在 `proto/client/v1/account.proto` 的 `DeleteFactorRequest` 增加 `string code = 2;` 并在 handler 透传；或同时支持密码作为二次验证手段。

5. **登录节流仍对未注册邮箱计数，可被定向锁号**  
   - 位置：`internal/app/client/account.go:277-280`、`internal/infra/auth/login_throttle_redis.go:44-49`  
   - 描述：与 F1-9.6 对应，当前实现未区分用户是否存在，所有失败登录均按邮箱计数。  
   - 影响：攻击者可针对目标邮箱故意失败 10 次，使其在 15 分钟内无法登录。  
   - 建议：仅在用户存在（或密码哈希非空）时调用 `RecordFailure`；未注册邮箱失败只返回统一错误，不递增计数器。

6. **会话数量仍无上限，可无限增长**  
   - 位置：`internal/infra/auth/session_service.go:45-70`、`internal/pkg/config/config.proto`、`configs/config.yaml.template`  
   - 描述：与 F1-9.7 对应，未实现配置化会话数量上限及最旧会话淘汰。  
   - 影响：长期未清理的会话会无限占用 PG 文档与 Redis rotation 存储，增加泄露面。  
   - 建议：在 `config.proto` 增加 `security.sessions.max_per_user`；创建会话时若超限则删除最旧的 `expire_at` 会话。

### 🟡 P2

7. **DeleteSessionsByUser 循环删除非事务，部分失败时状态不一致**  
   - 位置：`internal/infra/auth/session_service.go:156-179`  
   - 描述：分页拉取后逐条 `DeleteDocument`，中间某条失败时已删除的会话无法回滚。  
   - 影响：密码/邮箱变更后可能出现部分会话仍有效。  
   - 建议：使用文档 DB 批量删除接口，或在失败时返回部分删除数量并触发告警/重试。

8. **CreateJWT 测试未覆盖重放拒绝路径**  
   - 位置：`internal/app/client/jwt_test.go:30-87`  
   - 描述：测试仅断言 Redis 中存在一次性标记，未验证第二次携带同一 token 访问受保护端点会被拒绝。  
   - 影响：一次性消费逻辑缺少回归防护，未来重构容易回退。  
   - 建议：补充「同一 JWT 二次使用返回 Unauthenticated」的单元/集成测试。

9. **dummyPasswordHash 首次调用存在时序差异**  
   - 位置：`internal/app/client/account.go:151-159`、`account.go:300-303`  
   - 描述：`sync.OnceValue` 首次调用会计算 bcrypt 哈希，后续调用直接返回缓存值，导致「用户不存在」路径首次响应耗时明显长于后续请求。  
   - 影响：首次失败请求可与「密码错误」路径形成可测量时序差异。  
   - 建议：在进程启动或测试初始化时预热 `dummyPasswordHash()`。

10. **`TestAccount_CreateTOTPFactor_RequiresJWTSecret` 未遵循 `testing.Short()` 约定**  
    - 位置：`internal/app/client/mfa_test.go:108-140`  
    - 描述：该集成测试没有 `if testing.Short() { t.Skip(...) }`，导致 `go test -short ./internal/app/client/...` 在无 DB 环境时失败。  
    - 影响：本地/CI 快速检查不可用。  
    - 建议：补充 `testing.Short()` 跳过逻辑。

### 🟢 P3

11. **SignUp 频控在 project 校验前执行，非本项目请求也占额度**  
    - 位置：`internal/app/client/account.go:183-186` 在 `projectRepo.GetProject` 之前调用 `checkSignUpRateLimit`  
    - 影响：攻击者可用无效 project_id 消耗目标 IP 的注册额度。  
    - 建议：频控键增加 `project_id` 维度，或在校验 project 存在后再计数。

12. **多处 TODO/F11 备注属于已知债务**  
    - 位置：`internal/api/clientgrpc/account.go:382-383` 等  
    - 影响：仅为记录，不构成本轮修复缺陷。

---

## 3. 模块总体结论

- **修复完成度**：约 **70%**。F1 核心安全项（Magic URL 不透明化、TOTP 锁定/防重放、改密/邮箱撤销会话、SignUp 频控、邮箱校验、匿名升级、prefs 限制）已落地；但一次性 JWT 消费、MFA 删除接口契约、邮箱变更 staging、未注册邮箱节流豁免、会话上限等关键项仍处于部分或未修复状态。
- **剩余风险 Top 3**：
  1. **CreateJWT 一次性可被重放**（P1）：生成侧已登记，验证侧未消费，实际安全边界未形成。
  2. **DeleteFactor gRPC 接口阻断**（P1）：已验证 MFA 因子无法通过 API 删除，影响用户自助管理。
  3. **UpdateAccount 改邮箱未验证即生效**（P1）：与 F1-6 方案要求不符，削弱账号接管防护。
- **是否建议关闭本模块审查**：**不建议关闭**。上述 P1 项及 F1-9.6/F1-9.7 尚未修复，需补充 gRPC 契约、验证器消费逻辑、配置项与集成测试后再做一次回归复审。

---

## 验证记录

- `go vet ./internal/app/client/... ./internal/infra/auth/... ./internal/pkg/contexts/... ./pkg/jwtparser/... ./pkg/grpc/interceptor/...`：通过，无告警。
- `go test -short ./internal/app/client/... ./internal/infra/auth/... ./internal/pkg/contexts/... ./pkg/jwtparser/... ./pkg/grpc/interceptor/...`：
  - `internal/infra/auth`、`internal/pkg/contexts`、`pkg/jwtparser`、`pkg/grpc/interceptor`：通过。
  - `internal/app/client`：因 `TestAccount_CreateTOTPFactor_RequiresJWTSecret` 未在 short 模式跳过，且环境缺少 `TORCHWOOD_TEST_ADMIN_DATABASE_SOURCE` 而失败（非本次修改直接引入，但属于测试约定缺陷）。
