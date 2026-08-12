# 审查任务：05 - Account 用例层（internal/app/client）

## 角色

你是资深 Go 后端代码审查专家（认证业务领域）。对 Torchwood 项目（Appwrite 风格的 AI/Agent-Native BaaS 平台）的「Account 用例层」做一次**只读**审查。**不得修改任何代码**，只输出审查报告。

## 必读上下文

- 仓库根目录：`D:\Codes\qiulin\torchwood`
- 先读 `AGENTS.md`（开发约定）与 `docs/roadmap.md` §2.1（Client Account/Auth 能力清单）
- 架构：`internal/app/client` 是终端用户认证与账号业务用例层，被 `internal/api/clientgrpc` 调用；依赖 `internal/domain/*` 端口与 `internal/infra/*` 适配器（bun repo、Redis auth store、storage、messaging）
- 能力范围：注册/登录、refresh、会话管理、prefs、匿名登录、Email/Phone OTP、Magic URL、邮箱验证、密码找回/重置、一次性 JWT、OAuth2、TOTP MFA（创建/验证/删除 + 登录挑战）、账号日志、Teams 客户端 API
- 安全约定：JWT claims 与 `pkg/jwtparser` 映射兼容；密码哈希用 `pkg/password`；OTP/state/challenge 存 Redis（`internal/infra/auth`）

## 审查范围

- `internal/app/client/`（全部 `*.go`，含单元测试与 `*_integration_test.go`）
- 交叉引用（只读）：`internal/domain/users/`、`internal/domain/auth/`（端口）、`internal/infra/auth/`（会话/OTP/TOTP 实现）、`internal/infra/bun/model/`（用户模型）、`proto/client/v1/account.proto`

## 审查重点

1. **注册/登录流程**：邮箱规范化（大小写/trim）、重复注册、登录错误信息是否区分「用户不存在/密码错误」（枚举风险）、密码校验强度（`validator.go` 策略是否在用例层应用）、账号锁定/节流是否生效。
2. **会话生命周期**：refresh 轮换与重用检测、登出撤销（access token 失效是否真实发生）、会话数量上限、删除会话的权限校验（只能删自己的）。
3. **密码重置与邮箱验证**：token 一次性、过期、与用户/会话绑定；重置后是否撤销全部会话；验证链接的构造是否防参数篡改。
4. **OTP 流程**：发送频率限制、验证次数限制、码熵与过期、验证后失效；Phone/Email 枚举防护（发送时是否泄露账号是否存在）。
5. **Magic URL / 一次性 JWT**：一次性、过期、用途绑定（不能把 login magic 当 password reset 用）；JWT 的 aud/scope 限制。
6. **MFA 流程**：TOTP 创建（secret 只返回一次）、验证绑定、登录 challenge 的完整状态机（未验证前不能获得完整会话）、删除 factor 需再次验证；`mfa_challenge` 的过期与防重放。
7. **匿名登录**：匿名升级为实名时的数据迁移与身份绑定；匿名 token 与正式 token 区分。
8. **prefs / 账号资料更新**：JSONB 校验（大小、类型）、`PATCH /v1/account` 字段白名单（不能通过该端点改 email/password/status）。
9. **账号日志**：敏感操作是否记录、日志内容是否泄露 PII 或 token。
10. **事务与一致性**：用户创建 + 会话创建 + 默认项目分配等复合操作的事务边界；失败时部分写入的回滚。
11. **客户端 Teams 用例**：创建/加入/退出团队的角色与级联（对照 roadmap §2.3 验收标准）。

## 通用检查项

1. 安全：越权（A 用户操作 B 会话/账号）、枚举、重放、信息泄露、输入校验
2. 错误处理：错误吞掉、错误类型区分合理（NotFound/Conflict/InvalidArgument）
3. 并发：check-then-act 竞态（如 OTP 验证与失效）、事务边界
4. 一致性：与端口签名一致、与 proto/AGENTS 约定一致
5. 测试：每个流程是否有单元测试；集成测试是否覆盖端到端路径

## 输出要求

用简体中文输出审查报告，按严重级别分组：

- 🔴 **P0 严重**：认证绕过、越权、账号接管、token 泄露
- 🟠 **P1 高**：功能缺陷、状态机漏洞、边界条件错误
- 🟡 **P2 中**：代码质量、可维护性、性能隐患
- 🟢 **P3 低**：风格、命名、微小改进

每条问题必须给出：`文件路径:行号` + 问题描述 + 影响/风险 + 修复建议（不实际修改）。
最后给出模块总体评价（认证流程健壮性、状态机正确性、最需优先修复的 3 项）。

## 验证方式

- 可运行 `go vet ./internal/app/client/...` 辅助检查
- 集成测试（`*_integration_test.go`）需要本地 Postgres/Redis，**不要运行**；可阅读测试了解既有覆盖
