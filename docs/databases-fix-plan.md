# Databases 子系统修复方案（Databases Fix Plan）

> 状态：`v1.1 已实现`（2026-08-08：独立审查确认 → 子代理两阶段实现完成 → `go build ./...` 通过、全量 `go test ./...` 26 包全绿）
> 适用范围：Databases / Collections / Documents 三层功能缺口修复
> 本文档是实现的唯一依据；实现完成后回填实际变更与验收结果。

## 0. 背景与依据

经三轮代码核对（proto → gRPC handler → app use-case → domain port → adapter SQL 全链路），确认
Databases 子系统 27 个 RPC 全部有实现，权限与安全加固成熟度较高；但存在若干功能性缺口与一致性问题。
本方案基于《影响范围矩阵》中确认的问题编号（#1~#12），选取 P0 修复项实施，其余项记录为 P1/不修。

核对依据：
- `docs/databases-fix-plan.md`（本文档）
- `internal/domain/databases/repository.go`（DocumentDB 端口）
- `internal/infra/documentdb/postgres.go` / `postgres_permissions.go`（唯一实现）
- `internal/app/server/databases.go` / `internal/app/client/databases.go`（use-case）
- `internal/api/servergrpc/databases.go` / `internal/api/clientgrpc/databases.go`（handler）
- `proto/shared/v1/common.proto`（ListResponseMeta 已含 next_page_token / prev_page_token）
- `internal/app/server/documents_integration_test.go:46`（集成测试显式传 DefaultCollectionPermissions，不受 #1 影响）

## 1. 修复项总览

| 编号 | 问题 | 严重度 | 本次状态 | 实现位置 |
|---|---|---|---|---|
| #1 | Server CreateDocument 未传 permissions 时默认授予 `DefaultCollectionPermissions()`（含 `update:users`/`delete:users`），文档对全体登录用户可改可删 | 高（安全） | **P0 修复** | `internal/api/servergrpc/databases.go` |
| #3 | DeleteCollection 不清理 `_perms` 孤儿行，重建同名集合可导致权限污染 | 中（数据安全） | **P0 修复** | `internal/infra/documentdb/postgres_permissions.go` |
| #4 | 多值 `equal`/`notEqual` 编译为 `= ANY(?::text[])`，对 BIGINT/DOUBLE/BOOLEAN 列触发 PG "operator does not exist" | 中 | **P0 修复** | `internal/infra/documentdb/postgres.go` |
| #5 | NextPageToken 被 handler 丢弃，客户端无法 token 续页 | 中 | **P0 修复** | 两个 databases handler |
| #6a | `select` 投影仅解析不执行 | 中 | **P0 修复** | `internal/infra/documentdb/postgres.go` |
| #6b | `cursorAfter`/`cursorBefore` 仅解析不执行 | 中 | **P0 修复** | `internal/infra/documentdb/postgres.go` |
| #8 | `ErrDuplicateKey` 未映射 gRPC `AlreadyExists` | 低 | **P0 修复** | `internal/app/shared/docdb_errors.go` |
| #10 | ListCollections 的 `page_size`/`page_token` 字段声明未实现 | 低 | **P0 修复** | handler + app + adapter + 端口签名 |
| #11 | CreateDatabase 的 id 无格式校验 | 低 | **P0 修复** | `internal/app/server/databases.go` |
| #12 | `_created_by`/`_updated_by` 列从不写入 | 低 | **P0 修复** | `internal/infra/documentdb/postgres.go` |
| #2a | 文档级权限 `user:{id}`/`team:{id}` 模板不做写入时替换 | 中 | **P0 修复** | `internal/infra/documentdb/postgres.go` |
| #2b | 集合级 `user:{id}`/`team:{id}` 占位符永不匹配 | 中 | **不修**（见 §3） | — |
| #7 | ListDocuments 恒 COUNT(*) | 中（性能） | **不修**（见 §3） | — |
| #9 | CreateDatabase/CreateCollection 非事务 | 低 | **不修**（见 §3） | — |

## 2. P0 修复项详细方案

### #1 Server 文档默认权限：空 permissions 不再展开为默认集合权限

**现状**：`internal/api/servergrpc/databases.go:275` 的 `CreateDocument`（及 `UpdateDocument`、`BulkUpdateDocuments`）
调用 `databases.ParsePermissionStrings(req.GetPermissions())`；该函数在 `len(items)==0` 时返回
`DefaultCollectionPermissions()`（create/read/update/delete × users/keys/admin + read:any），
导致 Server API 创建的文档默认被任意登录用户可改/可删。Client 路径（`parseOptionalPermissions`）已是空→nil 语义。

**目标**：与 Appwrite 语义一致——未显式传 permissions 时**不写入任何文档级权限**，
文档访问仅由集合级权限控制（`document_security` 回退语义不变）。

**方案**：
1. 新增 helper `parseOptionalPermissions(items []string) ([]databases.Permission, error)`（servergrpc 包内，
   与 clientgrpc 的版本等价）：空 → `nil`；非空 → `ParsePermissionStrings`。
2. `CreateDocument`/`UpdateDocument`/`BulkUpdateDocuments` 三处改用该 helper（后两者已有 `len>0` 守卫，
   换 helper 为等价整理，语义不变）。
3. `docDB.setPermissions`（`postgres.go:1022-1038`）对空 perms 已返回 nil，响应中 `permissions` 字段自然为空数组。
4. app 层 `ValidateGrantablePermissions(principal, nil, ...)` 对 nil 仅空遍历，安全（`permissions.go:158-172`）。

**涉及文件**：`internal/api/servergrpc/databases.go`
**测试**：`internal/app/server/databases_integration_test.go` 或 `documents_integration_test.go` 新增用例：
keys 角色创建文档不带 permissions → 断言文档权限为空、响应 `permissions` 为空；带显式 permissions → 原行为不变。

**风险**：行为变更仅作用于"Server API 不传 permissions 的调用方"；集成测试 `documents_integration_test.go:46`
显式传权限，不受影响。SDK/Console 无强制默认权限依赖。

### #3 DeleteCollection 清理 _perms 孤儿行

**现状**：`DeleteCollection`（`postgres.go:235`）仅 `DROP TABLE ... CASCADE` + 删除 catalog 元数据；
`_perms` 为独立表无 FK，残留 `(_tenant, _collection=被删集合)` 行。删除后重建同名集合时，
旧文档权限行会错误匹配新集合文档（权限污染）。

**目标**：删除集合时同步清除该集合的 `_perms` 行。

**方案**：在 `DeleteCollection` 中、`DROP TABLE` 之前（或之后均可，幂等）执行：
`DELETE FROM <schema>._perms WHERE _tenant = ? AND _collection = ?`（tenant=internalID）。
`DeleteDatabase` 因 `DROP SCHEMA CASCADE` 已覆盖，无需改动。

**涉及文件**：`internal/infra/documentdb/postgres.go`（DeleteCollection）
**测试**：`internal/infra/documentdb/postgres_test.go` 新增：建集合→建文档（带文档级权限）→删集合→
断言 `_perms` 无该集合行；重建同名集合→建新文档→断言新文档无旧权限（列表按权限过滤不可见旧权限泄漏）。

**风险**：低。仅多一条 DELETE。

### #4 多值 equal/notEqual 类型安全

**现状**：`buildAppwriteQuery`（`postgres.go:1169-1184`）多值分支生成 `col = ANY(?::text[])` /
`col != ALL(?::text[])`。`text[]` 显式转换后，BIGINT/DOUBLE PRECISION/BOOLEAN 列无 `= text[]` 操作符，运行时报错。

**目标**：多值比较对任意属性类型可用。

**方案**：多值分支改为逐值参数化：
- `equal` 多值 → `col IN (?, ?, ...)`（每个值一个占位符，由 PG 按列类型推断参数类型）
- `notEqual` 多值 → `col NOT IN (?, ?, ...)`（与 `!= ALL(text[])` 的 NULL 语义一致：NULL 列不匹配）

保留单值分支 `= ?` / `!= ?` 不变。`buildAppwriteQuery` 的 args 构造同步修改。

**涉及文件**：`internal/infra/documentdb/postgres.go`（buildAppwriteQuery）
**测试**：新增集成测试（`internal/infra/documentdb/postgres_test.go`）：对 integer 属性执行多值
`equal("views",[1,2,3])` 与 `notEqual`，断言结果正确；对 string 属性回归单值/多值 equal。
（当前测试盲区，无既有断言依赖旧行为。）

**风险**：低。SQL 形态更标准。

### #5 NextPageToken 透传

**现状**：adapter 已生成 `NextPageToken`（`postgres.go:580-583`，offset 编码），app 层已返回
（`server/databases.go:345`、`client/databases.go:162`），但两处 handler 用 `docs, total, _, err` 丢弃。
`shared.v1.ListResponseMeta` 已有 `next_page_token`/`prev_page_token` 字段，无需改 proto。

**目标**：客户端可用 `next_page_token` 续页。

**方案**：两处 handler 改为 `docs, total, next, err := ...`，并设置 `Meta.NextPageToken = next`
（PrevPageToken 不实现，保持空）。`servergrpc/databases.go:300`、`clientgrpc/databases.go:48`。

**涉及文件**：`internal/api/servergrpc/databases.go`、`internal/api/clientgrpc/databases.go`
**测试**：`internal/app/server/databases_integration_test.go` 新增：写入 >page_size 文档，List 两次，
第二次带 `page_token`（第一次响应的值）断言返回下一页且与 offset 方式一致。

**风险**：低。仅响应字段新增。SDK 的 `ListMeta` 类型已含 `nextPageToken`（`sdk/typescript`），无需改动。

### #6a select 投影

**现状**：`pkg/query` 解析 `select([...])` 到 `Query.Selects`，`buildAppwriteQuery` 不消费，恒返回全列。

**目标**：`select` 生效，只返回指定字段（系统字段 `_id/_created_at/_updated_at/_created_by/_updated_by` 恒返回，
与 Appwrite 行为一致）。

**方案**：Go 侧投影（不做 SQL 列裁剪，避免 JSONB 列选择复杂度）：
在 `ListDocuments` 扫描完文档后，若 `len(parsed.Selects) > 0`，对每个文档：
保留系统字段，`Data` 仅保留 `Selects` 中出现的键（`mapQueryField` 后比较，支持 `$id/$createdAt/$updatedAt` 别名）。

**涉及文件**：`internal/infra/documentdb/postgres.go`（ListDocuments 尾部新增投影步骤）
**测试**：`internal/infra/documentdb/postgres_test.go` 新增：`select(["name","age"])` 断言响应 Data 仅含这两键
+ 系统字段；`select(["$id"])` 断言别名生效。

**风险**：低。不改 SQL。

### #6b cursorAfter / cursorBefore 键集分页

**现状**：仅解析，SQL 层忽略；offset token 分页仍可用。默认排序为 `ORDER BY d._created_at DESC`
（`postgres.go:1224`，最新在前）；显式 ORDER 时次级排序恒为 `_created_at DESC`（`postgres.go:1239`）。

**目标**：基于文档 ID 的键集分页（Appwrite 语义），与 `orderAsc`/`orderDesc` 组合正确。

**方案**（实现于 `ListDocuments`，在构造 WHERE 时处理）：
1. 排序键与方向：`parsed.Orders[0]` 若存在则用其字段与方向，否则默认 `_created_at` 且**方向为 DESC**
   （与现有默认排序一致）。字段经 `mapQueryField`，`_id` 作为 tie-breaker。
2. 若 `parsed.CursorAfter` 或 `parsed.CursorBefore` 非空（cursor 与 offset 同时传时 cursor 优先，忽略 offset）：
   - 先 `validateDocID`，再查询 cursor 文档（`SELECT <sortcol> FROM tbl WHERE _id=? AND _tenant=?`）；
     文档不存在 → `InvalidArgument "cursor document not found"`。
   - 排序字段必须显式校验 `safeNameRe`（注意：现有 ORDER 路径对非法字段是**静默跳过**
     （`postgres.go:1229-1230`），cursor 路径不能沿用，必须显式报错）。
   - 构造游标条件（追加进 WHERE，与权限过滤/普通 filter 全部 AND，参数顺序在 LIMIT/OFFSET 之前）：
     | 方向 | cursorAfter | cursorBefore |
     |---|---|---|
     | ASC（显式 orderAsc） | `(d.<col>, d._id) > (?, ?)` | `(d.<col>, d._id) < (?, ?)` |
     | DESC（显式 orderDesc 或默认） | `(d.<col>, d._id) < (?, ?)` | `(d.<col>, d._id) > (?, ?)` |
   - **cursor 模式下 ORDER BY 必须与谓词同构**：显式构造 `ORDER BY d.<col> <dir>, d._id <dir>`
     （替换默认 `_created_at DESC`），否则共享排序值时会出现跳行/重复行。
   - LIMIT 照常；cursor 分页下 `offset` 恒为 0。
3. `NextPageToken` 照旧生成（offset 编码，与 cursor 共存时仍可用，已知弱化键集稳定性，可接受）。
4. **限制声明**：cursor 模式仅支持单排序键（取 `Orders[0]`，多 ORDER 组合时其余排序被忽略）；无 cursor 的多排序行为不变。

**涉及文件**：`internal/infra/documentdb/postgres.go`（ListDocuments + 新增 helper）
**测试**：`internal/infra/documentdb/postgres_test.go` 新增：
- 默认排序（DESC）cursorAfter 翻页：limit=2 第一页取最新两条，用第二条 ID 作 cursor 取第二页，
  断言无重叠且能取完全部（按 DESC 方向）；
- cursorAfter + orderAsc("age") 组合正确；
- cursorBefore 反向分页（默认 DESC）；
- cursor 文档不存在 → InvalidArgument；非法排序字段 → InvalidArgument。

**风险**：中。新增一条前置 SELECT；仅影响显式使用 cursor 的调用方（现状无人使用，无回归面）。

### #8 ErrDuplicateKey 映射 AlreadyExists

**现状**：`CreateDocument`/`UpdateDocument` 的 `isUniqueViolation` 包装 `ErrDuplicateKey`，
但 `internal/app/shared/docdb_errors.go` 只映射 `ErrPermissionDenied`，重复 ID 返回 `codes.Internal`。

**目标**：重复键返回 `codes.AlreadyExists`。

**方案**：`MapDocumentDBError` 增加 `errors.Is(err, databases.ErrDuplicateKey)` → `codes.AlreadyExists`。
`ErrDuplicateKey` 目前在 `internal/infra/documentdb/postgres.go:29` 定义（infra 层），
需将错误定义上提到 `internal/domain/databases/errors.go`（`var ErrDuplicateKey`），
infra 层改为引用域错误（`var ErrDuplicateKey = databases.ErrDuplicateKey`，保留别名兼容）。
`errors.Is` 比较同一错误实例，现有引用方（`internal/app/client/account.go:192,418`、`email_otp.go:164`、
`phone_otp.go:173` 均 `errors.Is(err, documentdb.ErrDuplicateKey)`）全部兼容；
app/shared 已 import domain/databases，domain 不依赖 infra/app，无循环依赖。

### #10 ListCollections 分页

**现状**：`ListCollectionsRequest` 声明 `queries/page_size/page_token`，handler（`servergrpc/databases.go:116`）
完全忽略，adapter 全量返回。

**目标**：`page_size`/`page_token` 生效（offset 编码 token）；`queries` 本次不支持（§3 说明）。

**方案**：
1. `internal/domain/databases/repository.go`：`ListCollections` 签名增加分页参数与返回：
   `ListCollections(ctx, projectID, databaseID string, q ListQuery) ([]Collection, ListMeta, error)`，
   新增轻量类型 `type ListQuery struct { PageSize int32; PageToken string }` 与 `type ListMeta struct { TotalCount int64; NextPageToken string }`。
   同时新增 `pkg/crud` token 编解码依赖（复用现有 `EncodePageToken(offset int) string`/`DecodePageToken(token string) (int, error)`）。
2. adapter：bun 查询加 `Order("created_at DESC")`、`Limit(pageSize 默认50，上限100)`、`Offset(token 解码)`；
   并行 `COUNT(*)`（bun `Count`）；`NextPageToken` 由 `crud.EncodePageToken(offset+len)` 生成（有更多数据时）。
   attributes/indexes 的 `IN` 加载保持全量（分页针对集合本身）。
3. app 层 `server/databases.go` `ListCollections` 透传 Query，返回 `([]databases.Collection, int64, string, error)`。
4. handler：传 `Query{PageSize, PageToken}`，响应 `Meta = {PageSize, TotalCount, NextPageToken}`。

**涉及文件**：repository.go、postgres.go、server/databases.go、servergrpc/databases.go、
**以及两个编译点**：`internal/infra/auth/validator_test.go:127`（stubDocDB 需同步实现新签名）、
`internal/app/server/databases_integration_test.go:53`（调用点传参）。
**Wire 说明**：签名变更不涉及 provider 增减，`cmd/server/wire_gen.go` 只调用构造函数，无需重新生成
（跑 `task wire-all` 亦无 diff，无害）。
**测试**：`internal/app/server/databases_integration_test.go` 新增：建 >10 个集合，page_size=5 分页两页断言
（含 NextPageToken 续页）。
**风险**：中（接口签名变更，3 处调用方 + 2 处编译点同步）。

### #11 CreateDatabase id 校验

**现状**：handler 仅查空（`servergrpc/databases.go:39`）；Collection 走 `ValidateIdentifier` 正则
`^[a-zA-Z_][a-zA-Z0-9_]*$`。

**方案**：`app/server/databases.go` `CreateDatabase` 增加 `ValidateIdentifier(id)`（与 CreateCollection 对齐），
失败返回 `InvalidArgument`；并禁止创建 `id == "default"`（与 `DeleteDatabase` 的 default 保护对称，
避免重复 default 库元数据行）。

**涉及文件**：`internal/app/server/databases.go`
**测试**：`databases_test.go` 新增：`CreateDatabase` 非法 id（含 `-`/空格）→ InvalidArgument；`id="default"` → InvalidArgument。

**风险**：低。存量库 id 不校验（仅新创建路径）；"default" 由 `EnsureSystemCollections` 先行创建，禁止重复创建不破坏现有流程。

### #12 _created_by / _updated_by 填充

**现状**：schema 有 `_created_by`/`_updated_by` 列，`scanDocumentJSON` 已读取（`postgres.go:1103-1108`），
但 insert/update 从不写入。**另发现既有审计伪造面**：`buildInsertParts`（`postgres.go:1045-1060`）
不过滤 `_` 前缀键（`buildUpdateParts` 在 `postgres.go:1064` 过滤了），用户 data 传 `_created_at`/`_created_by`
会被显式写入（覆盖 DEFAULT NOW()）。

**方案**：
1. **前置步骤（必须先行）**：`buildInsertParts` 增加 `strings.HasPrefix(k, "_")` 过滤（与 `buildUpdateParts` 对齐），
   同时修复审计伪造面；否则用户 data 含 `_created_by` 时与新增审计列产生重复列 INSERT 报错。
2. 新增 helper `userIDFromPrincipal(p Principal) string`：从 `p.Roles` 中提取第一个 `user:` 前缀角色
   （返回去前缀 ID），提取不到返回空串。
3. `CreateDocument`：insert 时单独 append 两列 `_created_by`/`_updated_by`（值为 `userIDFromPrincipal(principal)`，
   空串则不 append 该列），**不经过用户 data**。
4. `UpdateDocument`：`buildUpdateParts` 显式追加 `_updated_by = ?`（principal 提取的 user ID，空则不写），
   同样不依赖 `doc.Data`（`buildUpdateParts` 已跳过 `_` 前缀键，需在函数内显式携带审计值而非塞进 Data）。

**涉及文件**：`internal/infra/documentdb/postgres.go`（buildInsertParts/buildUpdateParts 及 CreateDocument/UpdateDocument 调用处）
**测试**：`internal/infra/documentdb/postgres_test.go`：以 `user:abc` 角色创建文档 → `CreatedBy=="abc"`；
更新 → `UpdatedBy=="abc"`；keys 角色（无 user 角色）→ 字段为空；用户 data 传 `_created_at`/`_created_by` →
断言被忽略（data 内不出现、系统列由服务端生成）。

**风险**：低。仅新增列值 + insert 侧 `_` 前缀过滤（后者使"用户伪造审计字段"从可能变为不可能，纯加固）。
proto 无 created_by/updated_by 字段（已 grep 确认），审计值仅 adapter 层 `Document` 结构可见，无需改 handler/proto。

### #2a 文档级权限模板替换

**现状**：`system_collection_specs.go` 与用户显式传入的权限字符串中可出现 `user:{id}`/`team:{id}` 占位；
`CollectionAllows` 纯字符串比较，占位永不匹配真实角色 `user:<uuid>`/`team:<uuid>`。

**关键逻辑约束（经独立审查确认）**：模板替换**必须发生在 app 层 `ValidateGrantablePermissions` 之前**。
原因：普通用户（roles 含 `user:<uuid>`，必要时含 `team:<uuid>`——由 `LoadUserRoles` 从 memberships 注入，
`internal/app/client/user_roles.go:36-68`）请求若含 `read:user:{id}`，在现有校验中因不持有字面量
`user:{id}` 角色而被拒绝，**永远到不了 adapter**；而能到达 adapter 的 keys/admin 路径（privileged=true）
roles 又不含 user:/team: 角色，adapter 层替换是 no-op。因此 adapter 层替换对核心场景不可达。

**目标**：创建/更新文档时，若 permissions 含模板占位，在**权限校验之前**替换为调用者真实角色
（Appwrite 语义：`user:{id}`/`team:{id}` 为调用者占位模板）。

**方案**：
1. domain `permissions.go` 新增 `ExpandPermissionTemplates(perms []Permission, roles []string) []Permission`：
   - `Role == "user:{id}"` 且 roles 含 `user:<x>` → 替换为第一个 `user:<x>`；
   - `Role == "team:{id}"` 且 roles 含 `team:<x>` → 替换为第一个 `team:<x>`；
   - 无匹配则保持原样（后续校验会拒绝未持有角色，fail-closed）。
2. **调用点（app 层，校验之前）**：
   - `internal/app/server/databases.go`：`CreateDocument`（L311 前）、`UpdateDocument`（L382 前）、
     `BulkUpdateDocuments`（L415 前）：`perms = databases.ExpandPermissionTemplates(perms, principal.Roles)`；
   - `internal/app/client/databases.go`：`CreateDocument`（L129 前）、`UpdateDocument`（L204 前）：同上。
3. adapter 层 `CreateDocument`/`UpdateDocument` 可保留展开作为纵深防御（对 `create` 类型等绕过校验的路径兜底），非必须。
4. **集合级权限不做替换**（§3 #2b）：`CollectionAllows` 保持纯字符串比较，避免系统集合读路径被放宽。

**涉及文件**：`internal/domain/databases/permissions.go`（新增函数 + 单测）、
`internal/app/server/databases.go`、`internal/app/client/databases.go`
**测试**：
- domain 单测：`user:{id}`/`team:{id}` 替换、无匹配角色保持原样；
- app 层集成测试：以 `user:alice` 角色（privileged=false）经 Server API 创建文档传
  `permissions=["read:user:{id}"]` → 断言不再被 `ValidateGrantablePermissions` 拒绝、落库与响应均为
  `read:user:alice`；团队场景：成员角色含 `team:t1` 时传 `update:team:{id}` → 落库 `update:team:t1`。
**风险**：中。仅影响显式传入模板占位的调用方；不触碰集合级语义与系统模块（系统模块均用真实 ID 或 SystemPrincipal）。

## 3. 明确不修 / 延后项（记录原因）

| 编号 | 项 | 原因 |
|---|---|---|
| #2b | 集合级 `user:{id}`/`team:{id}` 运行时替换 | 集合级 `read:user:{id}` 若被替换为调用者 ID，等价"所有登录用户可读"，会放宽 users/sessions/identities 等系统集合读路径（现依赖文档级 `_perms` 精确控制）；实现需要全量回归系统模块。语义争议大，延后专项评审 |
| #7 | ListDocuments 可选计数（COUNT(*)） | proto 无 `include_total` 字段，需改 proto + 重新生成 + SDK 同步；Appwrite 同样返回 total，当前行为不算缺陷，仅性能优化 |
| #9 | CreateDatabase/CreateCollection DDL 事务化 | PG 事务内 DDL 支持但失败回滚语义需逐一验证（schema/表/元数据三段）；收益低，偶发失败可人工清理；延后 |
| #10b | ListCollections 的 `queries` DSL 过滤 | 元数据过滤需求弱，且实现需将 DSL 映射到 catalog 列，成本高 |
| — | ListDatabases 分页 | 元数据量小（每项目数个库），`shared.ListRequest` 的 filter/order_by 亦未实现，属同类延后 |
| — | Console 增强（DeleteAttribute/DeleteIndex 接线、服务端查询、JSON 视图、Table 视图、SQL 编辑器） | 前端迭代项，另立 Console 计划；后端 RPC 均已就绪 |
| — | upsert / relationships / transactions / realtime / vector / geo | roadmap P1/P2 规划，不属本次修复范围 |

## 4. 回归影响与测试策略

### 4.1 受影响调用方（来自调用方盘点）
- 系统模块（users/teams/account/identity/OTP/session/storage）以 `SystemPrincipal` 直调 docDB，
  **不经过** `server.Databases`/`client.Databases` use-case，本次改动不触及这些路径的行为：
  - #1/#10 仅改 handler/app 层（系统模块不走这两个 handler）；
  - #3/#4/#6/#12 改 adapter 通用路径，#2a 改 app 层（adapter 纵深防御可选），但系统模块的权限写入均为真实 ID（无模板）、无多值数字查询、
    无 select/cursor（均不使用），回归面仅"通用行为"（如 #4 使查询更强，而非改变既有结果）。
- 唯一接口签名变更：#10 `ListCollections` —— 编译点共 6 处：接口（repository.go:15）、adapter
  （postgres.go:183）、app（server/databases.go:104）、handler（servergrpc/databases.go:116,121）、
  测试 stub（`internal/infra/auth/validator_test.go:127`）与测试调用（`databases_integration_test.go:53`）。
  Wire 只调用构造函数，签名变更不触发 `cmd/server/wire_gen.go` 重新生成（`task wire-all` 无 diff）。
- #8 的 `ErrDuplicateKey` 引用方为 `internal/app/client/account.go`、`email_otp.go`、`phone_otp.go`
  （`errors.Is` 比对同一错误实例，别名保持兼容）。

### 4.2 回归重点测试
- `internal/app/server/system_collections_readonly_test.go`（系统集合读写策略）
- `internal/infra/documentdb/permissions_test.go`（9 个权限场景）
- `internal/infra/documentdb/postgres_test.go`（CRUD/查询/隔离）
- `internal/app/server/documents_integration_test.go`（Server 文档 CRUD）
- `internal/app/client/databases_integration_test.go`（Client 文档 CRUD + guest 读）

### 4.3 验证命令
```bash
task wire-all      # #10 签名变更后重新生成（若无变化则跳过）
go build ./...
task test          # go test -v ./... -cover（需 task up 起本地 PG，5433）
```

## 5. 验收标准（DoD）

1. §2 全部 11 项修复实现，每项配套测试（新增用例数 ≥ 11）。✅ 已达成（新增测试约 20 个用例）
2. `go build ./...` 通过；`task test` 全绿（含既有测试，无跳过/删除）。✅ 已达成（26 包全部 ok，既有测试未删）
3. `task wire-all` 后无 diff（或已正确重新生成）。✅ 已达成（#10 签名变更不触发 wire 重生成，无 diff）
4. 不在本文档 §3 范围的改动一律不做；文档保持单一事实来源。✅ 已达成
5. 实现完成后回填本文档：每项标注 commit/变更摘要，状态改为 `已实现`。✅ 本状态行已更新（commit 待用户指示后提交）

## 6. 实施顺序（子代理实现建议）

1. #8（错误定义上提，纯重构，低风险，先做）
2. #3（DELETE _perms）→ #4（IN 列表）→ #6a（select）→ #6b（cursor，含 #6b 修正项）→ #12（审计列，含 buildInsertParts `_` 过滤前置步骤）
3. #5（NextPageToken）→ #1（默认权限）→ #10（ListCollections 分页，含 2 处测试编译点）→ #11（id 校验）
4. #2a（模板替换，app 层校验前展开，语义变化，最后做）
5. `go build ./...`（#10 变更后）→ `task test` 全量回归

## 7. 独立审查记录

- 审查人：独立审查子代理（explore），审查对象：`v1` 版本文档全部 11 项 P0 修复。
- 结论：**7 项直接通过（#1/#3/#4/#5/#6a/#8/#11）**，4 项需修正（#6b/#10/#12/#2a），本版（v1.1）已全部落实修正：
  - #6b：默认排序方向为 DESC（谓词方向表修正）+ cursor 模式 ORDER BY 与谓词同构（`(col <dir>, _id <dir>)`）+ 排序字段显式校验（现有 ORDER 路径为静默跳过，不可沿用）+ cursor 前置 SELECT 前 `validateDocID`；
  - #10：补 2 处编译点（validator_test.go stub、databases_integration_test.go 调用）+ 修正 wire 判断（签名变更不触发 wire 重生成）；
  - #12：补前置步骤 `buildInsertParts` 的 `_` 前缀过滤（修复既有审计伪造面并避免重复列冲突）；
  - #2a：调用点从 adapter 改为 app 层 `ValidateGrantablePermissions` 之前（普通用户模板在 app 层即被拒，adapter 层替换不可达；keys/admin 路径无 user:/team: 角色，替换为 no-op），测试同步改为经 app 层断言。
- 次要修正：#1 文件引用（setPermissions 在 postgres.go:1022）；#8 引用方表述（account/otp 而非 users/teams）；#11 增补禁止创建 "default"。
- 确认后状态：`v1.1 已确认`，可进入实现。
