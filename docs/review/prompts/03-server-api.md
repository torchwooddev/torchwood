# 审查任务：03 - Server API 传输层（servergrpc + serverhttp）

## 角色

你是资深 Go 后端代码审查专家。对 Torchwood 项目（Appwrite 风格的 AI/Agent-Native BaaS 平台）的「Server API 传输层」做一次**只读**审查。**不得修改任何代码**，只输出审查报告。

## 必读上下文

- 仓库根目录：`D:\Codes\qiulin\torchwood`
- 先读 `AGENTS.md`（开发约定）与 `docs/roadmap.md` §0（AI/Agent-Native：Server API 供 Agent/自动化通过 scoped API Key 调用）
- 架构：`internal/api/servergrpc` 是 Server API 的 gRPC handler（管理面：Projects/API Keys/Users/Teams/Databases/Collections/Attributes/Indexes/Documents/Functions/OAuth Providers/Storage），经 grpc-gateway 暴露为 `/v1/server/*` REST；`internal/api/serverhttp` 是自定义 HTTP handler（Storage 上传下载、Functions 代码包、OAuth 回调）
- 约定：每个 gRPC 方法必须带 proto authz 注解（`proto/shared/v1/authz.proto` 的 `method_auth`，缺失会导致 `collectMethodsByAccess` 报错）；列表查询复用 `pkg/crud` 抽象，不手拼 SQL；鉴权由 `pkg/grpc/interceptor` 完成，handler 只接收 Principal
- 典型调用链：gRPC handler → app use-case → domain repo port → infra adapter

## 审查范围

- `internal/api/servergrpc/`（全部 `*.go`，含测试）
- `internal/api/serverhttp/`：`file_handler.go`、`functions_handler.go`、`oauth_handler.go`（含 `file_handler_integration_test.go`、`file_handler_uploads_test.go`）
- 交叉引用（只读）：`internal/app/server/`、`internal/app/storage/`、`internal/app/functions/`（用例层，用于核对校验与错误映射是否在正确层次）、`proto/server/v1/*.proto`（RPC 定义与 authz 注解）

## 审查重点

1. **输入校验层次**：请求字段校验（分页参数、ID 格式、枚举值、字段名/类型）是否在 handler 层充分执行；非法输入是否返回合理错误而非透传到用例层/DB；是否信任了客户端可伪造的字段（如 `created_by`、权限数组、项目 ID）。
2. **authz 注解完整性**：对照 `proto/server/v1/*.proto`，抽查每个 RPC 是否声明 `method_auth`（access level + permissions），权限命名是否与 scope 体系（`users.write`、`databases.write` 等）匹配，有没有「注解说需要权限但 handler 实际不检查」或反之。
3. **错误映射**：用例层错误（NotFound/Conflict/PermissionDenied/InvalidArgument）是否正确映射为 gRPC 状态码与结构化错误体（`proto/shared/v1/error.proto`）；错误信息是否泄露内部细节（SQL 错误、文件路径）。
4. **列表/分页**：`page`/`limit`/`cursor`/`order` 参数的解析与上限；是否复用 `pkg/crud` 而非手写。
5. **文件上传/下载**（`file_handler.go`）：multipart 大小上限、Content-Type 校验、文件名与路径处理（防路径穿越）、分片上传（`file_handler_uploads.go`）的 part 校验（大小上限、数量上限）、complete 的幂等性与互斥、下载的鉴权（file token、bucket public 的边界）。
6. **Functions 代码包**（`functions_handler.go`）：上传大小限制、zip 解压的安全（zip bomb、slip）、与 infra/functions 的衔接。
7. **OAuth 回调**（`oauth_handler.go`）：redirect_uri 校验（防 open redirect）、state 验证、错误页面处理。
8. **上下文与 Principal**：handler 是否正确从 context 取 Principal（而非自己解析 header）；`X-Torchwood-Project` 头是否被正确用于项目定位。
9. **并发**：handler 内的共享状态、长耗时操作（是否应在 worker 执行）、请求上下文取消传播。

## 通用检查项

1. 安全：注入、越权、路径穿越、信息泄露、输入校验
2. 错误处理：错误吞掉、错误状态码映射不当、panic 未 recover
3. 性能：无谓的复制、大请求体加载到内存、N+1
4. 一致性：与 proto 定义一致（字段命名、路径、body）、与 AGENTS.md 约定一致
5. 测试：每个 RPC 是否有测试；测试是否断言鉴权与错误路径

## 输出要求

用简体中文输出审查报告，按严重级别分组：

- 🔴 **P0 严重**：越权、注入、路径穿越、敏感信息泄露
- 🟠 **P1 高**：功能缺陷、校验缺失、错误映射错误
- 🟡 **P2 中**：代码质量、可维护性、性能隐患
- 🟢 **P3 低**：风格、命名、微小改进

每条问题必须给出：`文件路径:行号` + 问题描述 + 影响/风险 + 修复建议（不实际修改）。
最后给出模块总体评价（校验充分性、鉴权一致性、最需优先修复的 3 项）。

## 验证方式

- 可运行 `go vet ./internal/api/servergrpc/... ./internal/api/serverhttp/...` 辅助检查
- 集成测试需要本地 Postgres/Redis/MinIO，**不要运行**；可阅读测试了解既有覆盖
