# 修复任务 F11：Proto/OpenAPI 契约修复

## 角色

你是资深 Protobuf/API 设计工程师，负责修复 Torchwood proto 定义与 OpenAPI 产物的契约缺陷。
方案详见 `docs/review/fix-plan.md` §11（F11 批次）。**只修本任务列出的问题**。

## 工作目录与必读

- 仓库根目录：`D:\Codes\qiulin\torchwood`（Windows，pwsh）
- 必读：`AGENTS.md`（proto 生成约定）、`docs/review/fix-plan.md` §11、
  `docs/roadmap.md` §0（Agent-Native 验收标准）、`buf.yaml`、`buf.gen.yaml`
- 审查报告（背景）：`docs/review/` 下的 10 报告

## 修复清单

1. **OpenAPI 产物无认证元数据**（P1）：
   - 现状：`genproto/**/*.swagger.json` 全部无 securityDefinitions/security，
     `method_auth` 的 access/scope 语义在 OpenAPI 中不可见 → roadmap §0「仅凭
     OpenAPI 即可被 Agent 调用」的验收标准落空。
   - 修复：
     a. `buf.gen.yaml` 的 openapiv2 插件参数确认/调整（如 `json_names_for_fields=true`，
        输出稳定 JSON）；
     b. 在 `proto/shared/v1/authz.proto`（或新 options 文件）引入
        `grpc.gateway.protoc_gen_openapiv2.options.openapiv2_swagger` 相关 options，
        全局声明 securityDefinitions（apiKey `X-API-Key`、Bearer、cookie），
        并定义自定义 extension（如 `x-torchwood-access`/`x-torchwood-scopes`）
        将 `method_auth` 透传到 operation 级；
     c. 对 143 个 RPC 中至少对 ACCESS_API_KEY 服务补 operation 级 security 声明。
2. **TS SDK 与 proto 脱节**（P1）：
   - 现状：`sdk/typescript/src/server/` 缺 `functions.ts`（16 个 RPC 全缺）；
     `src/client/account.ts` 缺 createAnonymousSession、createOAuth2LinkSession/Token、
     createVerification/Update、createRecovery/Update、MFA 全套、createJWT、magic-url
     2 个、listLogs 共 16 个方法；`src/server/storage.ts` 缺 updateBucket/updateFile/
     createFileToken/getStorageUsage；`src/server/teams.ts` 缺 getPrefs/updatePrefs。
   - 修复：补齐上述方法（对照 `proto/` 定义与 Go SDK `sdk/go/server`/`sdk/go/client`
     的实现风格）；在 CI 或 `sdk/typescript` 包内增加契约测试——遍历 proto RPC
     集合与 SDK 导出方法集合比对（可在 `docs/review` 或 `tests/` 下建一个小脚本/
     测试文件，参考 `sdk/go/server/invoke_test.go` 的完整性测试思路）。
3. **REST 保留字路径段遮蔽资源 id**（P1）：
   - 位置：`proto/server/v1/databases.proto:93-102`（`documents/count`、`documents/bulk`、
     `documents/bulk/delete`）、`proto/server/v1/functions.proto:20-23`（`functions/runtimes`、
     `functions/specifications`）、`proto/client/v1/databases.proto:42-45`（count）。
   - 现状：`document_id`/`function_id` 客户端可自选，若取名为 count/bulk/runtimes 等，
     grpc-gateway 字面量优先匹配 → 这些资源经 REST 永远无法访问。
   - 修复（二选一，推荐 A）：
     A. 改 AIP-136 自定义方法风格：`/documents:count`、`/documents:bulkUpdate`、
        `/documents:bulkDelete`、`/functions:runtimes`、`/functions:specifications`；
     B. 保留路径但在 Create 请求校验拒绝保留字 id。
     重新生成后核对 gateway 路由与 SDK 调用路径（**注意**：改路径会影响 TS/Go SDK
     与 Console 前端调用点，需同步更新）。
4. **P2 补强**：
   - 为敏感方法补方法级 `method_auth`（SetVariables/GetVariables/CreateFileToken/
     CreateUserToken/APIKeys 服务等）——需要先在 `proto` 中为这些方法显式声明，
     再在 `pkg/grpc/interceptor/apikey_scope.go` 核对 scope 规则一致（或建立
     启动期一致性断言：Go 映射 ⊆ proto 方法集）。
   - 错误模型：`proto/shared/v1/error.proto` 与 `internal/infra/server/errors.go`
     补齐 error_code 映射（Aborted→CONCURRENT_MODIFICATION、FailedPrecondition、
     ResourceExhausted→QUOTA_EXCEEDED、DeadlineExceeded→TIMEOUT）。
   - 时间戳建模统一：请求侧 int64 unix（apikeys.proto:35、users.proto:125、
     account.proto:225,309,374）与响应侧 Timestamp 不一致 → 统一建议记录为
     v1 兼容债（本轮可只补注释，不做破坏性变更，除非评估成本低）。
   - 更新类请求补 optional（UpdateAccountRequest.name/email、UpdateUserRequest、
     UpdateFileRequest）→ 支持清空语义（对齐 projects.proto 的 optional 用法）。
   - buf 门禁：`Taskfile.yml` 的 generate-proto 增加 `buf lint` 步骤；删除字段一律
     reserved；`buf.yaml` 调整 lint 规则到项目实际风格（如允许消息复用类规则并说明）。
   - 敏感字段注释：apikeys.proto secret、storage.proto FileToken.token、users.proto
     TokenBundle、functions.proto Variable.value、oauth_providers.proto client_secret
     补「仅一次返回/不回显」注释与 client_secret 空串语义说明。

## 约束

- 本批次改动大且需重新生成：完成后必须执行 `task generate-proto`、
  `go build ./...`，并同步更新受影响的 SDK（TS）调用点与 Console 前端路径
  （如路径变更）
- 若本地 buf 不可用，修改 proto 后标注「待 generate-proto」，并检查
  `buf lint` 的静态输出（如可运行）
- **不要**改动业务 handler 逻辑（除路径/注解对应的必要调整）
- 破坏性变更（路径修改、optional 化）必须在汇报中明确列出

## 验证

- `task generate-proto`（或说明不可用原因）
- `go build ./...`、`go vet ./internal/...`
- TS SDK：`npx tsc --noEmit`（sdk/typescript）
- 若 buf 可用：`buf lint`、`buf breaking --against .git#branch=main`（评估变更）
- 契约测试（新增）运行通过

## 输出

最终汇报：按清单逐项给出「改动文件:位置 + 改动摘要 + 验证结果」；列出破坏性变更
清单（路径/字段/语义）与需要同步的前端/SDK 改动。
