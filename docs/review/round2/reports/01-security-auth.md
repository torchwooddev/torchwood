# Round 2 复审报告：01 - 安全与认证

> 审查范围：`pkg/grpc/interceptor/*`、`internal/infra/auth/*`、`pkg/jwtparser/*`、`pkg/secretbox/*`、`pkg/password/*`、`cmd/server/provides.go`、`.env.example`。
> 以当前工作区代码（HEAD）为准；`go vet` 与无外部依赖单元测试均通过。

## 1. 修复验证结论表

| 编号 | 修复项 | 结论 | 证据（当前代码位置） | 说明 |
|------|--------|------|----------------------|------|
| F1-2 | account token 校验非原子 | ✅ 已修复 | `internal/infra/auth/account_token_redis.go:106` 使用 `s.rdb.GetDel(ctx, key).Bytes()`；`:102-103` 注释说明原子消费 | GETDEL 实现校验+删除原子化；失败/不存在均返回明确 `Unauthenticated`/`Internal` |
| F1-3 | MFA 登录无防重放/锁定 | ✅ 已修复 | `internal/infra/auth/totp.go:111-130` `verifyCode` 统一调用 `checkFactorLock`、`recordFactorFailure`、`claimUsedCode`；`ValidateTOTP:100-106` 与 `VerifyTOTPFactor:81-98` 均走该路径；`internal/app/client/mfa.go:265-277` `CompleteMFASession` 增加 `checkMFACompleteRateLimit` 频控 | 60s 防重放（SETNX 占 3 个时间窗口）、15min/5 次锁定、Complete 频控均落地；锁定按 factor ID 维度，注册/登录复用同一状态函数 |
| F1-4 | TOTP secret 与 JWT 共用主密钥 | ✅ 已修复 | `internal/infra/auth/totp.go:49-51` `totpKey()` 用 `jwtparser.DeriveKey(master, "totp")` 派生；`:134-146` `decryptSecret` 先读 TOTP 域密钥，失败回退旧主密钥；`:89-96` 校验成功后重加密迁移 | 双密钥读窗口避免存量 TOTP 失效；测试 `totp_test.go:179-221` 覆盖迁移；cookie (`PurposeSessionCookie`)、file token (`PurposeFileToken`)、OTP HMAC key 均已域分离 |
| F2-1 | API Key 全量 scope 越权 console AdminsService | ✅ 已修复 | `pkg/grpc/interceptor/jwt.go:144-155` `permissionMethods` 分支在 `:147-150` 直接拒绝 `CredentialTypeAPIKey`；`internal/api/consolegrpc/admins.go:40-46` `requireAdminActor` 兜底要求 `ActorKind == Admin`；各 handler `:48-102` 均调用 | 测试 `jwt_auth_test.go:142-171` 断言 `*`/`all` scope 调 4 个 AdminsService 方法均返回 `PermissionDenied`；回归测试 `internal/api/consolegrpc/...` 通过 |
| F2-4-a | extractCredential 多凭证并存时拒绝 | ✅ 已修复 | `pkg/grpc/interceptor/jwt.go:161-192` 当 `authorization` + `cookie`、`authorization` + `x-api-key`、`cookie` + `x-api-key` 并存时均返回 `errors.New("multiple credentials provided")`；`:95-97` 映射为 `Unauthenticated` | 测试 `jwt_auth_test.go:248-272` 覆盖三种并存组合，断言 `Unauthenticated` |
| F2-4-b | HTTP 鉴权三处重复抽公共辅助 | ⚠️ 计划外未做 | `internal/api/serverhttp/file_handler.go:738-786` 与 `internal/api/serverhttp/functions_handler.go:173-220` 仍各自维护 `authenticate`/`authorize` | fix-plan 中标注为 P2 且“可后置”（与 F6 协调），当前未抽取；行为未变，但保留重复代码与潜在漂移风险 |
| F7-5 | JWT 弱默认被启动校验接受 | ⚠️ 部分修复 | `cmd/server/provides.go:45-54` 调用 `validateJWTSecret`；`:71-91` 拒绝空值、<32 字符、精确匹配弱默认值；`:84-88` 对包含弱子串的密钥仅 `Warn`；`.env.example:8-9` 已改为强随机占位值 | 弱值检查为精确匹配，未阻断包含弱子串的长密钥（如 `change-me-in-production-0123456789abcdef` 仅告警即可通过）；建议将已知弱模式改为子串即拒绝 |
| F7-6 | 审计落库无超时 + 错误静默 | ❌ 未修复 | `pkg/grpc/interceptor/audit.go:70` 仍使用 `a.repo.Insert(context.Background(), entry)`；`:71-73` 虽记录 Warn 日志，但 `context.Background()` 无超时 | 审计写库可能因 DB hang 而阻塞 gRPC 响应返回；未引入超时/降级机制；fix-plan 分配给 F2 协调，但当前代码未落地 |

**统计**：✅ 5 项、⚠️ 2 项、❌ 1 项、🔴 0 项。

## 2. 新发现问题

### 🟠 P1

1. **HTTP handler 与 gRPC 多凭证策略不一致**
   - 位置：`internal/api/serverhttp/file_handler.go:766-786`、`internal/api/serverhttp/functions_handler.go:202-220`
   - 问题：gRPC 拦截器已拒绝“Authorization + Cookie + X-Api-Key”任意两种并存，但 HTTP `authenticate` 遇到多凭证时按固定优先级取第一个（X-Api-Key > Authorization > Cookie），不拒绝。
   - 影响：同一请求携带 API Key 与 admin session cookie 时，HTTP 侧以 API Key 身份处理，与 gRPC 侧行为漂移；上传/下载等 HTTP 入口可能出现凭证混淆。
   - 建议：抽取公共 `httpAuth` 辅助时统一采用与 `extractCredential` 相同的多凭证拒绝语义，或至少对存在多种凭证头的情况返回 401。

### 🟡 P2

2. **`extractCredential` 未拒绝同一 metadata key 的多个值**
   - 位置：`pkg/grpc/interceptor/jwt.go:228-234` `firstMetadataValue`
   - 问题：gRPC `metadata.MD` 允许一个 key 对应多个值，当前实现只取 `values[0]`，未对 `authorization` 等凭证头出现多个值的情况做拒绝。
   - 影响：网关/代理若将多 header 合并为一个 metadata key 的多值，攻击者可能通过附加第二个凭证头干扰解析（虽然当前只取第一个，但语义不明确）。
   - 建议：当 `len(values) > 1` 且 key 为凭证类头时返回“multiple credentials provided”。

3. **弱 JWT 密钥校验未覆盖子串场景**
   - 位置：`cmd/server/provides.go:79-88`
   - 问题：仅对 `weakJWTSecretTokens` 做 `lower == w` 精确拒绝；包含弱子串的密钥只 warn 不阻断。
   - 影响：用户可能在弱默认值后追加随机字符“绕过”校验（如 `change-me-in-production-0123456789abcdef`）。
   - 建议：将子串匹配也改为拒绝，或把典型弱占位完整串（如 `change-me-in-production`）加入精确黑名单。

### 🟢 P3

4. **`validateJWTSecret` 缺少单元测试**
   - 位置：`cmd/server/provides.go:71-91`
   - 问题：启动期关键安全校验无自动化测试覆盖，回归风险高。
   - 建议：新增 `provides_test.go`，断言空值、<32 字符、精确弱值、含弱子串值、正常随机值的行为。

## 3. 模块总体结论

- **修复完成度估计**：约 **75%**（8 项中 5 项完整修复，2 项部分/计划外，1 项未修复）。
- **剩余风险 Top 3**：
  1. **审计落库无超时（F7-6）**：所有受保护 RPC 都可能被审计写库拖慢或阻塞，未降级为异步/超时，是当前最大未修复风险。
  2. **HTTP 与 gRPC 凭证策略不一致**：上传/下载等 HTTP 入口未同步多凭证拒绝语义，存在凭证混淆漂移。
  3. **JWT 弱密钥校验可被绕过**：子串匹配仅告警，长弱密钥仍可通过启动校验。
- **是否建议关闭本模块审查**：**不建议关闭**。F7-6 必须修复并通过测试后方可考虑关闭；F7-5 建议补齐子串拒绝；F2-4-b HTTP 公共辅助虽为 P2 计划外，但建议与 F6 协调后完成，以消除重复实现漂移。
