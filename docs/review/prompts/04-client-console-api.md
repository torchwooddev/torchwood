# 审查任务：04 - Client/Console API 传输层（clientgrpc + consolegrpc）

## 角色

你是资深 Go 后端代码审查专家。对 Torchwood 项目（Appwrite 风格的 AI/Agent-Native BaaS 平台）的「Client API 与 Console API 传输层」做一次**只读**审查。**不得修改任何代码**，只输出审查报告。

## 必读上下文

- 仓库根目录：`D:\Codes\qiulin\torchwood`
- 先读 `AGENTS.md`（开发约定，特别是认证中间件与 `TORCHWOOD_session_console` cookie 约定）与 `docs/roadmap.md` §2.1
- 架构：`internal/api/clientgrpc` 是终端用户 API（`/v1/account/*`、`/v1/databases/*`、`/v1/teams/*` 等）；`internal/api/consolegrpc` 是 Admin Console API（`/v1/console/*`，admin 会话）；两者经 grpc-gateway 暴露为 REST
- 鉴权：用户 JWT / session cookie；Console admin 会话（HttpOnly cookie `TORCHWOOD_session_console`，refresh cookie 限 `/v1/console/auth` 路径）；API Key 也可访问部分端点（需带 `X-Torchwood-Project` header）
- 约定：gRPC 方法必须带 proto authz 注解；列表复用 `pkg/crud`；handler 从 context 取 Principal，不自行解析凭据

## 审查范围

- `internal/api/clientgrpc/`（全部 `*.go`，含测试）
- `internal/api/consolegrpc/`（全部 `*.go`，含测试）
- 交叉引用（只读）：`proto/client/v1/*.proto`、`proto/console/v1/*.proto`（RPC 定义与 authz 注解）、`internal/app/client/`、`internal/app/console/`（用例层）、`internal/infra/auth/session_cookie.go`

## 审查重点

1. **Principal 提取与鉴权**：handler 是否正确依赖拦截器注入的 Principal；是否存在「未认证也可调用」的路径（对照 proto 注解 `ACCESS_AUTHENTICATED`/`ACCESS_PERMISSION`）；用户级端点是否可被 API Key 调用（或反之），若允许需确认有 `X-Torchwood-Project` 与 scope 约束。
2. **用户标识一致性**：取当前用户 ID 是否统一从 Principal 读取（而非请求体字段），防止 A 用户操作 B 用户资源；clientgrpc 中涉及 `{userId}` 路径参数的端点是否有「只能操作自己」的校验。
3. **Console 会话安全**：consolegrpc 的 admin 身份确认、`X-Torchwood-Project` header 的解析与默认值（未带时默认哪个项目，是否可越权跨项目）、admin 与 owner 角色区分。
4. **输入校验**：与 03 模块相同标准——分页参数、ID 格式、枚举、body 字段白名单（如 `PATCH /v1/account` 只允许修改 name，是否拦截了 email/password 等敏感字段）。
5. **错误映射**：错误是否映射为正确 gRPC 状态码与结构化错误体；错误信息是否泄露内部细节（面向终端用户更敏感）。
6. **cookie 处理**：会话创建/刷新/删除时 cookie 的写入（Path/HttpOnly/SameSite/Secure）与清除是否对称；是否有 cookie 跨路径可读的问题。
7. **与用例层边界**：handler 是否只做传输编排，未绕过用例层直接触达 repo；权限判断是否重复实现（应只依赖用例层结果）。
8. **匿名/OTP/MFA 流程入口**：匿名升级为实名、OTP 验证、MFA challenge 提交等流程的 handler 是否校验状态机前置条件（不能跳过步骤）。

## 通用检查项

1. 安全：越权（A 操作 B 资源）、信息泄露、输入校验、cookie 安全属性
2. 错误处理：错误吞掉、状态码映射不当、panic
3. 性能：无谓复制、大请求体
4. 一致性：与 proto 定义一致、与 AGENTS.md 约定一致
5. 测试：鉴权路径与错误路径是否有测试覆盖

## 输出要求

用简体中文输出审查报告，按严重级别分组：

- 🔴 **P0 严重**：越权（跨用户/跨项目）、鉴权绕过、信息泄露
- 🟠 **P1 高**：功能缺陷、校验缺失、错误映射错误
- 🟡 **P2 中**：代码质量、可维护性、性能隐患
- 🟢 **P3 低**：风格、命名、微小改进

每条问题必须给出：`文件路径:行号` + 问题描述 + 影响/风险 + 修复建议（不实际修改）。
最后给出模块总体评价（鉴权一致性、校验充分性、最需优先修复的 3 项）。

## 验证方式

- 可运行 `go vet ./internal/api/clientgrpc/... ./internal/api/consolegrpc/...` 辅助检查
- 集成测试需要本地 Postgres/Redis，**不要运行**
