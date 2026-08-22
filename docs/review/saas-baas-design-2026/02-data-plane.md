# 数据面设计评审（SaaS BaaS）

> 日期：2026-08-22。只读，以代码为准，不采信文档里已拍板的决策编号。  
> 产品：给 SaaS 团队用的应用数据原语（Database → Collection → Document），外加 Auth/Storage 留下的系统表。  
> 词汇：模块、接口、深度、缝、适配器、杠杆、局部性。  
> 范围：`internal/domain/databases/`、`internal/infra/documentdb/`、`internal/app/documents/`、`internal/app/{server,client}/databases.go`、`pkg/query/`、`pkg/ident/`、`internal/infra/projectschema/`、`internal/infra/bun/{model,bunrepo}/users*`、`internal/domain/events/`、`internal/infra/events/`。

追踪过的代表路径：Server `CreateCollection` → adapter DDL+catalog；Client/Server `CreateDocument` → `_perms` + outbox；`ListDocuments` ACL SQL 下推；系统 `users` 表 vs 用户集合 `posts`；查询 string DSL vs proto AST。

---

## 现状

物理布局是三层，不是「一个 DocumentDB」：

| 层 | 物理位置 | 谁在读写 |
|---|---|---|
| 控制面 | `public`（projects、outbox、…） | bun 控制面仓库 |
| 项目数据面 | `tw_<project.id>`（一段式） | catalog 表 + 系统静态表 `users`/`sessions`/…（bun，无 `_id`/`_perms`/`_version`） |
| 业务文档面 | `tw_<project.id>_<database.id>`（两段式） | 用户 collection 真表 + 每库一张 `_perms` |

建项：`CreateProject` 在同一事务里 `CREATE SCHEMA tw_<id>`、`projectschema.Apply`、`CreateDatabase(firstID 默认 "default")`（`internal/app/server/projects.go:87-103`）。`Apply` 把 catalog 与系统表迁进项目 schema（`internal/infra/projectschema/migrations/000001_catalog.up.sql`、`000008`/`000009`）。对外 `database_id="_"` 被 `RejectExternalDatabaseID` 拒绝（`internal/app/shared/database_id.go:25-32`）；`DeleteDatabase` 只走两段式，硬拒一段式（`internal/infra/documentdb/postgres.go:1516-1532`）。

文档调用链：

```
gRPC handler
  → app/server.Databases 或 app/client.Databases（策略：DDL 守卫 / guest 读 / owner ACE）
    → app/documents.Documents（OCC、grant、ResolveQuery）
      → databases.DocumentDB（接口上拆成 Catalog / SchemaApplier / Documents）
        → 唯一适配器 postgresDocumentDB（postgres.go 2592 行 + postgres_permissions.go）
```

`users` 不再走这条链：`bunrepo.UserRepository` 经 `Scoped(..., "users")` 打到 `tw_<project>.users`（`internal/infra/bun/bunrepo/users_repo.go:65-74`）。用户集合 `posts` 是 `tw_<project>_<db>.posts`，列是 `_id/_tenant/_created_at/_updated_at/_created_by/_updated_by/_version` + 声明属性（`postgres.go:1597-1617`）。

ACL：集合权限存在 catalog 行的 `permissions TEXT[]`；文档权限存在业务 schema 的 `_perms`。`document_security` catalog 默认 `TRUE`（`000001_catalog.up.sql:19`），handler 未设置时也默认 true（`internal/api/servergrpc/databases.go:119-123`）。List 必须把 `_perms` 编进 SQL，禁止 fetch-then-Check（`internal/domain/databases/repository.go:28-29`，实现 `postgres_permissions.go:89-127`）。

查询双栈：`queries[]string`（Appwrite DSL）与 `optional shared.v1.Query`（typed AST）。`documents.ResolveQuery` 优先 AST，两者同时带谓词则 InvalidArgument（`internal/app/documents/query.go:27-56`）。

写事件：用户集合的 create/update/delete 在同一 `RunInTx` 里 `Publish` 进 `public.document_events_outbox`（`postgres.go:1082-1122`，`internal/infra/events/outbox.go:36-72`）。系统集合不发。

---

## 设计问题

### 1. Catalog / SchemaApplier / Documents 是假缝；DocumentDB 仍是神模块

**事实。** 领域端口拆成三个接口，但 `DocumentDB` 把它们嵌回去，并写明「现有注入点多数不用改签名」（`internal/domain/databases/repository.go:4-54`）。生产适配器只有一个结构体 `postgresDocumentDB`（`postgres.go:69-86`），Wire 只提供 `NewPostgresDocumentDB`（`internal/infra/documentdb/provides.go:5-7`）。全仓库没有以 `databases.Catalog` / `SchemaApplier` / `Documents` 为参数类型的消费方。测试桩至少 11 处仍 `var _ databases.DocumentDB = ...`（account、functions、realtime、pagination、cascade、client databases 等）。

`postgres.go` 把这些塞进同一适配器：schema DDL、catalog CRUD、`_version` reconcile/cache、文档 CRUD、OCC、`_perms` SQL、Appwrite/AST 编译、outbox 发布、系统集合写保护、敏感列黑名单。`postgres_permissions.go` 只是把 ACL 过滤和 Bulk 循环拆到第二文件，不是第二个适配器。

**判断。** 拆接口而没有拆适配器、没有拆注入，不是深度，是注释。Catalog 读、DDL、文档读写是三个变更速率、三种失败模式（元数据 vs AccessExclusiveLock vs 行锁+ACL），却共享同一缓存、同一 `conn(ctx)`、同一 `IsSystemCollection` 分支。杠杆在 2600 行文件里，不在端口上。对 SaaS BaaS 来说，应用数据平面的模块边界应该是「catalog 服务 / schema 迁移器 / 带 ACL 的文档存储」三块可独立替换的适配器；现在替换存储等于重写整座神庙。

### 2. `_perms` 不是 ACL 模块，而是渗进 Documents 每个方法的横切

**事实。** `Documents` 每个方法都带 `principal Principal`（`repository.go:28-47`）。`Principal` 定义在 `domain/databases` 以免依赖 `domain/shared`（`access.go:1-48`）。鉴权逻辑散在：

- 领域谓词 `AllowsDocumentAccess` / `SkipDocumentPermissionFilter` / `ListAccessDenied`（`permissions.go:65-138`）
- adapter `checkDocumentPermission` / `listPermissionFilter` / `ensureCollectionAccessible`（`postgres_permissions.go:13-127`）
- 用例层 `ensureCollection`、guest vs 认证、`ValidateGrantablePermissions`
- 事件投递再实现一遍 `VisibleTo` → 同一套 `AllowsDocumentAccess`（`internal/domain/events/envelope.go:124-138`）

默认 `document_security=true` 时，即使用户集合已有集合级 `read`，`SkipDocumentPermissionFilter` 仍返回 false（`permissions.go:123-138`），List/Count/Sum 每行都走 `_perms` 相关子查询（`postgres_permissions.go:114-120`，接到 `ListDocuments` 的 `postgres.go:1152-1160`）。List 扫描后**不** `attachDocumentPermissions`（对比 Get 的 `postgres.go:845-850`）；列表响应里 `permissions` 恒空。

**判断。** 深度够的 ACL 模块：输入是「主体 + 资源 + 动作」，输出是「允许 / 过滤谓词」，存储与查询编译在缝的另一侧。现在 ACL 的接口就是 Documents 方法签名本身——换存储必须懂角色展开、`any` 合成、documentSecurity 覆盖语义、系统集合 OR 豁免。SQL 下推本身是对的（禁止 fetch-then-Check），但下推被焊死在文档适配器里，没有独立的「ACL 索引」模块。SaaS 应用数据的每一条 List 都为 `_perms` 付相关子查询，而调用方连列表上的 ACE 都拿不到。

### 3. 集合默认 `read:any` 对「SaaS 应用数据」是错误产品面

**事实。** 空权限列表会展开成 `DefaultCollectionPermissions()`，其中包含 `{Type: "read", Role: "any"}`，以及 users/keys/admin 的写（`permissions.go:12-28, 147-149`）。`ExpandPermissionRoles` **无条件**注入 `"any"`（`permissions.go:34-56`），因此任何主体（含 guest）都能匹配 `read:any`。Server `CreateCollection`：handler 解析空 permissions → 默认集，用例层 `len(perms)==0` 再套一次默认集（`internal/api/servergrpc/databases.go:115-123`，`internal/app/server/databases.go:133-136`）。Client `ListDocuments` / `GetDocument` / `CountDocuments` 标 `ACCESS_PUBLIC`（`proto/client/v1/databases.proto:69-86, 111-126`），未登录走 `GuestPrincipal{Roles:["guests"]}`（`internal/app/client/databases.go:72-93`，`access.go:18-19`）。

文档级默认不对称：Client 空 permissions 写成 `read/update/delete:user:<id>`（`internal/app/client/databases.go:142-144, 223-229`）；Server 空 permissions 表示**无文档 ACE**（`parseOptionalPermissions` 空列表返回 nil，`internal/api/servergrpc/databases.go:614-621`）。`documentSecurity=true` 且 `docHasPerms=false` 时回落到集合 ACL（`permissions.go:90-108`）→ Server/API Key 建出来的行对 guest 世界可读。

**判断。** 这是 Appwrite 式「公开集合 + 文档可再收紧」的博客/社区默认，不是 SaaS 应用数据默认。SaaS 团队往这里放的是订单、租户、账单、配置；他们期望的默认是「认证用户不可见他人行，匿名不可见任何行」。Client owner ACE 只保护 Client 创建路径；Functions / Server API / 漏传 permissions 的 SDK 调用会 silently 公开。`read:any` 作为集合默认，等于把公开读做成了零配置行为，而不是显式的 `read:any` 开关。

### 4. 双查询语言把接口做宽，没有把查询模块做深

**事实。** 传输层同时暴露：

- `repeated string queries`（Appwrite 字符串 codec：`equal("k","v")`，隐式 AND；有 `isNull` / `between` / `select` / `limit` / `offset` / `cursorAfter`）（`pkg/query/query.go:184-379`）
- `optional shared.v1.Query`（typed 树：eq/ne/…/and/or；**没有** isNull/between/select/cursor）（`proto/shared/v1/query.proto:18-51`，`pkg/query/proto/proto.go:12-46`）

领域 `databases.Query` 两者都扛（`document.go:44-50`）。`ResolveQuery` 与 adapter `astFrom` **各实现一遍**双栈冲突规则（`internal/app/documents/query.go:27-34`，`postgres.go:2405-2436`）。Client List handler 先 `ResolveQuery` 只为填 `meta.page_size`，再把**未折叠**的 `q` 交给用例，用例再 `ResolveQuery` 一次（`internal/api/clientgrpc/databases.go:54-62`，`internal/app/documents/documents.go:62-76`）。AST 自身还有 `Filter` 树和 `Filters` 叶列表两套（`pkg/query/query.go:57-58, 100-112`）；编译器优先树，否则把叶 AND 起来（`postgres.go:2438-2451`）。

字符串 codec **不能**表达 OR；proto **不能**表达 isNull/between/cursor。`users` 列表又走第三条：`ParseUserList` 白名单三算子六列，仍用 `ParseMany` 字符串（`internal/domain/users/list.go:14-35`）。分页同时存在 `page_token`、DSL `offset`、`cursorAfter/Before`（`postgres.go:1178-1248`）。

**判断。** 查询模块的深度应该是：一个 AST、一个编译器、一个分页。Codec 是适配器（HTTP 查询串、proto、SDK builder），不应出现在领域 Query 和 SQL adapter 入口上。现在调用方必须知道「两套不能同时带谓词」「OR 只能走 proto」「cursor 只能走字符串」「page_token 才是权威」。接口变宽，杠杆没有增加——SQL 编译已经吃 AST（这点够深，见发现 13），双栈是把旧 codec 焊死在新模型上。

### 5. schema-per-project × schema-per-database 对 SaaS BaaS 运维过重；`_tenant` 是过期隔离

**事实。** 每个项目至少一个 PG schema（`ident.ProjectSchemaName`，`ident.go:36-51`），每个业务库再一个（`ident.SchemaName`，`ident.go:68-81`）。`project.id` / `database.id` 最长 28，因为 `tw_`+28+`_`+28 必须塞进 PG 标识符 63 字节（`ident.go:8-18, 27-28`）。每库 `CREATE SCHEMA` + `_perms` 表（`postgres.go:94-125, 1567-1594`）。catalog 不在业务 schema，而在项目 schema 再拷一份 `document_{databases,collections,attributes,indexes}`（`000001_catalog.up.sql:4-62`）。启动 `EnsureAll` 对全部项目 Apply，4 路并行、30s 软超时（`internal/infra/projectschema/migrator.go:126-130`，`startup.go:12-41`）。

与此同时，每张 collection 表仍有 `_tenant BIGINT NOT NULL DEFAULT <internal_id>`，主键 `(_tenant, _id)`，所有文档 SQL 都带 `d._tenant = ?`（`postgres.go:1597-1617, 1150-1151`）。在两段式 schema 里，一个 schema 内所有行的 `_tenant` 是常数。

**判断。** 给「SaaS 团队」用的 BaaS，租户规模是项目数（客户数），不是终用户数。schema-per-project 在数百～数千客户时可接受（DROP 干净、catalog 隔离）。再按 API 的 `database` 原语物理建 schema，是把产品分层 1:1 映到 PG 命名空间：一客户 5 个库 = 1+5 个 schema，另加 catalog 四张表、每库 `_perms`、每集合一张表。`DeleteDatabase` = `DROP SCHEMA CASCADE` 很爽，`pg_dump`、连接 `search_path`、migration 脏标记、标识符长度全都为这个映射买单。

`_tenant` 在 schema 已隔离之后没有隔离作用，只是防御「打错 schema」和加宽主键。正确的租户物理模型要么：（a）项目 schema 内用 `database_id` 做逻辑库、表名前缀或 catalog 外键，DROP 走逐表；（b）继续 schema-per-database，但去掉常数 `_tenant`。现在两者的成本都付了。

### 6. 系统表已经 cut，DocumentDB 与 Users 用例仍背文档投影

**事实。** `users` 物理表是 `id` PK、无 `_id`/`_perms`/`_version`（`internal/infra/bun/model/users.go:10-28`，`000008_system_tables.up.sql:4-20`）。Auth 走 `users.Repository`。Databases API 对外不能以 `_` 寻址系统集合。

同一适配器仍保留：sentinel → 一段式映射（`postgres.go:1500-1514`）、sentinel 上只许系统名单建集合（`postgres.go:204-206`）、`systemCollectionsWriteProtected`（`postgres.go:42-57`）、`IsSystem` 跳过 `_version` 与 outbox（`postgres.go:1606-1608, 1097-1106`）、`sensitiveQueryFields` 按集合名黑名单（`postgres.go:2297-2304`）、D1 系统集合 ACL 为 OR 而非覆盖（`permissions.go:102-106`）。Server `Users.ListUsers` 仍把 bun 行 `userAsDocument` 成 `[]databases.Document`（`internal/app/server/users.go:65-80, 396-401`）。`databases.Query` 继续作为 Users 列表参数。

**判断。** 系统资源与应用文档已经是两条物理路径，这是对的（见发现 12）。留下的分支把 DocumentDB 的接口钉在「曾经万物皆文档」的形状上：调用方仍要知道 sentinel、系统名单、写保护、无 version。Users 对外投影成 Document，是假局部性——改用户列要同时懂 bun 模型、DocumentData、以及仍在用 string DSL 的 `ParseUserList`。对 SaaS 团队，`users` 是 Auth 原语，`posts` 是 Database 原语；缝应该在用例边界，而不是 adapter 里的 `if isSystem`。

### 7. Client / Server databases 在 E-2a 之后不是双实现，但仍是双包装

**事实。** 文档 CRUD 已进 `internal/app/documents`（OCC、grant、`ResolveQuery`、`MapDocumentDBError`）。Server 用例 = DDL/Bulk + `documentsCore()` + `AllowPrivilegedGrant`（`internal/app/server/databases.go:20-38, 356-368`）。Client 用例 = 无 DDL、guest 读、空 ACE → owner（`internal/app/client/databases.go:131-146`）。Proto 仍是两份 `ListDocumentsRequest`（`queries` + `query` 字段镜像：`proto/client/v1/databases.proto:175-185`，`proto/server/v1/databases.proto` 对应 List）。两套 gRPC handler 各自 `BindListQuery`。

**判断。** 「Server 管 schema 与特权写、Client 管终用户文档」是 BaaS 该有的产品缝，不是事故。事故是传输层把同一文档动词复制了两遍，查询双栈在两侧各接一次。再抽一层价值很小；该收的是 Query 入口与 Document 消息（已部分共享 `shared.v1.Document`），而不是再合并两个 Databases 用例。

---

## 能力缺失

### 8. 没有 relation：SaaS 应用的图在数据面不存在

**事实。** 属性类型白名单是 `string, integer, float, boolean, datetime, email, url, json`（`internal/app/server/databases.go:493-503`）。`pgTypeFor` 映到标量/`JSONB`（`postgres.go:1848-1867`）。`array=true` 在 adapter 被拒绝（`postgres.go:448-450`）。全仓库没有 relationship / 跨集合 join API。业务表在 `tw_<p>_<db>`，`users.id` 在 `tw_<p>.users`，不能 FK。文档插入只把 `doc.Data` 里非 `_` 键写成列，不校验 required、不校验引用（`postgres.go:2075-2089`）。

**判断。** SaaS 产品的数据几乎都是图：org → membership → user，order → line → sku。BaaS 若不提供 relation，团队只能把 `user_id` 当字符串存，自己保证完整性，自己做 N+1。这不是「第一版可以没有 join」的细节，而是数据原语比 Postgres 还浅——用了真表却故意丢掉 FK 与 JOIN。对比：Auth 系统表内部已经有 `REFERENCES ... ON DELETE CASCADE`（`000008_system_tables.up.sql:30`）。应用数据面反而更弱。

### 9. 没有跨文档事务

**事实。** Proto 无 Transaction RPC。单文档 Create/Update/Delete/Upsert 各自 `RunInTx`（行 + `_perms` + outbox）（`postgres.go:525-540, 854-869, 1005-1014`）。Bulk 是**同一集合**上循环 `UpdateDocument`/`DeleteDocument`，且 `SkipVersion: true`（`postgres_permissions.go:180-241`）。跨集合、跨库没有工作单元入口。`pkg/uow` 用在 payments/assets，不在文档用例。

**判断。** SaaS 写路径的典型形状是「建订单 + 多行明细」或「扣库存 + 写流水」。现在每篇文档是一个事务。Bulk 还放弃 OCC（发现 10）。没有多文档事务，Database 原语只能做单行 CRUD 商店，扛不住主交易路径。

### 10. 集合 schema 演化只有 ADD / DROP COLUMN

**事实。** SchemaApplier 有 `CreateAttribute` / `DeleteAttribute` / `CreateIndex` / `DeleteIndex`，没有 Rename、没有改类型、没有改 required/default（`repository.go:15-26`）。`CreateCollection` proto 不带 attributes，handler 传 `nil, nil`（`internal/api/servergrpc/databases.go:123`）；加列是事后 `ALTER TABLE ... ADD COLUMN IF NOT EXISTS`（`postgres.go:439-489`）。`attributeColumnSQL` 在 `Required` 时拼 `NOT NULL`（`postgres.go:1835-1837`）——对已有行的表，这会直接 DDL 失败。`DeleteAttribute` 丢列并删 catalog 行，不清理依赖的 `document_indexes`（`postgres_permissions.go:244-270`）。没有版本化的 collection migration 历史（项目 schema 的 `schema_migrations` 只管 Torchwood 自己的系统 DDL）。

**判断。** SaaS 产品会改 schema。BaaS 至少需要：可空加列、回填、改名、安全的类型拓宽、索引随列走。现在把「在线 ALTER」暴露成产品 API，却没有迁移计划、没有 expand/contract。团队要么锁表赌 ADD NOT NULL，要么放弃 required，在应用层校验——而应用层插入路径并不读 catalog 做 required 检查。

### 11. OCC `_version` 对单行并发够用，对 SaaS 写模型不够

**事实。** 用户集合 Update：`ExpectedVersion` 必填，`UPDATE ... WHERE _id AND _tenant AND _version = ?`，0 行再区分 not found / mismatch（`postgres.go:872-976`，`document.go:71-85`）。Delete：`SELECT _version ... FOR UPDATE` 再比较（`postgres.go:1035-1056`）。Create 后 version 从 DEFAULT 1 起。系统集合无此列。

旁路：Bulk 明确 SkipVersion 仍 `+1`（`postgres_permissions.go:193-194`）；Upsert 更新支「盲写（不做 OCC），但用户集合 `_version` 仍 +1」（`postgres.go:750-755`）。Update 权限检查与 OCC 更新之间没有 `FOR UPDATE`（与 Delete 不对称）。List 返回 `version`（`scanDocumentJSON`，`postgres.go:2178-2184`）但不返回文档 ACE。

**判断。** 单文档「读-改-写」冲突检测，这套够：客户端带 version，mismatch 可重读。SaaS 并发编辑还需要：（1）列表就能拿到 ACE 与 version，才能做授权 UI 与批量编辑；（2）Upsert/Bulk 不能静默 LWW，否则后台同步与人工编辑互踩；（3）跨行不变量（发现 9）不是 OCC 能补的。现在 OCC 是单行补丁，不是并发控制模块。

---

## 已够深

### 12. 系统表与业务文档的物理分叉已经硬编码，而且测过

**事实。** `businessSchema` 拒绝 sentinel、拒绝非两段式、拒绝与 `ProjectSchemaName` 相等的名字，避免 `DROP SCHEMA` 打到 `tw_<project>`（`postgres.go:1516-1532`）。`DeleteDatabase("default")` 集成测试断言 `tw_<pid>.users` 仍在、`tw_<pid>_default` 被 DROP（`internal/infra/documentdb/data_plane_schema_test.go:24-64`）。catalog 读路径不再 `Apply`（`GetCollection` 只 SELECT，`postgres.go:246-261`）；`EnsureCatalog` 是 SchemaApplier 上唯一允许 `projectschema.Apply` 的读旁路（`postgres.go:1451-1461`，`repository.go:13-14`）。

**判断。** 这一刀值得留。Auth/Storage 的表不是文档，不该有 `_perms`/`_version`，不该能被 `DeleteDatabase` 误伤。SaaS 团队看到的 Database 原语从 `default` 起，可删可重建；用户账号活在项目数据面。伤疤（发现 6）是接口没跟着物理模型改，不是分叉本身做错。

### 13. 单文档写路径：行 + `_perms` + outbox 同事务；查询编译已吃 AST

**事实。** Create 注释写明权限失败必须回滚，避免文档 fail-open（`postgres.go:543-544, 525-540`）。`publishDocumentEvent` 在外层 `RunInTx` 内、读回之后插入 outbox，系统集合 no-op（`postgres.go:1082-1122`）。outbox `Publish` 复用 `clients.InTx`（`internal/infra/events/outbox.go:67-72`）。信封带 ACL 快照供投递过滤，出站剥掉 acl（`internal/domain/events/envelope.go:23-31, 87-88`）。SQL 编译 `compileFilter` 递归 AND/OR，值全部绑定，标识符 `quoteIdent` + `safeNameRe`（`postgres.go:2478-2516, 2518-2540`）。List ACL 是 EXISTS 下推不是应用层过滤。

**判断。** 单行文档存储作为模块，这条路径是深的：调用方只说 Create/Update/Delete，适配器保证原子、可订阅、可按 ACL 过滤。缺的是模块边界（发现 1–2）和多行/关系（发现 8–9），不是这条路径写得浅。

### 14. `app/documents` 是文档用例该有的核；OCC 在 Update/Delete 上是真约束

**事实。** Client/Server 文档写都进同一 `Documents` 核：空 data 拒绝、grant 模板展开、`UpdateDocumentVersionRequired`（`internal/app/documents/documents.go:16-24, 37-59, 98-133`）。领域错误 `version_required` / `version_mismatch` / `version_column_unavailable`（`errors.go:19-33`）。写路径 `requireVersionColumn` **不**在热路径 ALTER（`postgres.go:1663-1676`）。

**判断。** 用例核的缝落对了：策略（guest、owner ACE、privileged grant、DDL）留在 Client/Server 包装层，不变式（OCC、grant、query 折叠）留在核。继续加深应把 Query 双栈收进核的单一 AST 入口（发现 4），而不是再复制一套 Databases 用例。

---

## 总判

数据面今天是 **「带 ACL 的单行 Postgres 表工厂」**，不是 **「SaaS 应用的数据库原语」**。

够深的局部：系统表与业务库物理隔离、单行写的事务+outbox+OCC、List 的 `_perms` SQL 下推、AST→SQL 编译。不够深的模块：DocumentDB 仍是一个适配器吞下 catalog/DDL/文档/ACL/查询；`_perms` 没有自己的接口；查询有两套语言。作为给 SaaS 团队用的 Database，缺 relation、缺跨文档事务、缺 schema 演化、默认 ACL 还是公开读。Client/Server 文档核已经抽出，剩下的重复主要在传输双栈，不是再抽一层 usecase 能修的。
