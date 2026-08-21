# E-2b 合并 client/server `message Document`（规格）

> Wave 3。对应 `docs/review/first-principles-plan.md` E-2b、`docs/review/first-principles-design.md` §11 E-2b。  
> 依赖 E-2a（Documents 单核 + 策略）已合入。日期：2026-08-21。**可施工。**

## 锁定

1. **只合并** `message Document`。今日两侧字段完全相同：`id=1`、`data=2`、`created_at=3`、`updated_at=4`、`permissions=5`、`version=6`。迁到 `proto/shared/v1/document.proto`，package `torchwood.shared.v1`。字段号 1–6 原样保留，禁止复用或重排。
2. Client/Server RPC 返回类型改为 `shared.v1.Document`；`ListDocumentsResponse.documents` 改为 `repeated shared.v1.Document documents = 1`（字段号保持 1）。
3. **继续 v1**：不 bump package 到 v2。Client/Server 仍是 `torchwood.client.v1` / `torchwood.server.v1`。
4. JSON **载荷**兼容：HTTP JSON 字段名与编号不变。
5. **Breaking**（有意 FILE breaking）：
   - gRPC / protobuf 全名：`torchwood.client.v1.Document` / `torchwood.server.v1.Document` → `torchwood.shared.v1.Document`
   - OpenAPI `$ref`：`#/definitions/torchwoodclientv1Document` / `#/definitions/torchwoodserverv1Document` → `#/definitions/v1Document`（grpc-gateway 插件把 shared 消息编成 `v1Document`）
   - Go 生成类型：RPC 返回 `*sharedv1.Document`（SDK Go 源码 breaking）
   - gRPC 方法描述符 output type 全名变化（unary payload 字段仍兼容）
6. 删除 client/server 本地 `message Document` 时 **不** reserved 整个消息名、**不**留空壳包装（会改变 JSON）。直接删消息、RPC 改返回 `shared.v1.Document`。CI 未跑 `buf breaking --against`。
7. TypeScript SDK 的 `Document` 是手写 JSON 接口（`sdk/typescript/src/types.ts`），字段名不变则不必为对齐而改 TS 类型。
8. 领域类型 `internal/domain/databases.Document` **不**改成 proto 类型；handler 只投影。

## 做

- 新增 `proto/shared/v1/document.proto`；`task generate-proto`；提交生成结果。
- 更新 Go 调用点：`internal/api/{client,server}grpc/databases.go`、`sdk/go/{client,server}` 及测试。
- 规格页与 `docs/developer/09-api-guide.md` Breaking change 说明。

## 不做

- 不合并 `ListDocumentsRequest` / `GetDocumentRequest` / Create/Update/Upsert/Delete 请求（两侧字段号或字段集不同）。
- 不动 `Transaction` / `TransactionOp`。
- 不砍 RPC、不改 201 方法面。
- 不改 ident / businessSchema / sentinel / projectschema / E-5 系统表。
- 不给系统集合加 `_version`，不改默认 `read:any`。
- 禁止手改 `genproto/**`。

## 版本策略

| 面 | 是否兼容 |
|---|---|
| proto package | 仍 v1，不 bump v2 |
| REST JSON body | 字段名/编号兼容 |
| OpenAPI `$ref` | breaking（`#/definitions/v1Document`） |
| Go SDK / genproto 类型 | breaking（`*sharedv1.Document`） |
| gRPC wire payload 字段 | 兼容；descriptor 全名 `torchwood.shared.v1.Document` breaking |
| TypeScript SDK JSON 接口 | 字段名不变则兼容 |

## 验收

- Client/Server `DatabasesService` 文档 RPC 返回 `torchwood.shared.v1.Document`。
- HTTP JSON 文档对象仍是 `id` / `data` / `created_at` / `updated_at` / `permissions` / `version`。
- `go test ./internal/api/clientgrpc/... ./internal/api/servergrpc/... ./sdk/go/client/... ./sdk/go/server/...` 绿。
- 请求消息字段号未改；`ListDocumentsRequest` 仍分包。
