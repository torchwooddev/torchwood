# Agent 默认工具箱

> Overlay，不是新 API。完整产品面仍是 **185 个 RPC**（Client 61 + Server 114 + Console 10），Agent 默认仅暴露 **18 个动词**。权威映射：`sdk/go/server/tools.go:42`（`Tools`）与 `sdk/typescript/src/server/tools.ts:34`（`agentTools`）；规格：`docs/review/wave3-e7-tool-catalog.md`。OpenAPI 以 `genproto/**/*.swagger.json`（`buf.gen.yaml:19` 的 `openapiv2`）为权威。
> 修订记录：2026-08-23 重写（以 `sdk/go/server/tools.go`、`sdk/typescript/src/server/tools.ts` 的 18 条 `TOOL_*`/`Tool*` 为准，核对 185 总数与 `genproto` swagger）。

---

## 1. 定位

- **不是**第 186 个 RPC，也**不是**把产品面砍到 20 动词；
- Agent / 自动化默认只看见下表；Console、CLI、SDK 仍走完整 Server API；
- 逃生舱仍是 `InvokeJSON(fullMethod, protojson)`（`sdk/go/server/invoke.go:20`）：覆盖全部 `torchwood.server.v1.*` unary，继续排除 `APIKeysService`；
- 本 catalog **不含** create/list/get/delete API key——密钥只在 Console 或带合适 scope 的管理流程里创建，不交给普通 Agent 工具面；
- 全量 185 的计数口径：`proto/client` + `proto/server` + `proto/console` 的全部 `rpc` 条目，见 `genproto/**/*.swagger.json` 的 `operationId`（`{Service}_{RPC}`）；
- 新增 RPC 后，overlay 是否收录由产品决策，完整 API 由 `InvokeJSON` 自动覆盖，无需改动工具箱即可调用。

---

## 2. 默认 18 个工具（`TOOL_*` / `Tool*`）

顺序与 `sdk/go/server/tools.go:42` 的 `Tools` 及规格文档一致，catalog 只读（Go `toolsByName`、TS `Object.freeze` + `Map`）。

| 工具名 | Server RPC | gRPC FullMethod | 输入要点 |
|--------|------------|-----------------|----------|
| `list_users` | `Users.ListUsers` | `/torchwood.server.v1.UsersService/ListUsers` | `page_size` / `page_token` / `queries[]string`（`shared.v1.ListRequest`） |
| `get_user` | `Users.GetUser` | `/torchwood.server.v1.UsersService/GetUser` | `id` |
| `create_user` | `Users.CreateUser` | `/torchwood.server.v1.UsersService/CreateUser` | `email`, `password`；可选 `name` / `status` / `labels` / `prefs` |
| `query_documents` | `Databases.ListDocuments` | `/torchwood.server.v1.DatabasesService/ListDocuments` | 必填 `database_id`、`collection_id`。**优先** `query`（`shared.v1.Query` AST，见 §3）；仍接受 `queries[]string` + 分页。两者冲突 → `InvalidArgument` |
| `get_document` | `Databases.GetDocument` | `/torchwood.server.v1.DatabasesService/GetDocument` | `database_id`, `collection_id`, `document_id` |
| `create_document` | `Databases.CreateDocument` | `/torchwood.server.v1.DatabasesService/CreateDocument` | `database_id`, `collection_id`, `document_id`, `data`；可选 `permissions` |
| `update_document` | `Databases.UpdateDocument` | `/torchwood.server.v1.DatabasesService/UpdateDocument` | 同上三元组；可选 `data` / `permissions` / `increment`。用户集合须带 `version`（OCC） |
| `upsert_document` | `Databases.UpsertDocument` | `/torchwood.server.v1.DatabasesService/UpsertDocument` | 三元组 + `data`；可选 `permissions`、`conflict_columns` |
| `delete_document` | `Databases.DeleteDocument` | `/torchwood.server.v1.DatabasesService/DeleteDocument` | 三元组；用户集合须带 `version`（OCC） |
| `list_collections` | `Databases.ListCollections` | `/torchwood.server.v1.DatabasesService/ListCollections` | `database_id`；可选 `queries[]string`、`page_size`、`page_token` |
| `get_collection` | `Databases.GetCollection` | `/torchwood.server.v1.DatabasesService/GetCollection` | `database_id`, `collection_id` |
| `invoke_function` | `Functions.CreateExecution` | `/torchwood.server.v1.FunctionsService/CreateExecution` | `function_id`；可选 `deployment_id`、`data`、`async` |
| `list_files` | `Storage.ListFiles` | `/torchwood.server.v1.StorageService/ListFiles` | `bucket_id`；可选 `queries[]string`、`page_size`、`page_token` |
| `get_file` | `Storage.GetFile` | `/torchwood.server.v1.StorageService/GetFile` | `bucket_id`, `file_id` |
| `grant_asset` | `Assets.Grant` | `/torchwood.server.v1.AssetsService/Grant` | `owner_id`, `def_code`, `quantity`, `idempotency_key`；可选 `expires_at` / `level` / `metadata` / `ref_type` / `ref_id` |
| `list_user_assets` | `Assets.ListUserAssets` | `/torchwood.server.v1.AssetsService/ListUserAssets` | `owner_id`；可选 `page_size`、`page_token` |
| `get_order` | `Payments.GetOrder` | `/torchwood.server.v1.PaymentsService/GetOrder` | `order_id` |
| `get_health` | `Health.Check` | `/torchwood.server.v1.HealthService/Check` | 无入参（`ACCESS_PUBLIC`） |

> 上传分片、OAuth 回调、Realtime WebSocket 为自定义 HTTP，不在本表，也不可经 `InvokeJSON` 调用。完整字段以 `tools.go:42` 的 `InputNotes` 与对应 proto 为准。

---

## 3. `query_documents` 双栈

`ListDocumentsRequest` 同时承载两套查询：

- `optional shared.v1.Query query`：`filter` 树 + `orders` + `page_size` / `page_token`（权威分页为 `page_token`，AIP-158）；
- 旧栈 `queries[]string` + `page_size` / `page_token`（Appwrite DSL，如 `equal("status","active")`，由 `pkg/query` 解析）。

两者同时提供且冲突 → `InvalidArgument`。gRPC/HTTP 的请求形状**不是**同一 JSON 换命名风格。

### 3.1 gRPC / `InvokeJSON` / `InvokeTool`（protojson，camelCase）

Agent 默认填 AST（完整 `ListDocumentsRequest`）：

```json
{
  "databaseId": "app",
  "collectionId": "notes",
  "query": {
    "filter": { "eq": { "attribute": "status", "values": ["active"] } },
    "pageSize": 20
  }
}
```

旧客户端只填 `queries` 仍有效：`{"databaseId":"app","collectionId":"notes","queries":["equal(\"status\",\"active\")"]}`。

### 3.2 HTTP（grpc-gateway）

| 方式 | 路径 | 载荷 | 说明 |
|------|------|------|------|
| GET 旧栈 | `GET /v1/server/databases/{database_id}/collections/{collection_id}/documents` | query：`queries` / `page_size` / `page_token` | TS `tw.server.databases.listDocuments()` 只覆盖这条 |
| POST AST | `POST /v1/server/databases/{database_id}/collections/{collection_id}/documents:list` | path 带库/集合 id；**body 是 `shared.v1.Query` 本身**（非整包 `ListDocumentsRequest`） | `genproto` 中 `DatabasesService_ListDocuments2`，OpenAPI 为 `POST .../documents:list` |

TS SDK 尚无 `documents:list` 封装；需 AST 时直接 `fetch` 该路径或走 Go `InvokeTool`。不要把 §3.1 的 `InvokeJSON` 示例原样 POST 到 REST。

---

## 4. 调用方式

### 4.1 Go（`sdk/go/server/tools.go:144` + `invoke.go:20`）

```go
import "github.com/torchwooddev/torchwood/sdk/go/server"

srv, _ := server.New("127.0.0.1:9060", server.WithAPIKey(key), server.WithProjectID("default"))

// 按工具名（catalog 校验）
tool, ok := server.LookupTool("query_documents") // Tool{ Name, FullMethod, InputNotes }
out, err := srv.InvokeTool(ctx, "query_documents", reqJSON) // 等价于 InvokeJSON(ctx, tool.FullMethod, reqJSON)

// 逃生舱：任意 Server unary（排除 APIKeysService）
respJSON, err := srv.InvokeJSON(ctx, "/torchwood.server.v1.UsersService/ListUsers", []byte(`{"pageSize":10}`))
```

`Tools` 顺序锁定（`tools.go:40` 注释），`LookupTool` 读 `init` 时拷贝；`InvokeTool` 未命中返回 `torchwood: unknown tool "<name>"`。

### 4.2 TypeScript（`sdk/typescript/src/server/tools.ts:34`）

```ts
import { agentTools, lookupAgentTool, TOOL_QUERY_DOCUMENTS } from "@torchwood/sdk";

const tool = lookupAgentTool("query_documents"); // { name, fullMethod, inputNotes } | undefined
console.log(agentTools.map(t => t.name)); // 18 个只读条目
// HTTP 调用仍走 tw.server.*（GET 旧栈）；AST 需手写 fetch 到 POST .../documents:list
```

TS 不提供 `InvokeJSON`；catalog 仅提供名字与 `fullMethod`，实际执行由宿主按 `fullMethod` 发 gRPC/HTTP。

### 4.3 其它 Server unary

除 `APIKeysService` 外，其余 Server unary 均可经 `InvokeJSON` 直调；Client / Console / APIKeys 走各自 SDK 或 Console，不入默认工具箱。

---

## 5. 完整 API 权威来源

- **Proto**：`proto/client/`、`proto/server/`、`proto/console/`、`proto/shared/`；
- **OpenAPI**：`task generate-proto`（`buf generate`）后 `genproto/**/*.swagger.json`（`buf.gen.yaml:19` 的 `openapiv2` 插件，`json_names_for_fields=true`，时间一律 `google.protobuf.Timestamp` → RFC3339）；
- **Scope**：每个 Server RPC 在 `internal/grpc/interceptor/apikey_scope.go:25` 有显式 `databases:read/write` 等映射，`AssertAPIKeyScopeCoverage` 在 `internal/infra/server/grpc.go` 启动期 fail-closed 校验；
- **计数**：185 = Client 61 + Server 114 + Console 10（以 `genproto` 的 `*.swagger.json` 中 `operationId` 去重计数为准，`buf breaking` 保障不兼容变更必经 `reserved`）。

Agent 集成建议：以 `genproto/**/*.swagger.json` 为 schema 权威生成工具 schema；`agentTools`/`Tools` 仅作默认 18 动词的便捷别名。

---

## 6. 边界与常见问答

**Q: 18 个够用吗？**

够做 80% 的 Agent 用例（查用户/读写文档/查集合/调函数/查文件/资产与订单）。剩余 20%（如建库/建属性/删数据）走 `InvokeJSON` 逃生舱，无需等工具箱收录。

**Q: 为什么不把 API Key 管理放进来？**

`APIKeysService` 被 `invoke.go:40` 的 `findServerMethod` 与 `tools.go` 双重排除：泄露的 Key 若能自铸新 Key，等同永久提权。密钥生命周期只在 Console 或带 `apikeys:write` 的受控流程里处理。

**Q: TS SDK 没有 `InvokeJSON` 怎么办？**

TS 属 `fetch` 层，`HttpTransport.request` 已支持 `auth:"apiKey"` 的任意路径；按 `genproto` 的 OpenAPI 路径直接 `fetch` 即可，或在 Node 侧复用 Go SDK 的 `InvokeJSON`。`agentTools` 只给 `fullMethod` 就是为宿主自选传输。

**Q: 新增 RPC 后要改动哪里？**

- Proto 层：按 `docs/developer/09-api-guide.md §1.4` 加 `method_auth` 与 `google.api.http`，字段删除必 `reserved`；
- 拦截器层：`apikey_scope.go:25` 登记 scope，否则 `AssertAPIKeyScopeCoverage` panic；
- 工具层（可选）：仅当产品决定收录为默认动词时，才在 `tools.go:42` / `tools.ts:34` 追加 `TOOL_*`；
- 生成物：`task generate-proto` 后提交 `genproto/**/*.swagger.json`，`buf breaking` 会拦住不兼容变更。

---

## 7. 本地验证

```bash
task generate-proto                          # 生成 genproto/**/*.pb.go + *.swagger.json
buf breaking --against '.git#branch=origin/main'  # 无 breaking change
golangci-lint run --new-from-rev=origin/main ./... # 棘轮 0 新增
go test ./sdk/go/server -run TestTools -v    # 校验 18 条 catalog 与 FullMethod 存在性
# OpenAPI 权威检查：每个 Server RPC 在 genproto/server/v1/*.swagger.json 有且仅有一条 operationId
```

关联文档：`sdk/README.md`（SDK 使用）、`docs/developer/09-api-guide.md`（Proto 注解与 OpenAPI 建模）、`docs/developer/12-sdk.md`（`InvokeJSON` 与 `FileTokenStore`）、`docs/review/wave3-e7-tool-catalog.md`（18 动词挑选依据）。
