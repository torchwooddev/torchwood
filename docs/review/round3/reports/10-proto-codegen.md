# 复审报告：10 - Proto 定义与代码生成（Round 3）

> 审查范围：`proto/` 全部、`buf.yaml` / `buf.gen.yaml` / `buf.lock`、`genproto/` 抽查、`sdk/typescript/src/` 类型与契约测试，交叉 `internal/infra/server/grpc.go`、`grpc_swagger_test.go`、`pkg/grpc/interceptor/apikey_scope.go`、handler 实现。
> 审查基准：`docs/review/prompts/10-proto-codegen.md`、`AGENTS.md` proto 规范、`docs/developer/09-api-guide.md` §1.3–§1.4、Round 2 报告。
> 方法：只读；**未**执行 `task generate-proto`。RPC/HTTP/authz 用 grep 全量枚举，genproto 与 SDK 抽查对照。

---

## 1. 摘要

本轮相对 Round 2 有实质收敛：**B3 自定义动词已落地**（proto / genproto / TS SDK / Console 一致，旧字面量路径在 `genproto/` 中零匹配）；**ConfirmEmailChange** 按产品决策标为 `ACCESS_PUBLIC`，同时具备 `method_auth` 与 `security: []` + `x-torchwood-access: public`；**时间字段统一 Timestamp**；更新类请求的关键 presence 字段已 `optional`；**swagger 与 `collectMethodsByAccess` 有启动期/单测断言**；**API key scope 有 fail-closed 集合相等断言**；CI 已跑 `buf lint` 与 TS SDK 契约测试。

144 个 RPC 全部带 `google.api.http`；14 个业务服务全部带 `service_auth`；缺注解会在 `collectMethodsByAccess` 启动失败。未发现 P0。无阻断级契约漂移。

残余集中在：OpenAPI 文档字段名（camelCase）与运行时 JSON（snake_case）双轨、CI 未跑 `buf breaking --against`、SDK 注释/类型少量滞后、文档仍引用旧路径。

---

## 2. Round 2 修复复核

| Round 2 项 | 本轮结论 | 关键证据 |
|---|---|---|
| F11-1 OpenAPI 认证元数据 | ✅ 保持 | 14 个 service proto 均声明三 scheme；公开方法 `security: []`；`internal/infra/server/grpc_swagger_test.go:59-165` 断言顶层/operation `x-torchwood-access` 与 `method_auth` 一致 |
| F11-2 TS SDK + 契约测试 | ✅ 已补齐 | `sdk/typescript/src/__tests__/contract.test.ts` 覆盖 ConfirmEmailChange 与自定义动词路径；`.github/workflows/ci.yml:94-96` 跑 `npm run test`；`Taskfile.yml:147-150` 的 `test` 依赖 `test-sdk-ts` |
| F11-3 / B3 REST 保留字 | ✅ 已改为自定义动词（不再是方案 B） | `proto/server/v1/databases.proto:141-149` `:count`/`:bulkUpdate`/`:bulkDelete`；`proto/server/v1/functions.proto:67-70` `:runtimes`/`:specifications`；`proto/client/v1/databases.proto:105` `:count`；`genproto/` 无旧字面量路径 |
| F11-4-1 敏感方法 method_auth | ✅ 敏感方法已显式标注 | APIKeys 四方法、`SetVariables`/`GetVariables`、`CreateFileToken`、`CreateUserToken` 均有方法级 `method_auth`；其余合法继承 `service_auth` |
| F11-4-2 scope 去硬编码 | ⚠️ 表仍硬编码，但已有启动断言 | `pkg/grpc/interceptor/apikey_scope.go:25-116` 仍为表；`:191-221` `AssertAPIKeyScopeCoverage` 由 `grpc.go:72-74` 启动期调用 |
| F11-4-3 error.proto 映射 | ✅ 保持 | `error.proto:7-18` 注释与 `errors.go:26-43` 主路径一致 |
| F11-4-4 Timestamp + optional | ✅ Round 2 缺口已补 | `UpdateCollectionRequest.name`、`UpdateAdminRequest.role` 已 `optional`；时间字段均为 `Timestamp` |
| F11-4-5 CI buf lint | ⚠️ lint 已接入，breaking 未接入 | `ci.yml:64-65` `buf lint`；无 `buf breaking --against` |
| F11-4-6 敏感字段注释 | ✅ 保持 | API key secret、file token、TOTP secret、server TokenBundle 均标注仅一次回显 |
| F8-2 DeleteSessions keepCurrent | ✅ 保持 | `account.proto:172` DELETE 无 body；SDK query 传 `keep_current` |
| `openapi.proto` dead code | ✅ 已删除 | `proto/shared/v1/` 仅剩 `authz`/`common`/`error` |
| ConfirmEmailChange（B1） | ✅ 新产品决策已落入契约 | proto public + swagger 一致；见 §4.2 |

---

## 3. 已核实健康

1. **RPC / HTTP / 服务级 authz 全覆盖**  
   144 个 `rpc`、144 条 `google.api.http`。14 个业务服务均有 `service_auth.default_access`。`collectMethodsByAccess`（`internal/infra/server/grpc.go:176-210`）缺注解或 `ACCESS_PERMISSION` 无 permissions 即返回 error；`assertRegisteredMethodsHaveAuthz`（`:119-122`、`:138-174`）对已注册方法 fail-closed。Health Check 的 `additional_bindings`（`/v1/health` + `/v1/server/health`）由 swagger 测试用 `Check2` 回退解析。

2. **B3 自定义动词**  
   Server：`documents:count` / `documents:bulkUpdate` / `documents:bulkDelete` / `functions:runtimes` / `functions:specifications`。Client：`documents:count`。genproto gateway/swagger 与 TS SDK（`sdk/typescript/src/server/databases.ts`、`functions.ts`、`client/databases.ts`）、Console API 一致。`genproto/` 中 `documents/count`、`documents/bulk`、`functions/runtimes` 零匹配。

3. **ConfirmEmailChange 鉴权与 OpenAPI**  
   `proto/client/v1/account.proto:131-145`：`ACCESS_PUBLIC` + `openapiv2_operation` `security: []` + `x-torchwood-access: public`。注释写明与 recovery 同一安全模型（256-bit secret + 24h TTL + GETDEL）。`genproto/client/v1/account.swagger.json:52-85` 与之一致。产品决策见 `docs/review/round2/backlog-fix-report.md` §1.2。

4. **更新请求 proto3 optional**  
   `UpdateProjectRequest`、`UpdateAccountRequest.name/email`、`UpdateUserRequest.status/name/email/email_verified`、`UpdateCollectionRequest`、`UpdateAdminRequest.role`、`UpdateBucketRequest`、`UpdateFileRequest.name/mime_type`、`UpdateFunctionRequest`、`UpsertOAuthProviderRequest.client_secret` 均用 optional 表达 presence。Struct/map（prefs/labels/metadata）空值语义在注释中写明，属既有约定。

5. **时间字段**  
   `created_at` / `updated_at` / `expires_at` / `expire_at` / `invited_at` / `joined_at` 全部为 `google.protobuf.Timestamp`。残留 `int64` 均为 size / count / duration_ms / increment / expires_in，不是时间。

6. **删除字段 reserved**  
   全 `proto/` 无 `reserved`。当前消息字段号连续，未见已删未保留的空洞。规范已写入 `AGENTS.md` 与 `09-api-guide.md` §1.3；本轮无待补 reserved 的既成删除。

7. **method_auth ↔ swagger**  
   `grpc_swagger_test.go:18-36` 的 `businessFileDescriptors` 与 `NewGRPCServer` 入参一致。测试要求 ≥14 个业务 swagger、≥140 个 operation。公开方法必须 `security: []`（TS 契约测试 `contract.test.ts:300-309` 再断言一层）。

8. **handler 无 stub**  
   `internal/api/{client,server,console}grpc` 对全部 144 个 RPC 均有实现；仅嵌入 `Unimplemented*Server` 作前向兼容，未见只返回 Unimplemented 的业务方法。

9. **genproto 未见手工改动**  
   抽查 `account.pb.go`、`databases.pb.gw.go`、`functions.pb.go`：均为 `DO NOT EDIT`；protoc-gen-go v1.36.10 与 `buf.gen.yaml:3` 一致；注释与 proto 同源（自定义动词说明、ConfirmEmailChange 摘要）。产物布局与四个远程插件匹配（`*.pb.go` / `*_grpc.pb.go` / `*.pb.gw.go` / `*.swagger.json`）。`openapi.proto` 陈旧产物已清理。

10. **TS SDK 方法集合未落后**  
    `contract.test.ts:51-214` 登记全部 Server + Client RPC，含 `ConfirmEmailChange`、`CountDocuments`、bulk 动词、functions 自定义动词。HTTP 绑定用例含 `:count`/`:bulkUpdate`/`:bulkDelete` 与 `email-change`。

11. **分页约定**  
    列表优先复用 `shared.v1.ListRequest` / `ListResponseMeta`。文档/集合/文件等动态资源用 `queries` + `page_size`/`page_token`，与 `pkg/query` 一致。

---

## 4. 特别关注项

### 4.1 B3 自定义动词 HTTP 注解

已完成，无回归。

```141:149:proto/server/v1/databases.proto
    option (google.api.http) = { get: "/v1/server/databases/{database_id}/collections/{collection_id}/documents:count" };
    ...
    option (google.api.http) = { patch: "/v1/server/databases/{database_id}/collections/{collection_id}/documents:bulkUpdate", body: "*" };
    ...
    option (google.api.http) = { post: "/v1/server/databases/{database_id}/collections/{collection_id}/documents:bulkDelete", body: "*" };
```

`count`/`bulk`/`runtimes`/`specifications` 不再占用 `{document_id}`/`{function_id}` 命名空间。旧路径依赖文档/验收清单仍有残留，见 P3。

### 4.2 ConfirmEmailChange authz

与产品决策一致，**不缺** `method_auth`。

```131:145:proto/client/v1/account.proto
  // ACCESS_PUBLIC：邮件链接点开即完成（与 recovery 同一安全模型——256-bit
  // 随机 secret + 24h TTL + GETDEL 一次性消费），点链接无需登录态。
  rpc ConfirmEmailChange(ConfirmEmailChangeRequest) returns (Account) {
    option (google.api.http) = { put: "/v1/account/email-change", body: "*" };
    option (torchwood.shared.v1.method_auth) = { access: ACCESS_PUBLIC };
    option (grpc.gateway.protoc_gen_openapiv2.options.openapiv2_operation) = {
      security: {}
      extensions: {
        key: "x-torchwood-access"
        value: { string_value: "public" }
      }
    };
  }
```

公开拦截器对携带的失效 JWT 会忽略而非拒绝（`pkg/grpc/interceptor/jwt.go:77-86`），因此即便 TS SDK 未显式 `auth: "none"`，邮件链接流程在协议层仍可达。SDK 注释仍写「需登录态」，属文档漂移（P3），不是契约错误。

### 4.3 reserved / optional / Timestamp

见 §3.4–§3.6。Round 2 的 `UpdateCollectionRequest.name`、`UpdateAdminRequest.role` 缺口已关闭。

### 4.4 `grpc_swagger_test.go` 与 method_auth

测试存在且覆盖全部业务 swagger。服务默认 access 与顶层 `x-torchwood-access` 必须相等；operation 有效值必须等于 `resolveMethodAccess`。这关闭了 Round 2「扩展靠手工、无断言」的 P1。

### 4.5 每个 RPC 是否都有 authz

是——通过 **服务级 `service_auth` + 方法级覆盖**。51/144 显式 `method_auth`（Account 全量 35、Admins 5、APIKeys 4、Client Databases 3 个 public 读、Functions/Users/Storage 各敏感方法）。其余 93 个继承服务默认（Server 面 `ACCESS_API_KEY`、Client Teams `ACCESS_AUTHENTICATED`、Health/ConsoleAuth `ACCESS_PUBLIC`）。`ACCESS_AUTHENTICATED` 在收集器中落入 permission 集合并默认 `["users"]`（`grpc.go:196-200`）。**不会**因缺方法级注解而启动失败。这是文档化模式，不再按 Round 2 的 P1 计。

---

## 5. 问题清单

### 🔴 P0 严重

无。

### 🟠 P1 高

无。

### 🟡 P2 中

1. **OpenAPI 字段名为 camelCase，运行时 JSON 为 snake_case**
   - 位置：`buf.gen.yaml:21`（`json_names_for_fields=true`）；`internal/infra/server/errors.go:119-121`（`UseProtoNames: true`）；对照 `genproto/client/v1/account.swagger.json:1413` `expiresAt` vs 实际线格式 `expires_at`；`docs/developer/10-console.md:154` 已记载。
   - 问题：swagger 是 Agent-Native 对外契约，但第一方客户端与 gateway 使用 proto 名。`protojson` 反序列化同时接受两种名字，**请求** camelCase 仍可工作；**响应**只发 snake_case。按 swagger 生成的严格客户端会读不到 `createdAt`/`expiresAt`。
   - 影响：外部 Agent 若以 swagger.json 为唯一事实来源，响应解析失败或字段全空。
   - 建议：OpenAPI 插件改 `json_names_for_fields=false`，或 gateway 改为输出 camelCase 并同步第一方 SDK。二选一后用契约测试锁死。

2. **CI 仍未跑 `buf breaking --against`**
   - 位置：`.github/workflows/ci.yml:64-65` 仅 `buf lint`；`buf.yaml:25-27` 已 `breaking: use: FILE`。
   - 问题：lint 挡不住字段号复用、删除未 reserved、HTTP 路径破坏。Round 2 已记录「无 main 基线则仅 lint」；当前 main 已存在，仍未加 against。
   - 影响：破坏性变更可能合入 v1 而不被 CI 拦截。
   - 建议：backend job 增加 `buf breaking --against .git#branch=main`（或仓库内冻结的 image）。

### 🟢 P3 低

3. **TS `Project` 类型缺少 `description`**
   - 位置：`sdk/typescript/src/types.ts:90-96`；proto `proto/server/v1/projects.proto:97-104` 有 `description = 3`。create/update 入参已接受 description（`sdk/typescript/src/server/projects.ts:19-33`）。
   - 影响：调用方 TypeScript 上看不到响应里的 description。
   - 建议：`Project` 补 `description?: string`。

4. **ConfirmEmailChange 第一方 SDK 注释仍写「需登录态」**
   - 位置：`sdk/typescript/src/client/account.ts:96-108`（且未像 `updateVerification`/`updateRecovery` 那样传 `auth: "none"`）；`sdk/go/client/account.go:94-96`。
   - 问题：与 proto `ACCESS_PUBLIC` 及产品决策相反。功能上公开拦截器会忽略坏 JWT，邮件链接仍可用。
   - 建议：注释改为「免登录，secret 即凭证」；TS 与其它 public 方法对齐 `auth: "none"`。

5. **`TokenBundle.expires_at` 类型注释过时；`ListMeta` 缺 `prev_page_token`**
   - 位置：`sdk/typescript/src/types.ts:26-27` 仍写「protojson 将 int64 序列化为字符串」；`:1-4` 无 `prev_page_token`（`proto/shared/v1/common.proto:18`）。
   - 影响：误导接入方按 unix 毫秒解析；翻页 meta 不完整。
   - 建议：注释改为 RFC3339；`ListMeta` 补字段。

6. **ErrorCode 三个枚举未接入 HTTP 映射**
   - 位置：`proto/shared/v1/error.proto:29-32` 的 `VALUE_OUT_OF_RANGE` / `OPERATION_NOT_ALLOWED` / `SERVICE_UNAVAILABLE`；`internal/infra/server/errors.go:26-43` 未映射。`codes.OutOfRange` HTTP 为 400，但 `error_code` 仍是 `INTERNAL_ERROR`；`codes.Unavailable` HTTP 503，error_code 同样是 INTERNAL。
   - 影响：Agent 无法按可编程 error_code 区分 503/越界。
   - 建议：补映射，或在 `error.proto` 注释标明「预留、当前不产出」。

7. **`buf.yaml` 声明 protovalidate 但无任何 proto import**
   - 位置：`buf.yaml:5`；全 `proto/` 无 `buf/validate` 或 `validate.proto`。
   - 影响：多余依赖，可能让人误以为字段校验已由 proto 承担。
   - 建议：启用校验注解，或从 deps 删除。

8. **buf CLI 版本漂移**
   - 位置：`Taskfile.yml:10` 安装 `v1.63.0`；`.github/workflows/ci.yml:62` 使用 `v1.65.0`。
   - 影响：本地 lint 与 CI 规则细节可能不一致。
   - 建议：钉死同一版本。

9. **开发者文档 / 验收清单仍写旧字面量路径**
   - 位置：`docs/developer/08-functions.md:37-38`、`docs/roadmap.md:210-211`、`docs/manual-acceptance-checklist.md:151`。
   - 问题：契约已是 `:runtimes` / `:specifications` / `:count`，文档会把接入方导向 404。
   - 建议：与 `09-api-guide.md` §1.3 breaking 说明对齐。

10. **方法级 `method_auth` 覆盖率仍约 51/144；scope 仍不在 proto 内**
    - 位置：多数 Server RPC 仅靠 `service_auth`；`proto/shared/v1/authz.proto:17-19` 的 `MethodAuth` 无 scope/op。
    - 问题：proto 还不是 scope 的单一事实来源。风险已被 `AssertAPIKeyScopeCoverage` 降到「漏登记即无法启动」。
    - 建议：保持现状即可；若要 Agent 从 proto 读 scope，再扩展 `MethodAuth`。不必为覆盖率数字补 93 条重复注解。

11. **全库仍无 `reserved` 样本**
    - 位置：`proto/` grep `reserved` 无结果。
    - 问题：规范已写，删除字段时仍依赖人工记忆。当前无已删字段。
    - 建议：CR checklist 保留即可，无需为「有规范」而预埋假 reserved。

---

## 6. 模块结论

- **Agent-Native 就绪度**：契约层（authz、HTTP、OpenAPI security、自定义动词、Timestamp、optional update）已达到可对外的 v1 质量。主要缺口是 **swagger 字段名与线格式不一致**（P2），会直接伤害「用 swagger 生成调用」的 Agent。
- **最需优先处理的 3 项**：
  1. 统一 OpenAPI 与 gateway 的 JSON 字段名（P2-1）。
  2. CI 增加 `buf breaking --against`（P2-2）。
  3. 同步 SDK 类型/注释与过时文档路径（P3-3/4/5/9）。
- **是否建议关闭本模块审查**：**建议关闭**（无 P0/P1；Round 2 的 F11-3/B3、ConfirmEmailChange authz、optional/Timestamp、swagger 断言、敏感 method_auth、CI lint/TS 测试均已落地）。残余 P2/P3 可并入日常迭代，不必再开一轮 focused proto review。

**Verdict：通过（pass，无 P0/P1）**
