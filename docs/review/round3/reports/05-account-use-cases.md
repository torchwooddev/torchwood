# Round 3 全量只读审核：05 - Account 用例层

> 审查范围：`internal/app/client/` 全部用例与测试；交叉对照 `internal/domain/auth`、`internal/infra/auth`、`proto/client/v1/account.proto`、`internal/api/clientgrpc/account.go`。
> 基准：当前工作区 HEAD；对照 `docs/review/prompts/05-account-use-cases.md`、Round 2 报告 `docs/review/round2/reports/05-account-use-cases.md`、B1 任务书 `docs/review/round2/backlog-next-round.md`。
> **只读**：未修改任何源代码。本环境无 shell，未执行 `go test`；集成测试按源码与断言阅读覆盖。

---

## 摘要

Account 用例层相对 Round 2 有实质跃迁。B1 邮箱 staging（`pending_email` + `email_change` token + `ConfirmEmailChange`）已按 A 档落地，且验收测试覆盖「旧邮箱可登录 / 新邮箱不可 → 确认后反转」「token 一次性」「新邮箱占用」「撤会话失败不切邮箱」。Round 2 的一次性 JWT 消费、DeleteFactor `code` 契约、未注册邮箱节流豁免、会话上限、先撤会话后提交、Magic URL secret 不回传等均已闭合。

本轮未发现 P0/P1（认证绕过、越权接管、token 明文回传）。残留为 P2/P3：Magic URL 响应形态可枚举邮箱、改邮箱路径缺发送频控、匿名升级不摘 `anonymous` 标签、account-token 错 secret 亦 GETDEL、JWT 团队角色默认只取 50 条 membership。

产品决策偏差（相对 B1 原文「USER 级」）：`ConfirmEmailChange` 为 `ACCESS_PUBLIC`（点邮件链接即完成），与 recovery 同模型；已在 proto 与 backlog-fix-report 记录，不按缺陷计。

**Verdict：有条件通过（Pass with P2）。** 无阻断项；建议本模块本轮可关闭，P2 列入下一轮。

---

## 已核实健康

### B1 邮箱 staging 验收对照

| 验收项 | 结论 | 证据 |
|--------|------|------|
| 改邮箱写 `pending_email`，不写 `email` | ✅ | `internal/app/client/account.go:454-477` |
| `url` 为空拒绝 | ✅ | `account.go:457-458`；测试 `account_g3_test.go:501-516` |
| 向**新邮箱**发确认信，向**旧邮箱**发安全通知 | ✅ | `account.go:515-528`、`:541-547`；`account_g3_test.go:146-152` |
| staging 不撤会话；Confirm 时先撤后提交 | ✅ | `account.go:503-510`、`:597-608`；`account_security_test.go:103-105` |
| Confirm 走 GETDEL，返回 record 新邮箱 | ✅ | `internal/infra/auth/account_token_redis.go:82-84,120-139`；purpose `email_change`：`internal/domain/auth/account_token.go:12,26-30` |
| 占用检查 + 唯一索引兜底 | ✅ | `account.go:578-589,610-612`；`account_g3_test.go:380-411` |
| `email_verified=true`，清 `pending_email` | ✅ | `account.go:601-607`；`account_g3_test.go:325-326` |
| 确认前旧邮箱可登录/找回，新邮箱不可 | ✅ | `account_g3_test.go:303-316`；SignIn/Recovery 均按 `email` 字段查，不读 `pending_email` |
| token 二次使用 Unauthenticated | ✅ | `account_g3_test.go:344-377` |
| purpose 隔离（email_change ≠ verification） | ✅ | `internal/infra/auth/account_token_redis_test.go`（`TestRedisAccountTokenStore_EmailChange`） |
| `mapUserDoc` / gRPC 不暴露 `pending_email` | ✅ | `account.go:821-833`；`internal/api/clientgrpc/account.go:532-544` |
| 系统集合含 `pending_email` 列 | ✅ | `internal/infra/documentdb/system_collection_specs.go:24-26` |
| proto + REST `PUT /v1/account/email-change` | ✅ | `proto/client/v1/account.proto:135-145,548-552` |
| 撤会话失败邮箱不变 | ✅ | `account_g3_test.go:414-451` |

相对任务书的**有意偏差**（不记缺陷）：

- 鉴权为 `ACCESS_PUBLIC` 而非 USER（`account.proto:133-137`）。use-case 不校验 principal（`account.go:551-554` 注释；`account_g3_test.go:454-498`）。安全模型 = 256-bit secret + 24h TTL + GETDEL。

### Round 2 遗留项回归

| Round 2 项 | 结论 | 证据 |
|------------|------|------|
| F1-1 Magic URL secret 不回传 | ✅ 保持 | `magic_url.go:84-91`；`magic_url_test.go:128-129` |
| F1-3 MFA 防重放/锁定 | ✅ 保持 | `mfa.go:275-277`；`mfa_test.go:181-197` |
| F1-5 DeleteFactor 需 code | ✅ 已补齐契约 | `proto/client/v1/account.proto:717-722`；`internal/api/clientgrpc/account.go:397-399`；`mfa.go:244-250`；`mfa_test.go:210-218` |
| F1-6 / R05-P1-2 邮箱未验证即生效 | ✅ 已修（B1） | 见上表 |
| F1-7 / G3-3 先撤会话后提交 | ✅ | 改密 `account.go:506-509`；recovery `recovery.go:129-137`；Confirm `account.go:597-600` |
| F1-8 一次性 JWT 验证侧未消费 | ✅ 已修 | `jwt.go:52,68-76`（`OneTime: true` + Register）；`internal/infra/auth/validator.go:161-171` Consume；`jwt_test.go:128-195` 二次使用拒绝 |
| F1-9.1 SignUp 频控 | ✅ | `account.go:154-180,204-208`（project 校验之后，含 project 维度） |
| F1-9.2 邮箱格式/长度 | ✅ | `email_otp.go:205-215` |
| F1-9.3 SignIn 时序枚举 | ✅ | `account.go:160-172` `init` 预热哑哈希；`:313-319` 不存在路径也 Verify |
| F1-9.4 prefs 大小限制 | ✅ | `account.go:725-742` |
| F1-9.5 匿名升级 | ✅ | `account.go:489-498` 空 hash 跳过 old_password；`account_security_test.go:123-163` |
| F1-9.6 未注册邮箱锁号 | ✅ 已修 | `account.go:313-319` 不 `recordLoginFailure`；`account_g3_test.go:234-260` |
| F1-9.7 会话无上限 | ✅ 已修（infra） | `internal/infra/auth/session_service.go:22-24,56-63,204-250`；`config.proto` `max_per_user` |
| P2-7 DeleteSessions 非事务 | ✅ 已修 | `session_service.go:170-201` `BulkDeleteDocuments` |
| P2-9 哑哈希首次时序 | ✅ 已修 | `account.go:170-172` |
| P2-10 MFA 集成测未 Short 跳过 | ✅ 已修 | `mfa_test.go:108-111` |
| Magic URL / 密码策略 / 系统集合只读 | ✅ | 见下文「已核实健康（流程）」 |

### 审查清单逐项（流程层）

| 重点 | 结论 | 要点 |
|------|------|------|
| 注册/登录 | 健康 | 邮箱 `ToLower+Trim`；重复 `AlreadyExists`+唯一索引；失败统一 `invalid credentials`；密码策略 8–72 + 字母数字（`internal/domain/users/password.go:10-35`）；节流 10/邮箱、30/IP / 15min |
| 会话 / refresh | 健康 | 轮换 + 重用删会话（`account.go:418-427`）；`refresh_rotation_test.go:66-126`；删会话校验 `user_id`（`account.go:765-776`）；登出删 session 文档，校验器按 SessionID 使 access 失效 |
| 重置 / 验证 | 健康 | purpose 隔离；GETDEL；重置先撤会话；链接 `userId+secret` 绑定 Redis key |
| OTP | 健康 | 发送 60s 冷却 + IP 10/h；校验 Lua 原子、5 次锁定、HMAC 存码（`otp_store_redis.go:20-26,31-53`）；发送不查用户是否存在（Email OTP 防枚举） |
| Magic URL / JWT | 大体健康 | secret 仅在邮件；JWT `OneTime` + Redis GETDEL；aud 与 end-user 同派生 key，绑定 SessionID |
| MFA | 健康 | secret 只在创建回传、落库 `enc:v1:`；未验证不发会话；删除 verified 需 TOTP；删除后 `RevokeByUser`；challenge 一次性（错码即消耗，属有意设计，见 `mfa_test.go:350-361`） |
| 匿名升级 | 功能可用 | 可设密码并 staging 邮箱；**未摘 `anonymous` 标签**（见 P2） |
| prefs / PATCH 白名单 | 健康 | 用例只改 name / pending_email / password_hash；status/email 不能经此直接改写；系统集合 Client 写全拒 |
| 账号日志 | 健康 | `logs.go:16-32` 按 actor 拉取，limit≤100；测试断言不含他人条目 |
| OAuth / 微信 | 健康 | 未验证邮箱拒绝（`identity.go:94-98`）；已有邮箱不自动绑（`:123-125`）；微信 unionid 跨端合并（`wechat.go:124-134`）；adapter 不填 Email，走占位邮箱 |
| 系统集合 | 健康 | 写路径全部系统集合拒绝；读路径拒绝 `users/sessions/identities`（`databases.go:92-98`）；`system_collections_readonly_test.go` |
| Client Teams | 基本健康 | 仅 owner 删队；邀请只能派 `member`；接受须 `email_verified`（`teams.go:121-135`）；缺本包测试（P3） |

---

## 🔴 P0 严重

无。

---

## 🟠 P1 高

无。

---

## 🟡 P2 中

### 1. Magic URL 创建响应可枚举已注册邮箱

- 位置：`internal/app/client/magic_url.go:71-73`（用户不存在返回空 `Challenge`）、`:91`（存在则返回 `challengeID+ExpireAt`）；对照 `recovery.go:69-70`（无论是否存在均 `return nil`）。
- 描述：测试 `magic_url_test.go:178-190` 名为 AntiEnumeration，只断言「不报错、不发信」。调用方可凭 `challenge_id` / `expire_at` 是否为空判断邮箱是否已注册（占位 `@torchwood.local` 亦走空响应，`:76-78`）。
- 影响：公开端点上的邮箱存在性枚举，与 recovery 的统一空成功不一致。
- 建议：存在与否一律返回结构相同的不透明 challenge（伪造 `challenge_id` + 固定/模糊 expire），或与 recovery 一样只返回空成功。

### 2. 改邮箱路径未走 account-token 发送频控

- 位置：`internal/app/client/account.go:515-528` 直接 `CreateEmailChangeToken` + `mailer.Send`。
- 对照：verification `verification.go:74-77`、recovery `recovery.go:57-60`、magic URL `magic_url.go:59-62` 均调用 `tokens.CheckSendRateLimit`。
- 描述：持有密码的调用方可对任意目标地址反复 `UpdateAccount` 发确认信（每次覆盖 Redis key，仅最新 secret 有效，但邮件仍发出）。
- 影响：认证后的邮件轰炸；60s 冷却未覆盖该路径。
- 建议：在签发 token 前对 `projectID + 新邮箱 + IP` 调用 `CheckSendRateLimit`。

### 3. 匿名升级不移除 `anonymous` 标签

- 位置：创建 `anonymous.go:53` `labels: []any{"anonymous"}`；升级 `account.go:443-488` 的 `updates` 不含 `labels`。
- 描述：设密码 / staging 邮箱后文档仍带 `anonymous`。`user_roles.go:33-35` 会把 `label:anonymous` 打进 JWT。
- 影响：依赖该 label 区分游客与实名的授权会把已升级用户继续当匿名；标签语义与真实身份长期不一致。
- 建议：密码写入成功后从 labels 去掉 `anonymous`；邮箱 Confirm 成功后再视产品决定是否加 `verified`（`user:{id}/verified` 已由 `email_verified` 驱动）。

### 4. account-token 先 GETDEL 再比对 secret，错误 secret 也会烧掉 token

- 位置：`internal/infra/auth/account_token_redis.go:122` GETDEL，`:136-138` 才比对 `SecretHash`。用例层 Magic URL / recovery / verification / email_change 全部走该路径。
- 描述：`magic_url_test.go:147-156` 已写明「错误 secret 会原子消费记录」。知道 `user_id`（UUID，可从团队成员、历史链接等泄露）即可对 recovery / magic / email_change / verification 做定点作废。
- 影响：可用性：合法邮件链接被作废，用户须重新申请（再叠加发送冷却）。非账号接管。
- 建议：Lua/WATCH：比对成功才 DEL；失败只递增 attempts，超过 N 次再删。

### 5. `ConfirmEmailChange` 先消费 token，占用检查或撤会话失败后无法用同一链接重试

- 位置：`internal/app/client/account.go:574-600`（`:574` Verify/GETDEL → `:578-589` 查重 → `:598-600` 撤会话 → `:601` 写 email）。
- 描述：注释已承认「token 已被原子消费，不可重试」。占用失败或撤会话失败时：`pending_email` 仍在，email 未切，链接作废。用户须再次 `UpdateAccount`（旧密码）重发。
- 影响：确认窗口内被抢注或会话删除瞬时失败时，体验中断；不是接管。与 B1「GETDEL 一次性」一致，属设计代价。
- 建议：查重（及用户存在性）放在 GETDEL 之前用只读 GET；仅在即将提交时消费。撤会话失败已有测试（邮箱不变），可考虑失败后保留 token 或自动重签。

### 6. JWT 团队角色加载不分页，默认最多 50 个 membership

- 位置：`internal/app/client/user_roles.go:48-52` `ListDocuments` 未设 `PageSize`、无翻页。
- 对照：`internal/infra/documentdb/postgres.go:891-898` PageSize≤0 回退 50，且 `maxQueryLimit=100`。
- 描述：接受邀请超过 50 个团队的用户，JWT 缺少部分 `team:{id}` / `team:{id}/{role}`。
- 影响：文档/团队 ACL 按 JWT 角色判定时出现「已是成员但无权限」。
- 建议：与 `ListSessions` 相同循环分页（PageSize=100 直至 `NextPageToken` 空）。

### 7. 停用账号登录错误信息可区分「不存在 / 已停用」

- 位置：`internal/app/client/account.go:292-293,319` 统一 `invalid credentials`；`:328-331` 对存在但 `!CanAuthenticate` 返回 `user account is not active`。
- 描述：密码正确且账号 `inactive`/`blocked` 时错误与「邮箱未注册」不同。
- 影响：可确认该邮箱对应一个被停用账号（需先猜中密码，或与密码错误路径组合探测）。
- 建议：对外仍返回 `invalid credentials`；停用态只打内部审计。

### 8. 无法取消 pending 邮箱；PUBLIC 确认后即可用新邮箱走 recovery

- 位置：`account.go:450` 仅当 `email != oldEmail` 才写入 pending；把 email 设回旧值是 no-op，`pending_email` 不会清除。Confirm 为 PUBLIC（`account.proto:135-137`）。Confirm 成功不向旧邮箱再发「已切换」通知。
- 描述：用户若把邮箱改到攻击者控制的地址（笔误/钓鱼），攻击者点链接即可切换并撤会话，随后对**新邮箱** `CreateRecovery` 重置密码。旧邮箱通知只在 staging 时发一次。
- 影响：需用户主动改邮箱到攻击者收件箱，不是无交互接管。PUBLIC 为产品决策。缺少取消与切换后通知，缩短了补救窗口。
- 建议：支持明确取消（清 `pending_email` 并删 token）；Confirm 后再通知旧邮箱；可选 Confirm 后短时间内禁止 recovery / 要求仍用旧会话。

---

## 🟢 P3 低

1. **Client Teams 无本包测试**  
   `internal/app/client/teams.go` 的 owner 删除、邀请角色限制、`email_verified` 才能接受等分支无 `*_test.go`。

2. **Confirm 不校验文档 `pending_email` 与 token 邮箱一致**  
   `account.go:590-607` 只按 token 记录写 `email`。UpdateDocument 失败但邮件已发出时仍能确认（通常可接受）；若运营侧清掉 pending 后旧 token 未过期，仍会切邮箱。

3. **`pending_email` 在 token 过期后残留**  
   仅 Confirm 成功时置 `nil`（`account.go:605`）。过期或用户放弃后字段一直在，响应不暴露，属脏数据。

4. **CreateTOTPFactor 不限制 pending/verified 数量**  
   `mfa.go:126-142` 只清过期 pending，可堆积多个 TOTP。

5. **无客户端 IP 时跳过注册/匿名频控**  
   `account.go:176-177`、`anonymous.go:65-68`。代理未注入 ClientInfo 时额度失效。

6. **`CreateOAuth2LinkTokenSession` 只要求「已登录」，不校验调用者 == `LinkUserID`**  
   `oauth2.go:102-118`。登录用 state 误拿到 link 端点会给 OAuth 用户建会话（token 丢弃）。应校验 `principal.UserID == oauthState.LinkUserID`。

7. **微信 `findOrCreateUserByEmail` 若未来填了 Email 会按邮箱自动并号**  
   `wechat.go:88-97`。当前 adapter（`oauth_wechat.go:189-207`）不解析 email，风险未激活；建议与 Google 一样：无 identity 时禁止按未验证邮箱并号。

8. **UpdateAccount 无法清空 name**  
   proto `optional name`（`account.proto:540`），用例 `if cmd.Name != ""`（`account.go:444-446`），handler 用 `GetName()`（`clientgrpc/account.go:88-89`）丢失 presence。

9. **OTP 冷却在发信前 SetNX**  
   `otp_store_redis.go:76-83`。mailer/SMS 失败后 60s 内无法重发。

---

## 模块结论

**认证流程健壮性**：高。注册/登录枚举面、refresh 重用检测、敏感变更先撤会话、OTP/Magic/MFA/JWT 一次性、OAuth 拒未验证邮箱、系统集合只读，均有测试锚点。

**状态机正确性**：邮箱变更 A 档 staging 闭环；MFA「有因子不发会话 → Complete 才发会话」正确；匿名升级密码立即生效、邮箱仍 staging，与 B1 一致。主要缺口是匿名标签生命周期，以及 Magic URL 创建响应与 recovery 的枚举语义不一致。

**最需优先处理的 3 项**（均为 P2，无 P0/P1）：

1. Magic URL 创建响应对已注册邮箱的枚举（P2-1）
2. 改邮箱补齐发送频控（P2-2）
3. 匿名升级摘掉 `anonymous` 标签（P2-3）

**是否建议关闭本模块审查**：**建议关闭本轮**。B1 与 Round 2 P1 均已核实落地；剩余 P2 不构成认证绕过，可进下一轮修复清单。

---

## 验证记录

- 静态通读 `internal/app/client/*.go` 及对应 `*_test.go`。
- 交叉阅读 token/session/validator/OTP/proto/handler。
- **未运行** `go test -short ./internal/app/client/...`（本审查环境无 shell）。集成测试需 PG/Redis，按任务要求未跑；覆盖度以测试源码为准。
