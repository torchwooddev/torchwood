# 复审报告：10 - Proto 定义与代码生成（Round 2）

> 审查范围：`proto/`、`buf.yaml`、`buf.gen.yaml`、`genproto/`、`sdk/typescript/src/` 及 interceptor/server 注册代码。  
> 审查基准：`docs/review/prompts/10-proto-codegen.md`、`docs/review/fix-plan.md` F11 与 F8-2 章节。  
> 代码版本：当前工作区（`HEAD` 在 `1288705` 之后，含 `c4d0bcb`、`8b9c86e` 等提交）。

---

## 1. 修复验证结论表

| 修复项 | 结论 | 关键证据 | 说明 |
|---|---|---|---|
| F11-1 OpenAPI 产物认证元数据 | ✅ 已修复 | `buf.gen.yaml:18-20`（openapiv2 插件输出 JSON）；所有 service proto 均声明 `openapiv2_swagger`，含 `apiKey`/`Bearer`/`cookie` 三个 scheme；`genproto/client/v1/account.swagger.json:1771-1796` 可见 `securityDefinitions` 与全局 `security`；公开方法在 operation 级声明 `security: []` + `x-torchwood-access`（`proto/client/v1/account.proto:65-71`）。 | 认证元数据已稳定生成；method_auth access level 通过手工 `openapiv2_operation` 映射到 operation 级扩展，未自动透传但结果一致。 |
| F11-2 TS SDK 与 proto 一致性 + 契约测试 | ⚠️ 部分修复 | `proto/server/v1/functions.proto:67-115` 16 个 RPC 全部映射到 `sdk/typescript/src/server/functions.ts:16-190`；`proto/client/v1/account.proto:62-354` 34 个 RPC 全部映射到 `sdk/typescript/src/client/account.ts:15-458`；契约测试 `sdk/typescript/src/__tests__/contract.test.ts:22-313` 本地 14/14 通过。 | SDK 与 proto 已对齐，契约测试也存在且通过；但 `.github/workflows/ci.yml:63-88` 未运行 TS SDK 测试，`Taskfile.yml:140-144` 的 `test` 也仅依赖 `test-sdk-go`。 |
| F11-3 REST 保留字路径段遮蔽资源 id | ✅ 已修复（方案 B） | `proto/server/v1/databases.proto:140-150` 仍使用字面量 `count`/`bulk` 路径，但 `internal/app/server/databases.go:21-27` 在 CreateDocument 时校验并拒绝 `count`/`bulk`；`internal/app/functions/management.go:19-25` 同样拒绝 `runtimes`/`specifications`；测试 `internal/app/server/databases_reserved_test.go` 覆盖。 | 未改为 `:count`/`:bulkUpdate` 自定义方法风格，而是通过服务端创建校验阻止保留字 id，符合 fix-plan 的备选方案。 |
| F11-4-1 101/143 方法补方法级 method_auth | ❌ 未修复 | 143 个 RPC 中仅 42 个显式声明 `method_auth`（如 `proto/client/v1/account.proto:62-354` 全部显式），其余 101 个依赖 `service_auth` 默认值（`proto/server/v1/functions.proto:62` 默认 `ACCESS_API_KEY`）；敏感方法 `SetVariables`/`GetVariables`（`proto/server/v1/functions.proto:100-103`）、`CreateFileToken`（`proto/server/v1/storage.proto:100`）、`CreateUserToken`（`proto/server/v1/users.proto:93`）、APIKeys 全服务均未方法级标注。 | 因所有 service 都设置了 `service_auth`，`internal/infra/server/grpc.go:183-185` 会回退到默认值，server 仍可启动；但 method_auth 覆盖率远低于 fix-plan 目标，敏感方法也未显式声明。 |
| F11-4-2 API key scope 映射去硬编码 | ❌ 未修复 | `pkg/grpc/interceptor/apikey_scope.go:20-111` 仍是硬编码的 `apiKeyScopeRules`；`proto/shared/v1/authz.proto:17-19` 的 `MethodAuth` 只含 `access`/`permissions`，未携带 scope 资源名/读写方向；`internal/infra/server/grpc.go:116-119` 的启动断言仅检查方法是否被分类到 public/apiKey/permission，不检查 scope 表与 proto 的一致性。 | 当前硬编码表与 proto 的 82 条 ACCESS_API_KEY 方法对齐，但无推导逻辑、无启动期一致性断言，新增 RPC 需人工同步两处。 |
| F11-4-3 error.proto 映射补齐 | ✅ 已修复 | `proto/shared/v1/error.proto:7-18` 注释已列出映射；`internal/infra/server/errors.go:34-35` `Aborted→ERROR_CODE_CONCURRENT_MODIFICATION`、`:40-41` `ResourceExhausted→ERROR_CODE_QUOTA_EXCEEDED`、`:42-43` `DeadlineExceeded→ERROR_CODE_TIMEOUT`。 | gRPC code 到 ErrorCode 的映射已落地。 |
| F11-4-4 时间戳统一 + optional update 字段 | ⚠️ 部分修复 | `google.protobuf.Timestamp` 已覆盖 `created_at`/`updated_at`/`expires_at`/`expire_at`/`invited_at`/`joined_at`（如 `proto/client/v1/account.proto:384-385`、`proto/server/v1/users.proto:148-149`）；`UpdateAccountRequest.name/email`（`proto/client/v1/account.proto:421-422`）、`UpdateUserRequest.status/name/email`、`UpsertOAuthProviderRequest.client_secret` 已 optional。 | 仍有缺口：`UpdateCollectionRequest.name` 为普通 `string`（`proto/server/v1/databases.proto:197`），`UpdateAdminRequest.role` 为普通 `string`（`proto/console/v1/admins.proto:120`），无法区分「不修改」与「清空」。 |
| F11-4-5 buf lint/breaking 接入 CI | ⚠️ 部分修复 | `buf.yaml:25-27` 已启用 `breaking: use: FILE`；本地 `buf lint` 通过；`Taskfile.yml:18-22` `generate-proto` 包含 `buf lint`。 | `.github/workflows/ci.yml` 未单独执行 `buf lint` 或 `buf breaking --against`；`proto/` 中无任何 `reserved` 声明，「删除字段一律 reserved」的规范尚未体现。 |
| F11-4-6 敏感字段「仅一次返回」注释 | ✅ 已修复 | `proto/client/v1/account.proto:581` TOTP `secret` 标注「仅创建响应返回明文 secret」；`proto/server/v1/apikeys.proto:95` API key `secret` 标注「明文 API key secret，仅创建响应返回一次」；`proto/server/v1/storage.proto:195-196` file `token`、 `proto/server/v1/users.proto:170` user token `access_token` 等均已标注。 | 关键 secret/token 字段已补注释；少量通用 token 字段（如 `TokenBundle.access_token/refresh_token`）未逐字段标注，但已在响应语义中自然表达。 |
| F8-2 DeleteSessions keepCurrent 无法传递 | ✅ 已修复 | `proto/client/v1/account.proto:123-127` 使用 `delete: "/v1/account/sessions"`，未加 `body: "*"`；`genproto/client/v1/account.pb.gw.go:268-301` 从 query 参数读取 `keep_current`；`sdk/typescript/src/client/account.ts:103-108` 通过 `query: { keep_current: String(keepCurrent) }` 发送。 | 采用「query 绑定」替代「DELETE body」，proto、gateway、TS SDK 三者一致，`keepCurrent` 可正常传递。 |

**统计**：✅ 5 项、⚠️ 3 项、❌ 2 项、🔴 0 项。

---

## 2. 新发现问题

### 🟠 P1

1. **方法级 `method_auth` 覆盖率严重不足，敏感方法仍继承 service 默认值**
   - 位置：`proto/server/v1/functions.proto:100-103`、`proto/server/v1/storage.proto:100`、`proto/server/v1/users.proto:93`、`proto/server/v1/apikeys.proto:60-72`；`internal/infra/server/grpc.go:183-185`。
   - 问题：143 个 RPC 仅 42 个显式声明 `method_auth`，其余 101 个靠 `service_auth` 默认值兜底。`SetVariables`、`GetVariables`、`CreateFileToken`、`CreateUserToken`、APIKeys 等敏感方法均未方法级标注。
   - 影响：proto 无法成为权限/OpenAPI/scope 的单一事实来源；未来调整 service 默认值时容易一次性暴露或锁死整组接口。
   - 建议：按 fix-plan 先补齐敏感方法的方法级 `method_auth`，再逐步覆盖全部 101 个 RPC。

2. **CI 未接入 `buf lint` 与 TS SDK 契约测试**
   - 位置：`.github/workflows/ci.yml:63-88` 无 `buf lint`/`buf breaking` 步骤，也无 `sdk/typescript` 测试；`Taskfile.yml:140-144` 的 `test` 仅依赖 `test-sdk-go`。
   - 问题：生成管线违规、proto 与 SDK 脱节、OpenAPI 安全元数据漂移均无法被 CI 拦截。
   - 影响：本轮 F11-1/F11-2 的修复成果容易在后续提交中回退。
   - 建议：在 CI backend job 增加 `buf lint` 与 `buf breaking --against <main>`；新增 frontend/SDK job 或在 backend job 中运行 `cd sdk/typescript && npm test`。

3. **REST 保留字路径仍使用字面量段，gateway 路由冲突未根除**
   - 位置：`genproto/server/v1/databases.pb.gw.go:2206-2214`（`documents/count`、`documents/bulk`）；`genproto/server/v1/functions.pb.gw.go:1325-1329`（`functions/runtimes`、`functions/specifications`）；`internal/app/server/databases.go:21-27` 仅校验新创建。
   - 问题：字面量路由仍优先于 `{document_id}`/`{function_id}` 通配路由；校验只能阻止新数据，无法修复已存在的保留字 id。
   - 影响：若历史数据已存在 `id="count"` 的文档，REST 下的 Get/Update/Delete 会命中 CountDocuments 或 BulkUpdate，导致 404/405/语义错误。
   - 建议：迁移到 `:count`/`:bulkUpdate` 自定义方法风格；若保持当前方案，需在升级说明中提示并清理旧数据。

4. **OpenAPI `x-torchwood-access` 扩展依赖手工维护，未从 `method_auth` 自动透传**
   - 位置：`shared/v1/openapi.proto` 仅作注释、未被任何 service proto import；每个公开方法手写 `openapiv2_operation`（如 `proto/client/v1/account.proto:65-71`）。
   - 问题：method_auth 与 swagger operation 扩展之间没有生成期或启动期一致性校验。
   - 影响：新增/修改方法时，可能出现 proto 权限与 OpenAPI 文档不一致。
   - 建议：在 protoc 插件或生成后脚本中自动注入 operation 级扩展，或在启动期断言 swagger 扩展与 `collectMethodsByAccess` 结果一致。

5. **API key scope 仍为硬编码表且无启动期一致性断言**
   - 位置：`pkg/grpc/interceptor/apikey_scope.go:20-111`；`internal/infra/server/grpc.go:116-119`。
   - 问题：scope 资源名与读写方向完全来自 Go 代码，proto `method_auth` 不携带 scope 信息。
   - 影响：新增 `ACCESS_API_KEY` RPC 必须同时修改 `apiKeyScopeRules`，否则会被 fail-closed 拒绝；但漂移不会提前发现。
   - 建议：在 `MethodAuth` 中扩展 `scope`/`op` 字段并由 proto 推导；或在启动期校验 `apiKeyScopeRules` 与 `apiKeyMethods` 的集合相等性。

6. **`UpdateCollectionRequest.name` 与 `UpdateAdminRequest.role` 未 optional**
   - 位置：`proto/server/v1/databases.proto:197`；`proto/console/v1/admins.proto:120`。
   - 问题：更新类请求中，普通 `string` 无法区分「未提供（不修改）」与「提供空串（清空）」。
   - 影响：调用方必须传 name/role 才能更新同消息里的其他字段。
   - 建议：参照 `UpdateAccountRequest` 标为 `optional string`。

### 🟡 P2

7. **`shared/v1/openapi.proto` 为 dead code**
   - 位置：`proto/shared/v1/openapi.proto:1-34`。
   - 问题：文件仅描述建模约定，未被任何 service proto import，生成的 `openapi.pb.go` 也无业务引用。
   - 影响：增加维护负担，开发者可能误以为 OpenAPI 选项由此文件驱动。
   - 建议：将其内容合并到开发者文档并删除该 proto；若保留，应被 service proto 实际 import 以强制约束。

8. **时间戳字段 JSON wire 格式发生 breaking change**
   - 位置：`c4d0bcb`；`genproto/client/v1/account.swagger.json:1354` 显示 `expiresAt` 类型为 `string`/`date-time`。
   - 问题：`expires_at`/`expire_at` 等字段从 `int64` unix 毫秒变为 RFC3339 字符串。
   - 影响：已发布的外部客户端/旧 SDK 若仍按 int64 解析会失败；commit message 已标注 breaking，但版本号仍为 v1、无 migration 说明。
   - 建议：在发布说明/changelog 中显式声明；对外提供版本协商或保留兼容字段。

9. **proto 中无任何 `reserved` 声明**
   - 位置：全 `proto/` 目录 `grep reserved` 无结果。
   - 问题：虽然当前未删除字段，但「删除字段一律 reserved」的规范未落地。
   - 影响：后续删除/重命名字段时容易破坏兼容性且 buf breaking 无法提前拦截。
   - 建议：将 reserved 使用纳入 code review checklist；后续删除字段时必须声明 reserved。

### 🟢 P3

10. **TS SDK 方法名过度缩写增加理解成本**
    - 位置：`sdk/typescript/src/server/functions.ts:34` `create` 对应 `CreateFunction`，`:49` `list` 对应 `ListFunctions` 等；已通过 `contract.test.ts` 显式映射。
    - 问题：方法名与 RPC 名不完全对应，新开发者需查映射表。
    - 建议：保持现有映射并在 SDK 文档中提供 RPC→方法名对照表。

---

## 3. 模块总体结论

- **修复完成度估计**：约 **55%–60%**。OpenAPI 安全元数据、错误映射、时间戳转换、敏感字段注释、F8-2 路径/SDK 同步等已完成；但 method_auth 覆盖率、API key scope 去硬编码、optional update 字段补齐、CI 接入等核心项仍未完成。
- **剩余风险 Top 3**：
  1. **method_auth 覆盖率不足**：101/143 方法依赖 service 默认值，敏感方法未显式声明，proto 尚不能作为权限单一事实来源。 
  2. **scope 硬编码且无一致性断言**：新增 RPC 需手工维护 `apiKeyScopeRules`，漂移风险高。 
  3. **CI 未覆盖 buf lint 与 TS 契约测试**：已落地的 F11-1/F11-2 成果可能在后续提交中不知不觉回退。
- **是否建议关闭本模块审查**：**不建议关闭**。F11-4-1 与 F11-4-2 是 fix-plan 明确项且尚未完成；CI 接入与 optional 字段缺口也需要补齐。建议在完成上述项后，再组织一次 focused review。
