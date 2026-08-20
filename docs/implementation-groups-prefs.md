# Torchwood Groups 用户组偏好实现方案

> 状态：**已实现**（2026-08-10 验收通过：2 RPC + reconcile 存量补列 + 自愈 + 权限矩阵
> 全部落地；定向测试 count=3 稳定；3 项实现偏差经裁决全部接受——P1 至此全部完成）
> 目标读者：维护者与后续扩展
> 关联：`docs/roadmap.md` §2.3（Groups & Memberships，最后残留项）、`AGENTS.md`（开发约定，必读）
> 参考：`docs/implementation-storage-chunked-upload.md`（上一轮同类方案：先审查、后实现、再汇报）
> 修订记录：2026-08-10 v2（独立评审修订：reconcile 伪代码 coll==nil 分支、use-case 补 resolveProject、
> 权限语义修正（已接受成员可写）、错误映射 MapDocumentDBError、reconcile 并发幂等、
> CreateAttribute 直接调用、行号修正等）

---

## 1. 目标与验收标准

实现用户组级偏好读写 `GET/PUT /v1/server/groups/{id}/prefs`，收尾 roadmap §2.3。

**验收标准**：

1. `GET /v1/server/groups/{id}/prefs` 返回用户组 `prefs` JSON（`google.protobuf.Struct`）；
   从未设置时返回空对象 `{}`。
2. `PUT /v1/server/groups/{id}/prefs` 整体替换 prefs；请求体为合法 Struct；
   返回更新后的 prefs。
3. 权限（评审修正——精确语义）：
   - API key 需 `groups.write` scope（`UpdateGroupPrefs`）/ `groups.read`（`GetGroupPrefs`）；
   - use-case 层 `resolveProject` 后 docDB update 校验：`keys`/`admin`/`PlatformAdmin`
     放行；**已接受成员（持有 `group:{id}` 角色，user_roles.go:62 注入）因系统集合
     OR 语义同样可写**；无用户组角色的 users 主体被拒（`ErrPermissionDenied`）。
4. **存量项目兼容（自愈）**：`GetGroupPrefs`/`UpdateGroupPrefs` 首请求即触发
   `EnsureSystemCollections` reconcile——幂等补齐 groups 表 `prefs` 列 +
   `document_attributes` 元数据；新老项目均可读写。
5. 权限拒绝在 HTTP 层映射为 403（显式 `MapDocumentDBError`，非 500）。
6. Console 用户组详情页增加 prefs 编辑卡片（JSON 编辑器 + 保存）。
7. `go test ./...`、`task lint`、`task build` 全绿。

---

## 2. 现状盘点（调研结论）

| 项 | 现状 |
|---|---|
| proto | `groups.proto:12-54` 10 个 RPC 无 prefs；`Group` 消息（:70-77）无 prefs；未 import `google/protobuf/struct.proto`；`GetGroupRequest{id}`（:61-63）可复用 |
| app use-case | `internal/app/server/groups.go`（527 行）无 prefs；`getGroupDoc`（:53，**不调 resolveProject**）；`adjustGroupTotal`（:441）是整文档单字段更新先例；现有方法均先 `resolveProject`（:39/:47） |
| handler | `internal/api/servergrpc/groups.go`（263 行）projectID 检查（:26-32）+ `dbPrincipal` + `mapGroupDoc`（:189-215） |
| 集合 spec | `groups`（system_collection_specs.go:148-172）attrs 无 prefs；`users_prefs` json 先例（:27）；`GetCollection` attributes 来自 `document_attributes` 元数据表（postgres.go:1197-1210） |
| 存量迁移缺口 | `EnsureSystemCollections`（postgres.go:855-913）对已存在集合 `continue`（:895-897）不补列；Server `CreateAttribute` 的"拒绝系统集合"只在 **app 层**（databases.go:179-181），**infra 层 `CreateAttribute`（postgres.go:324-353）无此守卫可直接调用**（ADD COLUMN IF NOT EXISTS + 元数据 INSERT 一步到位） |
| account prefs 先例 | `internal/app/client/account.go:498-535`（GetPrefs/UpdatePrefs + `SimpleDocumentUpdate` + ok-assert 兜底 :531-534）；`internal/api/clientgrpc/account.go:127-152`（`structpb.NewStruct` :134 + `AsMap`） |
| 权限语义 | 系统集合 docDB **OR 语义**（permissions.go:102-105 `collOK || docOK`）；groups 集合 update 权限 `group:{id}`/`keys`/`admin`（spec :158-171）；已接受成员经 `group:{id}` 角色可写用户组文档 |
| Console | `GroupDetailPage`（routes/groups/pages.tsx:208，DetailGrid :382-390）；api/groups.ts 无 prefs 函数 |
| 领域模型 | 无 Group 结构体（docDB Document 直传），prefs 以 `map[string]any` 表达 |
| 测试 | `groups_memberships_integration_test.go`（:17-109）无 prefs；`account_sessions_test.go:17-74` 是 prefs 测试先例 |
| wire | 无 provider 变更 → 无需 wire-all |

---

## 3. 分层实现规格

### 3.1 proto（`proto/server/v1/groups.proto` 扩展）

```proto
import "google/protobuf/struct.proto";

rpc GetGroupPrefs(GetGroupRequest) returns (GetGroupPrefsResponse) {
  option (google.api.http) = { get: "/v1/server/groups/{id}/prefs" };
}
rpc UpdateGroupPrefs(UpdateGroupPrefsRequest) returns (GetGroupPrefsResponse) {
  option (google.api.http) = { put: "/v1/server/groups/{id}/prefs", body: "*" };
}

message GetGroupPrefsResponse { google.protobuf.Struct prefs = 1; }
message UpdateGroupPrefsRequest { string id = 1; google.protobuf.Struct prefs = 2; }
```

- **复用 `GetGroupRequest`（评审定案）**：与 DeleteGroup 先例一致（HTTP 映射无冲突）；
  **不定义 `GetGroupPrefsRequest`**（避免死代码）。
- 服务级 `ACCESS_API_KEY` 已存在（:13），新 RPC 自动继承，无需 method_auth。
- `Group` 消息**不加** prefs 字段（列表接口不返回 prefs，避免大对象透传）。
- `task generate-proto`。

### 3.2 集合 spec + 存量补列（关键）

**`internal/infra/documentdb/system_collection_specs.go:148-154`**：

```go
"groups": {
  attrs: []databases.Attribute{
    {ID: "groups_name", Key: "name", Type: "string", Size: 256},
    {ID: "groups_permissions", Key: "permissions", Type: "json"},
    {ID: "groups_total", Key: "total", Type: "integer", Default: 0},
    {ID: "groups_prefs", Key: "prefs", Type: "json"},   // 新增
  },
  ...
```

**`internal/infra/documentdb/postgres.go` `EnsureSystemCollections`（:889-901）改造
（评审修正——伪代码分支必须正确）**：

```go
for _, id := range databases.SystemCollectionIDs {
    spec := systemCollectionSpecs(projectID)[id]
    coll, err := p.GetCollection(ctx, projectID, dbID, id)
    if err != nil { return err }
    if coll == nil {
        // 集合不存在 → 原逻辑：创建（绝不能 continue！）
        if err := p.CreateCollection(ctx, projectID, dbID, id, spec.name, spec.attrs, spec.indexes, spec.permissions, true); err != nil {
            return fmt.Errorf("create system collection %s: %w", id, err)
        }
        continue
    }
    // 集合已存在 → 幂等补齐缺失属性（存量项目迁移）
    if err := p.reconcileSystemCollectionAttrs(ctx, projectID, dbID, id, spec); err != nil {
        return err
    }
}
```

**`reconcileSystemCollectionAttrs` 实现要点（评审修正）**：

1. **直接调用 `p.CreateAttribute(ctx, projectID, dbID, id, attr)`**（infra 层无系统
   集合守卫，postgres.go:324-353 一步完成 ADD COLUMN IF NOT EXISTS + 元数据 INSERT）；
   无需抽 helper。
2. 按 `coll.Attributes` 的 **Key** 比对缺失（**切勿用 ID 比对**——存量行 ID 可能
   不符 `{collection}_{key}` 约定）。
3. **并发幂等（评审补充）**：多个请求同时发现缺失 → 同时 DDL（IF NOT EXISTS 幂等）
   + 同时 INSERT 元数据 → 撞 `UNIQUE (project_id, database_id, collection_id, key)`
   唯一约束（migration 000003:56）→ **对 `isUniqueViolation`（SQLSTATE 23505）忽略**，
   其余错误返回。
4. 边界声明：只修"物理列/元数据任一缺失"方向，不做反向校验（元数据在、列被手工删
   的罕见场景不处理）。
5. 性能：`EnsureSystemCollections` 每次请求已对 10 个集合各做一次 GetCollection
   （现状即如此）；reconcile 增加的是内存比对 + 仅首次 DDL，无新增常驻查询。

### 3.3 app use-case（`internal/app/server/groups.go` 扩展）

```go
func (t *Groups) GetGroupPrefs(ctx context.Context, projectID, groupID string, principal databases.Principal) (map[string]any, error)
func (t *Groups) UpdateGroupPrefs(ctx context.Context, projectID, groupID string, prefs map[string]any, principal databases.Principal) (map[string]any, error)
```

**两个方法开头必须（评审修正——否则 reconcile 永不触发 + 新项目 SQL 42P01 500）**：

```go
if _, err := t.resolveProject(ctx, projectID); err != nil {
    return nil, err
}
```

（`resolveProject` groups.go:47 是 `EnsureSystemCollections` 的唯一调用入口——
首请求即触发补列，存量项目自愈。）

- `GetGroupPrefs`：`getGroupDoc(ctx, projectID, groupID, principal)`（nil → `NotFound`
  "group not found"）→ `doc.Data["prefs"]` 若为 `map[string]any` 返回，否则 `{}`。
- `UpdateGroupPrefs`：
  1. `prefs == nil` → `InvalidArgument` "prefs is required"；
  2. `getGroupDoc`（nil → NotFound）；
  3. `SimpleDocumentUpdate(databases.Document{ID: groupID, Data: {"prefs": prefs}}, nil)`
     → `UpdateDocument`；
  4. **错误显式映射（评审修正）**：`return nil, appshared.MapDocumentDBError(err)`
     （import `internal/app/shared`，先例 databases.go:335/410）——否则 raw
     `ErrPermissionDenied` 经 gRPC 变 `codes.Unknown` → HTTP 500 而非 403；
  5. 返回 `updated.Data["prefs"]`：**ok-assert 兜底**（照抄 account.go:531-534），
     失败返回 `map[string]any{}`。

### 3.4 gRPC handler（`internal/api/servergrpc/groups.go` 扩展）

```go
func (s *GroupsService) GetGroupPrefs(ctx context.Context, req *serverv1.GetGroupRequest) (*serverv1.GetGroupPrefsResponse, error)
func (s *GroupsService) UpdateGroupPrefs(ctx context.Context, req *serverv1.UpdateGroupPrefsRequest) (*serverv1.GetGroupPrefsResponse, error)
```

- projectID 检查（:26-32 模式）→ use-case → `structpb.NewStruct(prefs)`（失败 →
  `InvalidArgument` "prefs is not serializable"，参照 clientgrpc/account.go:134）。
- UpdateGroupPrefs：`req.GetPrefs() == nil` → `InvalidArgument`；`AsMap()` 透传。

### 3.5 Console

**`console/src/api/groups.ts`**：

```ts
export async function getGroupPrefs(id: string): Promise<Record<string, unknown>> {
  const res = await api.get<{ prefs: Record<string, unknown> }>(`/server/groups/${id}/prefs`);
  return res.data.prefs ?? {};
}
export async function updateGroupPrefs(
  id: string,
  prefs: Record<string, unknown>
): Promise<Record<string, unknown>> {
  const res = await api.put<{ prefs: Record<string, unknown> }>(
    `/server/groups/${id}/prefs`, { prefs }
  );
  return res.data.prefs ?? {};
}
```

**`console/src/routes/groups/pages.tsx` `GroupDetailPage`**：DetailGrid（:382-390）后新增
「用户组偏好」卡片：

- `useQuery(["groups", id, "prefs"], () => getGroupPrefs(id!))`；
- 编辑：JSON 文本域**复用 `FormField`**（components/resource/shared.tsx:47）；
  JSON 解析失败提示**照抄 routes/databases/pages.tsx:1356-1370 模式**
  （`JSON.parse` + toast.error + 对象校验）；保存后 invalidate `["groups", id, "prefs"]`。

### 3.6 scope 与鉴权

- 新 RPC 继承服务默认 `ACCESS_API_KEY`；`apiKeyScopeRules` 登记（参照 :54-63 Groups 段）：
  - `GetGroupPrefs` → `{"groups", "read"}`
  - `UpdateGroupPrefs` → `{"groups", "write"}`
- 否则 API key 调用（含 `*` scope）fail-closed 拒绝。

---

## 4. 实现顺序（建议）

| 步骤 | 内容 | 验证 |
|------|------|------|
| 1 | proto + `task generate-proto` + scope 登记 | 编译通过 |
| 2 | 集合 spec + EnsureSystemCollections reconcile（含并发幂等）+ 集成测试 | `go test ./internal/infra/documentdb/...` |
| 3 | use-case（resolveProject 开头 + MapDocumentDBError 映射）+ 集成测试 | `go test ./internal/app/server/...` |
| 4 | handler | `go build ./...` |
| 5 | Console（api + 卡片） | `task console-build` |
| 6 | 全量验证 + 文档（roadmap/developer/checklist） | 见 §5 |

每步完成跑 `gofmt -l .`（必须空）+ `go vet ./...`。

---

## 5. 测试与验证

- **reconcile 集成测试**（`internal/infra/documentdb/`）：
  - 手工建旧 spec 的 groups 集合（只含 name/permissions/total）→ 调
    `EnsureSystemCollections`（新 spec 含 prefs）→ 断言 `GetCollection` attributes
    含 prefs 且物理列存在（写一条含 prefs 的文档成功）；重复调用幂等；
  - **并发**：两 goroutine 同时 EnsureSystemCollections → 均无错误（唯一键冲突被吞）。
- **use-case 集成测试**（`internal/app/server/groups_prefs_integration_test.go`）：
  - 创建用户组 → GetGroupPrefs 返回 `{}` → UpdateGroupPrefs({"theme":"dark"}) →
    GetGroupPrefs 返回更新值 → 再次 UpdateGroupPrefs 整体替换（旧键消失）；
  - **存量自愈（评审补充）**：先只建旧 spec 集合、**不调 EnsureSystemCollections**，
    直接调 GetGroupPrefs/UpdateGroupPrefs → 断言触发 reconcile 并成功读写
    （覆盖验收标准 4 的"首请求即自愈"）；
  - 用户组不存在 → NotFound；prefs nil → InvalidArgument；
  - **权限（评审修正的精确断言）**：`Principal{Roles: []string{"keys"}}` 与
    `{Roles: []string{"admin"}}` 可写；**已接受成员 `{Roles: []string{"users",
    "user:<uid>", "group:<id>"}}` 可写（OR 语义）**；无用户组角色的
    `{Roles: []string{"users", "user:<uid>"}}` → `codes.PermissionDenied`。
- **scope 测试**：`apikey_scope_test.go` 追加 GetGroupPrefs read / UpdateGroupPrefs
  write 断言。
- **handler 冒烟**（可选）：servergrpc groups 测试（现有无该文件则跳过或新建）。
- **全量验证**：`go test ./...`（.env 提供 TORCHWOOD_TEST_DATABASE_SOURCE）、
  `task lint`、`task build`。
- CI 无需改动。

---

## 6. 范围外（明确不做）

- `Group` 消息与 `mapGroupDoc` 不加 prefs 字段（列表不返回 prefs）。
- Client Groups API 的 prefs（纯 Server API 项；client/databases.go 对系统集合写
  一律拒绝 :97 等，不存在旁路）。
- prefs 深度合并/部分更新（整体替换，与 account prefs 一致）。
- 用户组其他设置（头像/描述等）。
- **DeleteGroup 语义说明**：prefs 存于 groups 集合文档内，`DeleteGroup`（groups.go:127-144）
  删文档即连 prefs 一并删除，无独立清理问题。
- 仅 owner 可改用户组偏好的产品收紧（如需，后续在 use-case 用
  `AcceptedGroupRoleLabels` 校验——本期不做，保持与 docDB 权限一致）。

---

## 7. 关键坑（实现时必须注意）

1. **存量补列是核心**：只改 spec 不动 `EnsureSystemCollections` → 存量项目写 prefs
   报 42703。reconcile 必须同时写物理列与 `document_attributes` 元数据；
   **只修"任一缺失"方向**，不做反向校验。
2. **`EnsureSystemCollections` 的 `coll == nil` 分支必须创建集合**（评审阻断 1——
   写错成 continue 会导致全站系统集合永不创建）。
3. **use-case 必须 `resolveProject` 开头**（评审阻断 2）——reconcile 的唯一触发
   入口；漏掉则存量项目永不补列、新项目首个 prefs 请求 500。
4. **错误映射**：`UpdateDocument` 错误必须 `appshared.MapDocumentDBError`（评审
   重要 4）——否则权限拒绝 HTTP 500 而非 403。
5. **reconcile 并发**：元数据 INSERT 撞唯一约束（23505）必须忽略（评审重要 5）。
6. **权限语义**（评审阻断 3）：已接受成员（`group:{id}`）可写 prefs——测试断言
   按此，不要按"成员应被拒"写。
7. **reconcile 比对按 Key** 而非 ID（存量行 ID 可能不符约定）。
8. **scope 登记**：两个新 RPC 必须登记（fail-closed），读/写划分正确。
9. **`structpb.NewStruct` 失败** → `InvalidArgument`（非法 JSON 值如 NaN）。
10. **UpdateGroupPrefs 返回 ok-assert 兜底**（照 account.go:531-534）。
11. **无 wire 变更**：`task wire-all` 后 wire_gen 应无 diff。

---

## 8. 文档同步清单（第 6 步必做）

1. `docs/roadmap.md:117`：§2.3 用户组偏好状态"待办"→"✅ 完成"。
2. **`docs/manual-acceptance-checklist.md:312`**（评审遗漏补充）：移除"Groups prefs
   不在验收范围"行，在 Server Groups 节补一条验收项（GET/PUT prefs 读写 + 权限拒绝
   断言）。
3. `docs/developer/06-databases.md:94`：groups 集合属性表追加 `prefs`（:91 的 users
   行含 prefs 是现成模板）；:101 的 EnsureSystemCollections 描述补"对存量集合幂等
   补列 reconcile"。
4. `docs/developer/12-sdk.md:233`（可选）：Server Groups 段补
   `getGroupPrefs/updateGroupPrefs`（SDK 文档走"示例优先"风格则非必须）。
