# Torchwood 后端 API 开发指南

> 本文面向需要新增 gRPC API 方法的开发者，以 `ProjectsService`（`proto/server/v1/projects.proto` →
> `internal/api/servergrpc/projects.go` → `internal/app/server/projects.go` → `internal/infra/bun/bunrepo/project_repo.go`）
> 为真实范例，手把手走完「proto → 生成 → domain → app → infra → api → Wire」全流程。
> 目标读者：新增资源 / 新增方法的后端开发者。
> 关联：`AGENTS.md`（开发约定，必读）、`docs/roadmap.md` §0（Agent-Native API 定位）。
> 修订记录：2026-08-09 初版（以 projects 全链路为范例；错误映射与分页约定按代码核实）；2026-08-12 更新错误映射表与 CLI 章节（按代码核实）。

---

## 0. 调用链总览

典型调用链（与 AGENTS.md 一致）：

```
gRPC handler（internal/api/servergrpc）
  → app use-case（internal/app/server）
    → domain repo port（internal/domain/projects，interface）
      → infra adapter（internal/infra/bun/bunrepo 或 internal/infra/documentdb）
```

每一层只依赖下层接口：

| 层 | 目录 | 职责 |
|----|------|------|
| 传输层 | `internal/api/` | proto 编解码、参数提取、分页 token 编解码、审计资源标注 |
| 用例层 | `internal/app/` | 鉴权判定、输入校验、事务编排、错误映射为 gRPC status |
| 领域层 | `internal/domain/` | 模型（struct）、仓库端口（interface） |
| 适配层 | `internal/infra/` | bun 元数据表查询 / documentdb 动态文档查询 |

---

## 1. 步骤 1：在 proto 中定义 RPC 与 message

API 的单一事实来源是 `proto/` 下的 `.proto` 文件，分四组：

- `proto/server/v1/`：管理面（Agent / API Key / Console 调用），如 `projects.proto`、`users.proto`；
- `proto/client/v1/`：终端用户面，如 `account.proto`、`databases.proto`；
- `proto/console/v1/`：Console 专用管理面，如 `admins.proto`、`auth.proto`；
- `proto/shared/v1/`：共享定义，如 `authz.proto`（鉴权注解）、`common.proto`（`ListRequest`/`ListResponseMeta`）、`error.proto`（错误枚举）。

### 1.1 服务与方法的骨架

以 `proto/server/v1/projects.proto` 为模板：

```proto
syntax = "proto3";

package torchwood.server.v1;

option go_package = "github.com/torchwooddev/torchwood/genproto/server/v1;serverv1";

import "google/api/annotations.proto";
import "google/protobuf/timestamp.proto";
import "shared/v1/authz.proto";
import "shared/v1/common.proto";

service ProjectsService {
  // 服务级默认访问级别：未在方法上单独标注时，所有方法默认 ACCESS_API_KEY。
  option (torchwood.shared.v1.service_auth) = { default_access: ACCESS_API_KEY };

  rpc CreateProject(CreateProjectRequest) returns (Project) {
    option (google.api.http) = { post: "/v1/server/projects", body: "*" };
  }

  rpc ListProjects(shared.v1.ListRequest) returns (ListProjectsResponse) {
    option (google.api.http) = { get: "/v1/server/projects" };
  }

  rpc GetProject(GetProjectRequest) returns (Project) {
    option (google.api.http) = { get: "/v1/server/projects/{id}" };
  }

  rpc UpdateProject(UpdateProjectRequest) returns (Project) {
    option (google.api.http) = { patch: "/v1/server/projects/{id}", body: "*" };
  }
}
```

要点：

- 每个 RPC 必须声明 `google.api.http` 注解，grpc-gateway 据此生成 HTTP 路由（`POST/GET/PATCH/DELETE`）。
- 路径字段用 `{id}` 语法绑定到请求 message 的同名字段（如 `GetProjectRequest.id`）。
- **列表请求复用 `shared.v1.ListRequest`，响应 meta 复用 `shared.v1.ListResponseMeta`**（见 §9），不要为每个资源重造分页字段。

### 1.2 authz 注解（强制）

`proto/shared/v1/authz.proto` 定义了两级注解：

```proto
enum AccessLevel {
  ACCESS_LEVEL_UNSPECIFIED = 0;
  ACCESS_PUBLIC = 1;          // 匿名可访问
  ACCESS_AUTHENTICATED = 2;   // 任意有效凭证
  ACCESS_PERMISSION = 3;      // 必须命中 permissions 之一
  ACCESS_API_KEY = 4;         // API key 或 admin console session
}

message MethodAuth { AccessLevel access = 1; repeated string permissions = 2; }
message ServiceAuth { AccessLevel default_access = 1; }

extend google.protobuf.MethodOptions { MethodAuth method_auth = 52001; }
extend google.protobuf.ServiceOptions { ServiceAuth service_auth = 52002; }
```

- 服务级 `service_auth.default_access` 可省略方法级标注（`ProjectsService` 即此模式）；
- 需要收紧的单个方法可显式覆盖。参照 `proto/console/v1/admins.proto`：

```proto
rpc ListAdmins(ListAdminsRequest) returns (ListAdminsResponse) {
  option (google.api.http) = { get: "/v1/console/admins" };
  option (torchwood.shared.v1.method_auth) = {
    access: ACCESS_PERMISSION
    permissions: ["owner", "admin"]
  };
}
```

> **强约束**：所有 gRPC 方法必须能解析出 authz 注解，否则 server 启动即失败，见 §10。

### 1.3 message 定义约定

- **proto3 optional 表达字段 presence**：更新类请求中「未传 ≠ 空串」的字段用 `optional string`，
  app 层据此判断是否要更新，如 `proto/server/v1/projects.proto` 的 `UpdateProjectRequest`：

```proto
message UpdateProjectRequest {
  string id = 1;
  optional string name = 2;         // 空值不修改（proto3 optional 表达 presence）
  optional string description = 3;
}
```

- **删除字段一律 `reserved`**：禁止直接删除或复用已发布的字段号/字段名；
  删除时声明 `reserved 5;`（字段号）与 `reserved "old_name";`（字段名），
  buf breaking（`buf.yaml` 已启用 `breaking: use: FILE`）据此拦截破坏性变更。
- 时间字段用 `google.protobuf.Timestamp`（HTTP JSON 映射为 RFC3339 字符串）；
- 生成的 Go 代码在 `genproto/`，**禁止手工编辑**。

> **⚠️ Breaking change（REST 自定义动词迁移，R10-P1-3/B3）**：Server API 的字面量
> 路由段 `documents/count`、`documents/bulk`、`documents/bulk/delete`、
> `functions/runtimes`、`functions/specifications` 已废弃，改为自定义动词
> `:count`/`:bulkUpdate`/`:bulkDelete`/`:runtimes`/`:specifications`（旧路径返回 404）。
> `count`/`bulk`/`runtimes`/`specifications` 不再占用 id 命名空间，可作
> document_id/function_id 使用；升级前请先重命名或删除历史保留字 id 资源。
> **Client API 同步迁移**：`/v1/databases/{database_id}/collections/{collection_id}/
> documents/count` 同样改为 `documents:count`（旧路径 404），Client API 的
> `count` 不再占用 document_id 命名空间。

---

## 1.4 OpenAPI 认证建模约定

所有 service proto 通过 `grpc.gateway.protoc_gen_openapiv2.options.openapiv2_swagger`
文件选项声明 `securityDefinitions` 与全局 `security`，使生成的 swagger.json 可被
外部 Agent 直接用于鉴权调用（roadmap §0 验收标准）。

三个统一 security scheme（所有文件保持一致）：

| scheme | 传输方式 | 适用面 |
|--------|----------|--------|
| `apiKey` | 请求头 `X-API-Key` | Server API / Agent 调用（access=api_key） |
| `Bearer` | `Authorization: Bearer <jwt>` | Client API 登录态（access=authenticated/permission） |
| `cookie` | `Cookie: TORCHWOOD_session_console=<sid>` | Console admin 会话 |

`method_auth` 的 access level 以 `x-torchwood-access` 扩展透传到 swagger 顶层
与 operation 级（值域 = AccessLevel 小写：`public`/`authenticated`/`permission`/`api_key`）：

- operation 未显式声明时继承 swagger 顶层（服务默认）值；
- `ACCESS_PUBLIC` 方法必须声明 `security: []`（匿名可达，覆盖全局 security）；
- 顶层扩展必须等于服务默认 access（`service_auth.default_access`），operation 级
  有效值必须等于 `method_auth` 的 access——由 `internal/infra/server/grpc_swagger_test.go`
  的 `TestSwaggerAccessExtensionMatchesCollectMethodsByAccess` 启动期断言。

新增服务/方法的规范：文件级设置 `openapiv2_swagger`（`security_definitions` +
`security` + `extensions.x-torchwood-access`）；与方法级 `method_auth` 不一致的
方法用 `openapiv2_operation` 覆盖（`security` + `extensions.x-torchwood-access`）。

---

## 2. 步骤 2：生成代码

```bash
task generate-proto    # 即 cd 仓库根后执行 buf lint + buf generate
```

`buf.gen.yaml`（v2 格式）声明四个远程插件，全部输出到 `genproto/`：

```yaml
version: v2
plugins:
  - remote: buf.build/protocolbuffers/go:v1.36.10   # *.pb.go
    out: genproto
    opt: [paths=source_relative]
  - remote: buf.build/grpc-ecosystem/gateway:v2.27.4 # *_gw.pb.go（HTTP 转换）
    out: genproto
    opt: [paths=source_relative]
  - remote: buf.build/grpc/go:v1.6.0                 # *_grpc.pb.go（服务接口）
    out: genproto
    opt: [paths=source_relative]
  - remote: buf.build/grpc-ecosystem/openapiv2:v2.27.3 # *.swagger.json
    out: genproto
    opt: [json_names_for_fields=true]
```

生成后：

- `genproto/server/v1/projects_grpc.pb.go` 提供 `ProjectsServiceServer` 接口与
  `RegisterProjectsServiceServer`；
- `genproto/server/v1/projects.pb.gw.go` 提供 `RegisterProjectsServiceHandlerFromEndpoint`
  供 gateway 使用；
- 每个 proto 文件还会生成 `File_server_v1_projects_proto` 描述符，供 §10 的鉴权收集使用。

---

## 3. 步骤 3：domain 层定义端口与模型

领域层只定义「端口」（interface）与「模型」（struct），不依赖任何存储实现。

### 3.1 模型（`internal/domain/projects/project.go`）

```go
type Project struct {
	ID          string
	Name        string
	Description string
	Status      string
	Settings    map[string]any
	InternalID  int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
```

注意字段全部是领域类型（`string`/`int64`/`time.Time`），不含 protobuf 类型——proto 与领域模型的
互转（`mapProject`）只在 api 层做。

### 3.2 仓库端口（`internal/domain/projects/repository.go`）

```go
type Repository interface {
	CreateProject(ctx context.Context, p *Project) error
	GetProject(ctx context.Context, id string) (*Project, error)
	GetProjectByName(ctx context.Context, name string) (*Project, error)
	ListProjects(ctx context.Context) ([]Project, error)
	UpdateProject(ctx context.Context, p *Project) error
	DeleteProject(ctx context.Context, id string) error
}
```

约定：

- 方法返回 `(*Project, error)` 时，「不存在」返回 `(nil, nil)`，由上层决定映射为 NotFound 还是
  AlreadyExists（`project_repo.go` 中 `sql.ErrNoRows` → nil）；
- 一个资源一个 interface 文件；跨资源协作按需新增端口（如 `ProjectResolver.InternalID`）。

---

## 4. 步骤 4：app 层实现用例

用例层是业务规则的家（`internal/app/server/projects.go`）：Principal 鉴权、输入校验、事务、错误映射。

### 4.1 用例结构体与构造器

```go
type Projects struct {
	projectRepo projects.Repository
	docDB       databases.DocumentDB   // 动态文档层端口（可选依赖）
	db          *clients.Database      // 事务执行器
}

func NewProjects(projectRepo projects.Repository, docDB databases.DocumentDB, db *clients.Database) *Projects {
	return &Projects{projectRepo: projectRepo, docDB: docDB, db: db}
}
```

命令模式：入参定义为 `XxxCommand` struct，不直接吃 proto 类型，保持用例层与传输层解耦。

### 4.2 鉴权与校验（CreateProject 前半段）

```go
principal, ok := contexts.Principal(ctx)
if !ok {
	return nil, status.Error(codes.Unauthenticated, "unauthenticated")
}
// 项目是平台级资源，创建仅限平台 admin（console 会话的 owner/admin 角色）。
if principal.ActorKind != shared.ActorKindAdmin || !principal.IsPlatformAdmin {
	return nil, status.Error(codes.PermissionDenied, "platform admin required to create projects")
}
if cmd.Name == "" {
	return nil, status.Error(codes.InvalidArgument, "name is required")
}
```

- Principal 由认证拦截器注入（`pkg/grpc/interceptor`），用例层通过 `contexts.Principal(ctx)` 读取
  （`internal/pkg/contexts/principal.go`），不要自己解析凭证；
- 校验规则写为 package 级常量/正则，并加注释引用出处（如 `projectIDRe` 引用安全评审 M7 / P2-9）。

### 4.3 事务（CreateProject 后半段）

```go
err := s.db.RunInTx(ctx, func(txCtx context.Context) error {
	if err := s.projectRepo.CreateProject(txCtx, p); err != nil {
		return fmt.Errorf("insert project: %w", err)
	}
	if err := s.docDB.EnsureSystemCollections(txCtx, p.ID, p.InternalID); err != nil {
		return fmt.Errorf("ensure system collections: %w", err)
	}
	return nil
})
```

事务内多个仓库调用必须共用 `txCtx`；事务外返回包装错误（`fmt.Errorf("...: %w", err)`）保留错误链。

### 4.4 越权与错误语义（GetProject / UpdateProject）

- 越权访问**返回 NotFound 而不是 PermissionDenied**，避免资源存在性探测（安全评审 M7）：

```go
if !principal.IsPlatformAdmin && (principal.ProjectID == "" || principal.ProjectID != id) {
	return nil, status.Error(codes.NotFound, "project not found")
}
```

- 更新类请求把「nothing to update」前置检查放在取数之前，避免语义歧义；
- 撞名查重返回 `InvalidArgument`（而不是依赖 DB unique violation 变 500）。

---

## 5. 步骤 5：infra 层实现 adapter

### 5.1 元数据表：bun 仓库（`internal/infra/bun/bunrepo/project_repo.go`）

```go
func (r *projectRepo) GetProject(ctx context.Context, id string) (*projects.Project, error) {
	m := new(model.Project)
	err := r.db.NewSelect().Model(m).Where("id = ?", id).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return mapProjectToDomain(m), nil
}
```

- 表模型在 `internal/infra/bun/model/`（如 `model.Project`），仓库内用 `mapProjectToModel` /
  `mapProjectToDomain` 双向转换；
- `db` 来自 `*clients.Database`（内嵌 bun.DB），构造器签名 `func NewXxxRepository(db *clients.Database) xxx.Repository`，
  返回类型写**端口 interface**，便于 Wire 注入与测试替换。

### 5.2 动态文档：documentdb adapter

用户资源（users/sessions/files/buckets/teams）与用户动态集合走 PostgreSQL 动态文档 adapter：
`schema-per-database + _tenant + _perms`。端口定义在 `internal/domain/databases/document.go`
（`DocumentDB` interface，含 `CreateDatabase`/`CreateCollection`/`ListDocuments`/`CountDocuments` 等），
实现为 `internal/infra/documentdb/postgres.go` 的 `NewPostgresDocumentDB(db)`。

- 动态文档查询**优先使用 `pkg/query`**（Appwrite 风格 DSL）：`equal`、`notEqual`、`greaterThan`、
  `contains`、`orderDesc`、`orderAsc`、`limit`、`offset`、`select`、`cursorAfter`/`cursorBefore` 等；
- 非 System 路径有字段白名单 + 敏感字段黑名单校验（未声明列 → `InvalidArgument`）；
- adapter 返回领域错误（`databases.ErrDuplicateKey`、`ErrPermissionDenied` 等），由
  `internal/app/shared.MapDocumentDBError`（`internal/app/shared/docdb_errors.go`）
  统一映射为 gRPC status（`AlreadyExists` / `PermissionDenied` / ...）。

---

## 6. 步骤 6：api 层 handler 与注册

### 6.1 handler（`internal/api/servergrpc/projects.go`）

```go
type ProjectsService struct {
	serverv1.UnimplementedProjectsServiceServer
	projects *appserver.Projects
}

func NewProjectsService(projects *appserver.Projects) *ProjectsService {
	return &ProjectsService{projects: projects}
}

func (s *ProjectsService) CreateProject(ctx context.Context, req *serverv1.CreateProjectRequest) (*serverv1.Project, error) {
	p, err := s.projects.CreateProject(ctx, appserver.CreateProjectCommand{
		Name:        req.GetName(),
		Description: req.GetDescription(),
	})
	if err != nil {
		return nil, err
	}
	return mapProject(p), nil
}
```

handler 的职责边界：

- 嵌入 `UnimplementedXxxServiceServer`（新增方法时旧 client 不 panic）；
- 从 proto 请求提取参数 → 组装 Command → 调用用例 → 错误原样向上抛（错误映射已在用例层完成）；
- 用 `mapXxx(domain)` 私有函数转回 proto 响应（`timestamppb.New(...)` 转时间）；
- 更新类方法给审计拦截器标注资源：`ctx = contexts.WithAuditResource(ctx, req.GetId())`；
- 列表方法负责 page token 编解码（§6.2）。

### 6.2 列表 handler：分页 token 编解码

```go
func (s *ProjectsService) ListProjects(ctx context.Context, req *sharedv1.ListRequest) (*serverv1.ListProjectsResponse, error) {
	list, info, err := s.projects.ListProjects(ctx, req.GetPageSize(), req.GetPageToken(), req.GetFilter(), req.GetOrderBy())
	if err != nil {
		return nil, err
	}
	var nextToken, prevToken string
	if info.HasNext {
		nextToken = crud.EncodePageToken(info.NextOffset)
	}
	if info.HasPrevious {
		prevToken = crud.EncodePageToken(info.PreviousOffset)
	}
	resp := &serverv1.ListProjectsResponse{
		Projects: make([]*serverv1.Project, len(list)),
		Meta: &sharedv1.ListResponseMeta{
			PageSize:      info.PageSize,
			NextPageToken: nextToken,
			PrevPageToken: prevToken,
			TotalCount:    int32(info.TotalCount),
		},
	}
	for i, p := range list {
		resp.Projects[i] = mapProject(&p)
	}
	return resp, nil
}
```

用例层负责真实分页（`internal/app/server/projects.go` 的 `ListProjects`）：

```go
params, err := crud.ParseListParams(pageSize, pageToken, filter, orderBy)
// ... 取全量 → 按 params.Offset/params.PageSize 切片
info := crud.BuildPaginationInfo(params, len(all), hasMore)
```

### 6.3 注册 gRPC 服务（`internal/infra/server/grpc.go`）

新增服务时改三处：

1. `collectMethodsByAccess(...)` 的 file descriptor 列表追加
   `serverv1.File_server_v1_projects_proto`（新增 RPC 自动纳入鉴权收集）；
2. `serverv1.RegisterProjectsServiceServer(grpcSrv, projects)`；
3. 若该服务有新的 http 路由，同时在 `internal/infra/server/grpc_gateway.go` 的 `register` 列表追加
   `serverv1.RegisterProjectsServiceHandlerFromEndpoint`。

> 启动期有两道 fail-closed 检查（§10），漏了注解或漏了注册都会直接报错退出，这是设计预期。

---

## 7. 步骤 7：Wire 装配

Provider 声明分散在四层，`task wire-all` 生成 `cmd/server/wire_gen.go`：

| 文件 | 注册内容 |
|------|----------|
| `internal/api/provides.go` | `servergrpc.NewProjectsService`（handler 构造器） |
| `internal/app/provides.go` | `server.NewProjects`（use-case 构造器） |
| `internal/infra/bun/provides.go` | `bunrepo.NewProjectRepository`（adapter 构造器） |
| `cmd/server/provides.go` | 汇总 `api.ProviderSet` + `app.ProviderSet` + `infra.ProviderSet` + `domain.ProviderSet` |

构造器签名变化（新增/删除参数）后执行：

```bash
task wire-all     # wire-server + wire-worker
```

Wire 按构造器参数类型自动匹配依赖（`*clients.Database`、`databases.DocumentDB` 等均为单例 provider）。

---

## 8. 错误处理约定

### 8.1 用例层：`status.Error(codes.X, message)`

app 层所有预期内错误必须返回带 gRPC code 的 `status.Error`，禁止裸 `errors.New` 泄漏到 gRPC 边界
（裸错误在 gateway 会被包装为 `codes.Internal`）。

代码中已确认的状态码用法：

| gRPC code | 场景（以代码为准） |
|-----------|--------------------|
| `codes.Unauthenticated` | `contexts.Principal(ctx)` 取不到；凭证缺失/无效（拦截器） |
| `codes.PermissionDenied` | 角色/scope 不足；平台级操作被 API key 调用；`pkg/grpc/interceptor` 的 `permissionMethods` 未命中 |
| `codes.NotFound` | 资源不存在；**越权访问伪装 NotFound 防枚举** |
| `codes.InvalidArgument` | 必填缺失、长度超限（description ≤ 512）、ID 白名单不匹配（`^[a-z0-9-]{1,64}$`）、nothing to update、撞名 |
| `codes.AlreadyExists` | 重复创建（DB unique violation 经 `MapDocumentDBError` 映射） |
| `codes.Internal` | 未映射的存储层错误（外层 `fmt.Errorf` 包装后透出） |

### 8.2 gateway：统一 JSON 错误体（`internal/infra/server/errors.go`）

grpc-gateway 用自定义 `HTTPErrorHandler` 把 gRPC status 转为统一结构：

```json
{
  "error": {
    "type": "invalid_request_error",
    "code": "InvalidArgument",
    "message": "name is required",
    "error_id": "<uuid>",
    "error_code": "ERROR_CODE_INVALID_REQUEST"
  }
}
```

- `error_id` 每次请求生成新 UUID，便于日志关联排障；
- `error_code` 取自 `proto/shared/v1/error.proto` 的 `ErrorCode` 枚举，现有映射（`HTTPErrorHandler`
  内 `switch st.Code()` + `grpcCodeToHTTP`）：

| gRPC code | error_code | HTTP 状态码 |
|-----------|-----------|-------------|
| InvalidArgument | `ERROR_CODE_INVALID_REQUEST` | 400 |
| FailedPrecondition | `ERROR_CODE_PRECONDITION_FAILED` | 400 |
| OutOfRange | `ERROR_CODE_INTERNAL_ERROR`（未显式映射，走默认） | 400 |
| Unauthenticated | `ERROR_CODE_INVALID_CREDENTIALS` | 401 |
| PermissionDenied | `ERROR_CODE_PERMISSION_DENIED` | 403 |
| NotFound | `ERROR_CODE_RESOURCE_NOT_FOUND` | 404 |
| AlreadyExists | `ERROR_CODE_RESOURCE_CONFLICT` | 409 |
| Aborted | `ERROR_CODE_CONCURRENT_MODIFICATION` | 409 |
| ResourceExhausted | `ERROR_CODE_QUOTA_EXCEEDED` | 429 |
| DeadlineExceeded | `ERROR_CODE_TIMEOUT` | 504 |
| 其他（Unknown/Unimplemented/Unavailable/Canceled 等） | `ERROR_CODE_INTERNAL_ERROR` | 500（Canceled=499、Unavailable=503、Unimplemented=501 等对应码） |

- `type` 字段：`invalid_request_error` / `authentication_error` / `permission_error` /
  `not_found_error` / `conflict_error` / `rate_limit_error` / `server_error`；
- `ErrorCode` 枚举完整清单（`proto/shared/v1/error.proto`）：
  `ERROR_CODE_INVALID_REQUEST`、`ERROR_CODE_RESOURCE_NOT_FOUND`、`ERROR_CODE_INVALID_CREDENTIALS`、
  `ERROR_CODE_PERMISSION_DENIED`、`ERROR_CODE_RESOURCE_CONFLICT`、`ERROR_CODE_QUOTA_EXCEEDED`、
  `ERROR_CODE_PRECONDITION_FAILED`、`ERROR_CODE_CONCURRENT_MODIFICATION`、
  `ERROR_CODE_VALUE_OUT_OF_RANGE`、`ERROR_CODE_OPERATION_NOT_ALLOWED`、
  `ERROR_CODE_INTERNAL_ERROR`、`ERROR_CODE_SERVICE_UNAVAILABLE`、`ERROR_CODE_TIMEOUT`。

---

## 9. 列表查询约定（AIP-132 / 158 / 160）

列表复用 `shared.v1.ListRequest` 与 `shared.v1.ListResponseMeta`（`proto/shared/v1/common.proto`），
由 `pkg/crud` 统一处理：

**请求参数**（`shared.v1.ListRequest`）：

| 字段 | 说明 | 标准 |
|------|------|------|
| `page_size` | 每页条数，默认 50，上限 1000 | AIP-158 |
| `page_token` | 上一页返回的 `next_page_token`，base64 编码的 offset，TTL 24h | AIP-158 |
| `filter` | 过滤表达式（带 digest 校验，翻页时 filter/order_by 必须一致） | AIP-160 |
| `order_by` | 排序（AIP-132 的 ordering） | AIP-132 |
| `queries` | 动态文档层 Appwrite DSL 查询串（documentdb 场景） | — |

**响应 meta**（`shared.v1.ListResponseMeta`）：`page_size`、`next_page_token`、`prev_page_token`、`total_count`。

**`pkg/crud` 关键入口**（`pkg/crud/list.go`、`pagination.go`）：

- `crud.ParseListParams(pageSize, pageToken, filter, orderBy)`：校验 page_size 边界与 token 格式，返回
  `ListParams{PageSize, PageToken, Filter, OrderBy, Offset}`；
- `crud.EncodePageToken(offset)` / `crud.DecodePageToken(token)`：offset 型 token 编解码（`v1:legacy`，
  过期自动拒绝）；
- `crud.BuildPaginationInfo(params, totalCount, hasMore)`：产出 `HasNext/HasPrevious/NextOffset/PreviousOffset`，
  供 handler 生成 next/prev token；
- 常量：`DefaultPageSize = 50`、`MaxPageSize = 1000`。

> **强约束**：列表查询复用 `pkg/crud`（或等价的 AIP 抽象），不要手拼 SQL filter/order；动态文档
> 优先使用 `pkg/query`。翻页一致性由 token 内嵌的 filter digest / order_by 校验保证
> （`ValidatePageTokenForRequest`），filter/order_by 变化会报 `InvalidArgument`。

---

## 10. 强制约束清单（对应 AGENTS.md）

1. **gRPC 方法必须带 authz 注解**：
   - `collectMethodsByAccess`（`internal/infra/server/grpc.go`）对每个方法解析 `method_auth` 或
     服务级 `service_auth`，解析不到（`UNSPECIFIED`）即报
     `missing auth policy for method <service>/<method>`；
   - `ACCESS_PERMISSION` 方法必须显式声明非空 `permissions`，否则报错；
   - 启动期 `assertRegisteredMethodsHaveAuthz` 再校验一次：已注册但不在任何 access map 的方法
     直接拒绝启动（fail-closed，防漏配方法被任意凭证放行）。
   - 新增服务文件后记得把它加入 `collectMethodsByAccess` 的 file descriptor 列表。
2. **列表复用 `pkg/crud`**，动态文档优先 `pkg/query`；不要手拼 SQL filter/order。
3. **JWT claims 保持 `pkg/jwtparser` 兼容**：新增凭证/上下文字段必须沿用 `Claims` 的短键映射
   （`tid`/`uid`/`usn`/`akd`/`pid`/`sid`/`ttp`/`rls`/`scp`/`exp`/`iat`），不要自造键名。
4. **端口在 domain、适配器在 infra**：用例层只依赖 `internal/domain/xxx` 的 interface；
   `wire.Bind` 在 `internal/infra/provides.go` 完成接口绑定（如 `wire.Bind(new(domainauth.SessionService), new(*auth.SessionService))`）。
5. **错误必须带 code**：预期内错误用 `status.Error`；存储层原始错误用 `%w` 包装保留错误链。
6. **安全默认值**：越权返回 NotFound 防枚举；平台级资源仅平台 admin 可操作；API key 禁止管理 API Keys
   （`IsAPIKeysServiceMethod` 拦截，防自铸 key 提权）。

---

## 11. 开发后自检清单

1. `task generate-proto` 后编译通过，`genproto/` 无手工改动；
2. `go build ./...` 通过；
3. `task wire-all` 重新生成 wire_gen.go（构造器签名有变化时）；
4. 新增方法跑一遍 `go vet ./...` 与 `gofmt -l .`（必须空输出）；
5. 按 §8/§9 核对错误码与分页参数命名；
6. 集成测试参照 `internal/api/servergrpc/projects_test.go`（stub repo + `contexts.WithPrincipal`）与
   `internal/testutil`（真实 DB），详见 `docs/developer/11-testing.md`。

---

## 12. 用 Torchwood CLI 调用 Server API

`cmd/client`（二进制 `bin/torchwood[.exe]`，cobra）通过 gRPC 调用 Server API，认证走 `x-api-key` metadata（**不传** `X-Torchwood-Project`，该 header 仅对 admin console session 有效）。`health` 为公开命令、`uuid` 为本地工具（无需 key），其余命令必须先提供 API key（`--api-key` 或 `TORCHWOOD_CLI_API_KEY`）。

常用示例：

```bash
torchwood health get
torchwood uuid
torchwood users list --api-key <secret> --page-size 20
torchwood users create --email a@b.c --password 'pw' --data '{"labels":{"team":"core"}}'
torchwood projects get default
torchwood databases documents create app notes --data '{"title":"hi"}'
torchwood storage usage
torchwood functions executions create hello --input '{}' --async
torchwood oauth-providers list
torchwood rpc /torchwood.server.v1.UsersService/ListUsers --data '{"pageSize": 10}'
```

命令树（`torchwood --help`）：

```text
uuid         生成本地 UUID v4（无需 API key；纯文本输出，便于传给 --id）
databases    create/list/get/delete；collections create/list/get/update/delete；
             attributes create/delete；indexes create/delete；
             documents create/list/get/update/upsert/delete/count/bulk-update/bulk-delete
teams        create/list/get/delete；prefs get/update；memberships create/list/get/
             update/update-status/delete
storage      buckets create/list/get/update/delete；files list/get/update/delete；
             usage（不做文件上传/下载与分片会话，也不提供 files create/token）
functions    runtimes/specifications；create/list/get/update/delete；
             deployments create/list/get/delete（create 走 gRPC 纯消息，≤8MiB；
             更大代码包走 multipart 上传接口，≤50MiB）；
             variables set/get；executions create/list/get
oauth-providers list/upsert/delete（proto 无 get 方法）
```

要点：

- **动态分发机制**：`rpc` 逃生舱与全部具名命令最终都走 `sdk/go/server` 的
  `InvokeJSON`——按 full method name 从 `protoregistry.GlobalFiles` 查找，限定
  `torchwood.server.v1.*` 且排除 `APIKeysService`。**新增 Server API RPC 无需
  在 CLI 登记**，proto 方法自动获得支持；`cmd/client/import_guard_test.go` 兜底
  禁止 CLI 源码直接 import genproto/grpc/protobuf。
- **具名命令**覆盖 `proto/server/v1` 全部资源（health/projects/users/databases/teams/
  storage/functions/oauth-providers）以及本地 `uuid` 工具，方法级覆盖见上方命令树。
- **请求参数**：标量用具名 flag，复杂结构（labels/prefs 等 `Struct`、document data）
  用 `--data` 传 protojson（camelCase 字段名），与 flag 冲突时以 `--data` 为准。
- **安全边界**：CLI 不提供 api-keys 命令（API Key 凭证被服务端拦截器禁止调用），
  不提供 `projects create/update`（限平台 admin）。
- 错误统一输出 `code + message`（`PermissionDenied` 附带 scope 提示）到 stderr，非 0 退出码。
- 设计文档：`docs/implementation-bootstrap-and-cli.md` §4；快速上手：`docs/developer/02-quickstart.md` §7。
