# 09 后端 API 开发指南

> 面向后端开发者：以 `ProjectsService` 为范例，走完 `proto→genproto→domain→app→infra→api→Wire` 全流程，并约定分页、错误与 OpenAPI 一致性。
> 源码：`proto/server/v1/projects.proto`、`internal/api/servergrpc/`、`internal/app/server/`、`pkg/crud/`、`internal/infra/server/grpc_swagger_test.go`。

## 1 调用链总览

```
gRPC handler (internal/api/*grpc) → app use-case (internal/app/*) → domain port (internal/domain/*, interface) → infra adapter (internal/infra/{bun,documentdb,storage})
```

每层只依赖下层接口；Wire（`cmd/server/provides.go→wire_gen.go`）按构造器类型自动装配。

## 2 步骤 1：proto 定义

`proto/` 四组：`server/v1`（管理面，API Key / Admin）/`client/v1`（终端用户）/`console/v1`（Console 专用）/`shared/v1`（`authz.proto`/`common.proto`/`document.proto`/`error.proto`/`query.proto`）。生成到 `genproto/`（禁止手改）。

以 `proto/server/v1/projects.proto:5` 为模板：

```proto
syntax="proto3";
package torchwood.server.v1;
import "google/api/annotations.proto";
import "google/google/protobuf/timestamp.proto";
import "shared/v1/authz.proto";
import "shared/v1/common.proto";
option go_package="github.com/torchwooddev/torchwood/genproto/server/v1;serverv1";
option (grpc.gateway.protoc_gen_openapiv2.options.openapiv2_swagger) = {
  security_definitions:{ security:{key:"apiKey" value:{type:TYPE_API_KEY in:IN_HEADER name:"X-API-Key"}}}
  security:{security_requirement:{key:"apiKey" value:{}}}
  extensions:{key:"x-torchwood-access" value:{string_value:"api_key"}}
};
service ProjectsService {
  option (torchwood.shared.v1.service_auth)={default_access:ACCESS_API_KEY};
  rpc CreateProject(CreateProjectRequest) returns (Project){ option (google.api.http)={post:"/v1/server/projects" body:"*"}; }
  rpc ListProjects(shared.v1.ListRequest) returns (ListProjectsResponse){ option (google.api.http)={get:"/v1/server/projects"}; }
  rpc GetProject(GetProjectRequest) returns (Project){ option (google.api.http)={get:"/v1/server/projects/{id}"}; }
  rpc UpdateProject(UpdateProjectRequest) returns (Project){ option (google.api.http)={patch:"/v1/server/projects/{id}" body:"*"}; }
}
```

### 2.1 鉴权注解（强制）

`proto/shared/v1/authz.proto:9`：`ACCESS_PUBLIC(1)`/`ACCESS_AUTHENTICATED(2)`/`ACCESS_PERMISSION(3)`/`ACCESS_API_KEY(4)`；`MethodAuth{access,permissions}` 扩展 `52001`，`ServiceAuth{default_access}` 扩展 `52002`。服务级 `service_auth` 可省略方法级，单方法可用 `method_auth` 覆盖（如 `console/v1/admins.proto:ListAdmins` `access:ACCESS_PERMISSION permissions:["owner","admin"]`）。未解析出 authz 的方法启动即 `missing auth policy`。

### 2.2 消息约定

- 更新类 `optional` 表达 presence：`optional string name=2;` 未传=不修改（`HasName()` 判别）；空串语义由 `UpdateCollectionRequest` 注释显式说明。
- 删除字段一律 `reserved`（字段号+字段名，禁止复用），`buf breaking --against '.git#branch=origin/main'` 门禁。
- 时间 `google.protobuf.Timestamp`（HTTP JSON RFC3339，`timestamppb.New`）。
- 列表统一 `shared.v1.ListRequest`/`ListResponseMeta`（`proto/shared/v1/common.proto:7`），勿重造分页字段。

## 3 步骤 2：生成

```bash
task generate-proto # buf lint + buf generate（buf.gen.yaml v2：go/gateway/grpc/openapiv2 → genproto，paths=source_relative）
```

产物：`*_grpc.pb.go`（`XxxServiceServer` + `Register`）、`*.pb.gw.go`（`RegisterXxxHandlerFromEndpoint`）、`*.swagger.json`（`json_names_for_fields`）、`* .pb.go` 描述符（供 `collectMethodsByAccess` 聚合鉴权）。

## 4 步骤 3：domain 端口

`internal/domain/projects/project.go` 纯 struct（`string/time.Time/map`，无 protobuf 类型），`repository.go` 接口：

```go
type Repository interface {
  CreateProject(ctx context.Context, p *Project) error
  GetProject(ctx context.Context, id string)(*Project,error) // 不存在→(nil,nil)，由上层映射 NotFound
  ListProjects(ctx context.Context)([]Project,error)
}
```

跨资源端口按需新增（如 `APIKeyRepository`）。所有 infra 实现以 `wire.Bind(new(domainauth.SessionService), new(*auth.SessionService))` 绑定（`internal/infra/provides.go`）。

## 5 步骤 4：app 用例

`internal/app/server/projects.go` 以 `XxxCommand` 解耦 proto：鉴权→校验→事务→错误映射。

```go
principal, ok := contexts.Principal(ctx) // interceptor 注入
if principal.ActorKind!=shared.ActorKindAdmin || !principal.IsPlatformAdmin { return PermissionDenied }
if cmd.Name=="" { return InvalidArgument }
err := db.RunInTx(ctx, func(txCtx context.Context) error {
  if err:=projectRepo.CreateProject(txCtx,p); err!=nil{return err}
  schema,_:=ident.ProjectSchemaName(p.ID)
  conn.ExecContext(txCtx, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s`, quoteIdent(schema)))
  return projectschema.Apply(txCtx, db, p.ID)
})
```

- 越权返回 `NotFound` 防枚举（`GetProject` 对非绑定项目伪装）。
- 撞名先查后返回 `InvalidArgument`/`AlreadyExists`，勿依赖裸 `unique_violation→500`。
- 所有预期错误 `status.Error(codes.X, msg)`（裸 `errors.New` 会被 gateway 包为 `Internal`）。

## 6 步骤 5：infra 适配

元数据 `bun`：`internal/infra/bun/model/project.go` + `bunrepo/project_repo.go:NewSelect().Where("id=?").Scan`（`sql.ErrNoRows→nil`），构造器 `func NewXxxRepository(db *clients.Database) xxx.Repository`。

动态文档仅业务集合走 `internal/infra/documentdb/postgres.go:NewPostgresDocumentDB(db,pub)`：`schema-per-database + _tenant + _perms`，`pkg/query` 优先（`equal/contains/search...`），字段白名单+敏感黑名单，未声明列→`InvalidArgument`；端口错误经 `internal/app/shared.MapDocumentDBError` 映射。

## 7 步骤 6：api handler

`internal/api/servergrpc/projects.go`：

```go
type ProjectsService struct{ serverv1.UnimplementedProjectsServiceServer; projects *appserver.Projects }
func (s *ProjectsService) CreateProject(ctx context.Context, req *serverv1.CreateProjectRequest)(*serverv1.Project,error){
  p, err:=s.projects.CreateProject(ctx, appserver.CreateProjectCommand{ID:req.GetId(), Name:req.GetName()})
  if err!=nil{return nil,err}
  return mapProject(p), nil // timestamppb.New 转换时间
}
```

职责：嵌 `Unimplemented`、参数→Command、用例→`map` 回 proto；更新类 `ctx=contexts.WithAuditResource(ctx,req.GetId())`；列表编码 token（下）。

### 7.1 列表分页（`shared.v1.ListRequest` + `pkg/crud`）

`proto/shared/v1/common.proto:7`：`page_size/page_token/filter/order_by/queries`；响应 `ListResponseMeta{page_size,next_page_token,prev_page_token,total_count}`（AIP-132/158/160）。

`pkg/crud/list.go:57` `ParseListParams(pageSize,pageToken,filter,orderBy)`：校验 `page_size∈[1,1000]`（默认 50）、`page_token` 解析得 `Offset`；`pagination.go:360` `BuildPaginationInfo(params,totalCount,hasMore)` 产出 `HasNext/NextOffset/HasPrevious/PreviousOffset`，`EncodePageToken(offset)`（`v1` base64 JSON，`DefaultTokenTTL=24h`，`FilterDigest`/`order_by` 校验）。

Handler：

```go
list, info, _ := s.projects.ListProjects(ctx, req.GetPageSize(), req.GetPageToken(), req.GetFilter(), req.GetOrderBy())
meta := &sharedv1.ListResponseMeta{PageSize: info.PageSize, TotalCount: int32(info.TotalCount)}
if info.HasNext { meta.NextPageToken = crud.EncodePageToken(info.NextOffset) }
if info.HasPrevious { meta.PrevPageToken = crud.EncodePageToken(info.PreviousOffset) }
```

- `filter/order_by` 显性化：`ValidatePageTokenForRequest`（`list.go:163`）要求翻页时二者与首请求一致（digest 不一致→`InvalidArgument`）；勿手拼 SQL `filter/order`。
- `pkg/crud/filter.go`/`order.go` 供静态表列表复用，动态文档优先 `pkg/query`（见 `06-databases.md` §6）。

示例：

```bash
curl -H 'X-API-Key: <key>' 'http://127.0.0.1:9080/v1/server/storage/buckets?page_size=20&filter=name%20eq%20"a"&order_by=name%20asc'
# 响应 {buckets:[...], meta:{page_size:20,next_page_token:"...",total_count:42}}
```

## 8 步骤 7：Wire

`internal/{api,app,infra}/provides.go` 各自 `ProviderSet`，汇总于 `cmd/server/provides.go`。

```go
// api/provides.go
wire.Bind(new(serverv1.ProjectsServiceServer), new(*servergrpc.ProjectsService))
// app/provides.go
wire.NewSet(server.NewProjects)
// infra/bun/provides.go
wire.Bind(new(projects.Repository), new(*bunrepo.ProjectRepo))
```

改构造器签名后 `task wire-all`（含 `wire-server` + `wire-worker`）重生成 `cmd/server/wire_gen.go`。

### 注册

`internal/infra/server/grpc.go:collectMethodsByAccess(descriptors...)` 聚合全部 `File_xxx_proto` 的 `method_auth/service_auth`，未覆盖服务增 file 后两处登记（此处 + `grpc_gateway.go:RegisterXxxHandlerFromEndpoint`），`assertRegisteredMethodsHaveAuthz` fail-closed。

## 9 错误与网关

用例层 `codes.Unauthenticated/PermissionDenied/NotFound/InvalidArgument/AlreadyExists`；`FailedPrecondition/OutOfRange` 用于 `version_*`/`超限`。

`internal/infra/server/errors.go:HTTPErrorHandler` 统转 JSON：

```json
{"error":{"type":"invalid_request_error","code":"InvalidArgument","message":"...","error_id":"<uuid>","error_code":"ERROR_CODE_INVALID_REQUEST"}}
```

映射：`InvalidArgument→400/ERROR_CODE_INVALID_REQUEST`、`Unauthenticated→401/INVALID_CREDENTIALS`、`PermissionDenied→403/PERMISSION_DENIED`、`NotFound→404/RESOURCE_NOT_FOUND`、`AlreadyExists/Aborted→409/RESOURCE_CONFLICT/CONCURRENT_MODIFICATION`、`ResourceExhausted→429/QUOTA_EXCEEDED`、`DeadlineExceeded→504/TIMEOUT`。

## 10 OpenAPI 与一致性断言

每服务文件声明 `openapiv2_swagger`：`security_definitions{apiKey(X-API-Key),Bearer(Authorization: Bearer),cookie(Cookie: TORCHWOOD_session_console)}` + `security{apiKey}` + `extensions{x-torchwood-access: api_key/public/authenticated/permission}`。

`method_auth` 与 `x-torchwood-access` 必须一致：未显式声明的 operation 继承 swagger 顶层（服务默认），`ACCESS_PUBLIC` 需 `security:[]`。

`internal/infra/server/grpc_swagger_test.go:73` `TestSwaggerAccessExtensionMatchesCollectMethodsByAccess` 逐 `genproto/**/*.swagger.json` 断言：`businessFileDescriptors()`（与 `grpc.go` 同步）→ `collectMethodsByAccess` 推导 access → 比对 `doc.XAccess`（顶层=服务默认）与每 `operation.x-torchwood-access`（继承或显式）完全一致；新增服务后两处 file 列表同步更新，否则测试失败（≥14 文件、≥140 operation）。

## 11 OutboxService 示例（新增服务的完整参照）

`proto/server/v1/outbox.proto:56`（`ACCESS_API_KEY` 默认，顶层 `x-torchwood-access=api_key`）：

```proto
service OutboxService {
  rpc ListDeadLetters(ListDeadLettersRequest) returns (ListDeadLettersResponse){
    option (google.api.http)={get:"/v1/server/outbox/dead-letters"};
  }
  rpc ReplayDeadLetter(ReplayDeadLetterRequest) returns (ReplayDeadLetterResponse){
    option (google.api.http)={post:"/v1/server/outbox/dead-letters/{event_id}:replay" body:"*"};
  }
}
```

步骤复盘：`proto` 定义→`task generate-proto`→`internal/domain/shared/ports.go:OutboxRepository` 扩展→`internal/app/events/outbox_admin.go` 用例（`5s` per-statement 超时）→`internal/infra/events/outbox.go` 适配（`document_events_outbox_dead`）→`internal/api/servergrpc/outbox.go` handler（`ListRequest→crud.ParseListParams`）→`grpc.go`/`grpc_gateway.go` 注册→`task wire-all`。

CLI 调用：`torchwood outbox list-dead --project <id>` / `torchwood rpc /torchwood.server.v1.OutboxService/ListDeadLetters --data '{"project_id":"shop","pageSize":20}'`。

## 12 自检清单

1. `task generate-proto && go build ./...` 通过，`genproto/` 无手改；2. `task wire-all` 已重生成；3. `go vet` + `gofmt -l` 空；4. 错误码/分页符合 §7/§9；5. `TestSwaggerAccessExtensionMatches...` 通过；6. 集成测试参照 `internal/api/servergrpc/projects_test.go`（`stub repo + contexts.WithPrincipal`）与 `internal/testutil` 真库。

## 13 参考

- `AGENTS.md` §编辑遵循模式（端口/适配器、`reserved`/`optional`/`Timestamp`、`pkg/crud/pkg/query`）、`README.md` §Architecture。
- `docs/developer/06-databases.md`（三层与 `pkg/query`）、`07-storage.md`（File Token 与 multipart）、`08-functions.md`（信号量与 Trim）。
- `sdk/README.md` 与 `sdk/go/server`（`InvokeJSON` 动态分发，CLI `import_guard_test.go`）。
