# Round-3 全量审核报告：08 - CRUD 抽象与领域端口

> 审查范围：`pkg/crud/`、`internal/domain/`（auth / users / projects / databases / teams / storage / functions / audit / idgen / messaging / shared）、交叉 `pkg/query/`、`pkg/idgen/`，以及端口实现与列表查询复用（`internal/infra/bun/bunrepo/`、`internal/infra/documentdb/`、`internal/app/server/`）。
> 审查基准：当前工作区代码（相对 Round-2 `c640d9b` 之后的修复批次）。
> 执行方式：只读审查；未修改任何源代码。

---

## 1. 摘要

本模块安全面总体可控：`pkg/crud` 的字段名经正则白名单、映射值有二次校验、比较值走绑定参数；动态文档查询走独立的 `pkg/query` + documentdb adapter，与 AIP-160 抽象分层清晰。Round-2 的 P1/P2 修复项（function ID 小写、LIKE 转义、`UpdateFunction`/`UpdateDeployment` 的 `project_id` 过滤、ListDocuments 默认排序 `_id` tiebreaker）均已落地。

主要剩余问题不在注入/越权，而在**抽象契约与复用**：`pkg/crud` 几乎只被 `ListProjects` 调用，且该路径解析了 `filter`/`order_by` 却不应用；其余 `ListRequest` 入口或整表倾倒，或走 Appwrite DSL 却在传输层丢掉 `next_page_token`。领域层整体干净（无 bun 类型泄漏），但 `teams`/`users` 校验函数直接返回 gRPC `status.Error`，破坏端口层与传输层的边界。

**Verdict：有条件通过。** 无 P0/P1；P2 以复用缺口与领域泄漏为主，可合入，但不宜关闭本模块审查。

---

## 2. 已核实健康

### 2.1 Round-2 修复回归

| 项 | 结论 | 证据 |
|----|------|------|
| **F3-2** ListDocuments `page_size` 失效 | ✅ 仍成立 | `pkg/query/query.go:181-208` `ParseMany` 不再注入默认 limit；`internal/infra/documentdb/postgres.go:891-902` 用 `q.PageSize` 回退并 clamp 到 `maxQueryLimit=100`。 |
| **F5-1** Function ID 路径穿越 | ✅ 仍成立 | `internal/app/functions/management.go:16-19,63-65` 字符集 + 长度校验。 |
| **F5-2** Get/DeleteDeployment 跨项目 IDOR | ✅ 仍成立 | `internal/domain/functions/repo.go:17-20` 端口带 `projectID`；`function_repo.go:83-88,127-132` SQL 带 `project_id` + `function_id`。 |
| **R08-P1-1** 大写 function ID 导致镜像名非法 | ✅ 已修复 | `management.go:19` 收紧为 `^[a-z0-9][a-z0-9_-]{0,63}$`，注释明确 Docker 小写约束。 |
| **R08-P2-2** crud `contains`/`notcontains` LIKE 未转义 | ✅ 已修复 | `pkg/crud/filter.go:258-265,315-321` `escapeLikePattern` + `ESCAPE '\'`；`filter_test.go:1000-1082` 覆盖 `%`/`_`/`\`。 |
| **R08-P2-3** `UpdateFunction`/`UpdateDeployment` 缺 `project_id` | ✅ 已修复 | `function_repo.go:60-66,116-124` `WherePK()` 外追加 `project_id`（部署再加 `function_id`）。 |
| **R08-P2-4** ListDocuments 默认排序无 `_id` | ✅ 已修复 | `postgres.go:1928-1929` `ORDER BY d._created_at DESC, d._id DESC`。 |
| **R08-P3-5** mock `DeleteDeployment` 未校验 `projectID` | ✅ 已修复 | `internal/app/functions/mocks_test.go:110-119` 跨项目/跨函数直接 no-op。 |
| **R08-P3-6** `idgen.IsValid` 只判非空 | ✅ 已文档化（接受） | `pkg/idgen/id.go:20-24` 明确字符集校验归各 use-case。 |
| **R08-P3-7** `page_size > maxQueryLimit` clamp 测试 | ❌ 仍缺 | `postgres_test.go:681-773` 覆盖 5/0/-1/DSL 优先/非法 token，**没有** `PageSize: 200` 断言 clamp 到 100。 |

### 2.2 本轮核对为健康的项

- **领域不依赖 bun / genproto**：`internal/domain` 无 bun 引用；实体均为纯 Go 结构。唯一传输泄漏见 P2-4。
- **端口均有 infra 实现**（除死端口 `ProjectResolver`，见 P3）：`DocumentDB`→postgres、`FunctionRepo`→bunrepo、`Executor`→docker、项目/API Key/Admin/OAuth/Audit→bunrepo、auth 各 store→Redis、`Generator`→`infra/idgen.Service`、Mailer/SMS、`ObjectStore`/`UploadSessionStore`、`Queue`→Redis。Wire 绑定集中在 `internal/infra/provides.go`。
- **Filter SQL 注入面封闭**：字段名 `^[a-zA-Z_][a-zA-Z0-9_.]*$`；映射值二次校验，非法映射退化为 `FALSE`（`filter.go:253-288`）；值一律 `?` 绑定。`IsTrustedSQLMappingFragment` 仅放行服务端 JSON path。
- **动态文档与 AIP 抽象边界正确**：文档列表用 `pkg/query`（Appwrite DSL），**不**经 `pkg/crud.BuildSQLWhere`。`query.ParseMany` 与 documentdb 的 page_size 回退职责分离清晰。
- **Principal ↔ JWT claims 映射一致**：`jwtparser.Claims` 的 `uid/akd/pid/sid/rls/scp/ttp` 与 `shared.Principal` 对齐；端用户角色实时 `LoadUserRoles`（不信任 JWT 旧 `rls`）；admin 角色来自 DB 而非 token；API Key 走独立路径，`Roles=["keys"]`、`Permissions=scopes`。`ActorKind` 三值封闭（`end_user/admin/service`）。
- **权限不变量在领域层**：`databases/permissions.go` 完整表达 documentSecurity、系统集合 OR 豁免、`any` 写拒绝、模板展开与授予校验；有对应单测。用户状态、OAuth 回跳白名单、ID 策略解析同理。
- **cursor 生产路径只编码 offset**：documentdb / ListProjects 使用 `EncodePageToken`/`DecodePageToken`，token 内无用户数据；HMAC 签名 API 已具备但未接入（见 P3）。
- **`HasAnyPermission` fail-open 有守门说明**：`principal.go:69-72` 写明依赖 `collectMethodsByAccess` 强制非空 permissions。

---

## 3. 问题清单

### 🔴 P0 严重

无。未发现可利用的 SQL 注入面或 cursor/端口导致的越权数据泄露。

### 🟠 P1 高

无。Round-2 P1（大写 function ID）已修复；本轮新发现均属契约空洞或分层问题，不构成现行路径上的功能断裂或越权。

### 🟡 P2 中

1. **`pkg/crud` 几乎未被列表路径复用，且唯一调用方忽略 filter/order_by**
   - 位置：
     - `internal/app/server/projects.go:101-133` — `ParseListParams` 收下 `filter`/`orderBy` 后只做内存切片分页，**从不** `ParseFilter`/`ParseOrderBy`。
     - `internal/api/servergrpc/functions.go:87-103`、`apikeys.go:59-76`、`databases.go:63-77`、`oauth_providers.go:25` — 接受 `ListRequest` 但整表倾倒，page_size/page_token/filter 全丢。
     - `pkg/crud/repository.go:4-13` — 泛型 `Repository` **仓库内零引用**，没有 FieldMappings 统一 List 实现。
   - 影响：违反 AGENTS.md「列表查询复用 `pkg/crud`，不要手拼 SQL filter/order」。客户端按 AIP-160/132 传 `filter`/`order_by` 会**静默失效**（既不报错也不过滤）。元数据列表（functions/keys/databases）无法分页，数据量上来后会打满响应。
   - 修复建议：`ListProjects` 用 `FilterValidator`/`OrderByValidator` 应用白名单字段（`name`/`status`/`created_at`），过滤后分页；其余 `ListRequest` 入口至少走 `ParseListParams` + repo 层 `LIMIT/OFFSET`（或把 `FunctionRepo.List*` 扩成 `ListParams`）。无计划支持的字段应在 handler 对非空 `filter`/`order_by` 返回 `InvalidArgument`，避免静默丢弃。

2. **`ParseFilter` 按字面拆 AND/OR，不感知引号与括号**
   - 位置：`pkg/crud/filter.go:149-155,102-147`
   - 代码：`regexp.MustCompile(`(?i)\s+` + op + `\s+`).Split`；注释写明「Simple implementation」。
   - 影响：`name = "foo AND bar"` 会被拆成两条表达式，AIP-160 语义错误。当前生产几乎不用 crud filter，故不是线上事故，但是共享抽象库的正确性缺陷，一旦 `ListProjects` 或后续元数据列表接上就会踩中。
   - 修复建议：改为引号感知的 tokenizer（跳过 `"..."`/`'...'` 内的 AND/OR）；混合 AND/OR 继续拒绝；补单测。

3. **`OperatorHas` 生成 MySQL `JSON_CONTAINS`，与 PostgreSQL 运行时不兼容**
   - 位置：`pkg/crud/filter.go:342-345`
   - 影响：本仓库唯一数据库是 Postgres。`has` 一旦被 `BuildSQLWhere` 使用会直接 SQL 报错。当前无生产调用，属于抽象正确性债。
   - 修复建议：改为 Postgres `jsonb_exists` / `?` 算子，或在接入前从公开操作符集中移除 `has` 并让 `ParseFilter` 拒绝。

4. **领域层泄漏 gRPC 类型**
   - 位置：
     - `internal/domain/teams/membership.go:6-7,20-37` — `ValidateStatus`/`ValidateRole` 返回 `status.Error(codes.InvalidArgument, …)`
     - `internal/domain/users/password.go:6-7,16-35` — `ValidatePasswordStrength` 同样返回 gRPC status
   - 影响：领域包依赖 `google.golang.org/grpc`，无法在非 gRPC 上下文（CLI、worker、单测）独立复用；错误分类被钉死在传输层。同模块的 `users.ValidateStatus` 已用普通 `fmt.Errorf`，风格也不一致。
   - 修复建议：领域返回普通 `error`（可导出哨兵或校验错误类型），由 app/api 映射 `codes.InvalidArgument`。

5. **部分列表排序仍缺稳定键**
   - 位置：
     - `internal/infra/documentdb/postgres.go:239-242` — `ListCollections` `Order("created_at DESC")`，无 `id`。
     - `postgres.go:1943-1944` — 用户指定 order 时只追加 `, d._created_at DESC`，**仍无 `_id`**。
     - `internal/infra/bun/bunrepo/project_repo.go:54-56`、`function_repo.go:47-48`、`audit_repo.go:65-67` — 静态表列表同样单字段排序。
   - 影响：同时间戳多行时 offset 分页可能重复/遗漏。cursor 模式（`postgres.go:958-961`）已用 `(sortField, _id)` 谓词，offset/默认路径与之不等价。
   - 修复建议：所有默认/用户 order 末尾追加主键 tiebreaker（文档 `_id`，集合/项目/函数 `id`）。

6. **文档类 List 在传输层丢弃 `NextPageToken`（AIP-158 合同缺口）**
   - 位置：
     - `internal/api/servergrpc/users.go:61-76` — `ListUsers` 第三返回值 `_`，`Meta` 无 `NextPageToken`
     - `internal/api/servergrpc/teams.go:52-67` — `ListTeams` 同
     - `internal/api/servergrpc/storage.go:53-73,161-172` — `ListBuckets`/`ListFiles` 同
   - 影响：documentdb 已按 page_size 分页并生成 token（`postgres.go:1005-1012`），但客户端只能看到第一页。`queries` 的 `offset`/`limit` 仍可用，故不是完全不可分页，但 `page_token` 字段形同虚设。与 crud 的 AIP-158 抽象脱节。
   - 修复建议：把 use-case 返回的 token 写入 `ListResponseMeta.next_page_token`；`page_size<=0` 时用 crud 默认 50 而不是把 0 回传给客户端。

### 🟢 P3 低

7. **`projects.ProjectResolver` 是死端口**
   - 位置：`internal/domain/projects/repository.go:37-39`
   - 影响：全仓库无实现、无 Wire bind；documentdb 自己实现 `resolveInternalID`。增加「端口未实现」的误导。
   - 修复建议：删除接口，或让 documentdb 依赖该端口并提供 bun 实现。

8. **`FilterValidator.caseInsensitive` / `ValidatePageTokenIntegrity` / `TokenChecksum` 为死代码或占位**
   - 位置：`pkg/crud/filter.go:389-393`（flag 从不参与 `Validate`）；`list.go:104-124`（完整性检查几乎是空实现）；`pagination.go:429-454`（加权字符和，8 位字母，非 HMAC）。
   - 影响：调用方误以为已有大小写不敏感匹配或 checksum 防篡改。生产分页走的是无签名 `EncodePageToken`。
   - 修复建议：实现或删除；checksum 若保留应改为 SHA-256/HMAC，并与 `EncodeSignedCursorPageToken` 合并。

9. **`DecodePageToken` 仍接受无过期的 legacy `v1:<offset>`**
   - 位置：`pkg/crud/pagination.go:201-209`
   - 影响：`v1:999999` 可永久伪造任意 offset。分页跳页本身通常不可越权，但与 AIP-158「opaque token」及 24h TTL 不一致；签名 cursor API（`EncodeSignedCursorPageToken`）无生产调用。
   - 修复建议：去掉 legacy 回退，或仅在显式兼容开关下启用；documentdb 列表改为 signed token。

10. **Client Databases 把 `shared.Principal` 转成 `databases.Principal` 时丢掉 `PlatformAdmin`**
    - 位置：`internal/app/client/databases.go:50,62`（只拷 `Roles`）；对比 `internal/app/client/teams.go:29` 与 `internal/api/servergrpc/storage.go:282-287` 均拷贝 `PlatformAdmin`。
    - 影响：若 console admin 走 Client Databases API，不会触发文档层平台管理员旁路（偏 fail-closed，不构成越权）。转换口径不一致。
    - 修复建议：抽公共 `ToDocumentPrincipal`，统一拷贝 `Roles` + `PlatformAdmin`。

11. **领域错误与 ID 类型不统一；部分不变量仍在 app**
    - 位置：`users/status.go` 用 `fmt.Errorf`，`users/password.go`/`teams/membership.go` 用 gRPC status，`databases/errors.go` 用哨兵；实体 ID 多为 `string`，仅 `shared.Principal.ActorID` 为 `idgen.ID`；function/project ID 字符集分别在 `management.go:19` 与 `projects.go:23`。
    - 影响：跨模块 `errors.Is` 不可靠；ID 校验规则分散，新写入口易漏。
    - 修复建议：领域统一哨兵/校验错误；标识符规则下沉到 domain 或 `pkg/idgen` 具名校验函数。

12. **`page_size > maxQueryLimit` 仍无回归测试（R08-P3-7 遗留）**
    - 位置：`internal/infra/documentdb/postgres.go:900-901` vs `postgres_test.go:681-773`
    - 修复建议：插入 ≥101 行，`PageSize: 200`，断言返回 100。

13. **`HasAnyPermission` 空列表 fail-open；JWT `ActorKind=service` 会落到 end_user 分支**
    - 位置：`internal/domain/shared/principal.go:73-76`；`internal/infra/auth/validator.go:134-196`（`case "admin"` 以外一律当端用户）。
    - 影响：前者已有启动期守门注释，直接调用需自行判空；后者仅当有人签发 `akd=service` 的 JWT 才会误分类（API Key 不走 JWT）。
    - 修复建议：`HasAnyPermission` 对空列表改为 fail-closed，或改名标明「仅拦截器使用」；`principalFromJWT` 对未知 `ActorKind` 拒绝。

---

## 4. 模块结论

| 维度 | 评价 |
|------|------|
| 抽象安全性 | 良好。字段白名单 + 参数绑定 + LIKE 转义已齐；cursor 不含业务数据。 |
| 领域纯度 | 中上。无 bun/文档 DB 类型泄漏；权限/状态/OAuth 回跳等不变量大部分在领域。gRPC 错误是主要杂质。 |
| 端口完整性 | 高。除 `ProjectResolver` 死接口外均有适配器；functions 写路径已带 `project_id` 纵深。 |
| 复用度 | **偏低。** `pkg/crud` 是完整的 AIP-132/158/160 工具箱，但生产几乎只用了 page token 编解码；filter/order/泛型 repo 处于「写好了没接上」状态。文档列表正确走 `pkg/query`，与 crud 双轨是有意设计，但元数据列表既没用 crud 也没显式拒绝 AIP 字段。 |
| Principal / JWT | 一致。角色实时解析、admin 权限来自 DB、API Key scopes 独立，符合 AGENTS.md。 |

**最需优先处理的 3 项：**

1. 堵住 `ListRequest` 的静默丢弃：`ListProjects` 真正应用 filter/order，或对不支持字段返回错误；functions/keys/databases 至少接分页。
2. 领域层去掉 gRPC 依赖（`teams.Validate*`、`users.ValidatePasswordStrength`）。
3. 补齐排序稳定键（ListCollections / 用户 order / bunrepo 列表）以及 `page_size` 上限 clamp 测试。

**是否建议关闭本模块审查：不建议。** Round-2 的安全修复已收口，无新的 P0/P1，阶段性合入没有阻碍；但 crud 复用与领域纯度两项 P2 仍是本模块的核心债，应在下一轮或独立清理批次关闭。
