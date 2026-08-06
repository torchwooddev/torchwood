# P0 设计方案回顾与偏差确认

> 基于对 `D:\Codes\baas\appwrite` 和 `D:\\Codes\\qiulin\\Torchwoodwork` 的二次 review，列出当前 P0 设计与两者的对齐点、偏差点、以及需要进一步确认/补充的事项。

---

## 1. 确认对齐的设计点

| 领域 | 我们的方案 | Appwrite / fleetwork 做法 | 结论 |
|------|-----------|--------------------------|------|
| 核心框架 | Lynx | fleetwork 已深度使用 | 沿用 |
| DI | Wire + 分层 ProviderSet | fleetwork 标准 | 沿用 |
| 配置 | protobuf config + `TORCHWOOD_` 环境变量 | fleetwork 标准 | 沿用 |
| 静态表 ORM | bun + `db/migrations/*.sql` (golang-migrate) | fleetwork 标准 | 沿用 |
| 列表查询 | `pkg/crud` + field mappings + `ExecuteListQuery` | fleetwork 标准 | 沿用 |
| gRPC 服务注册 + 拦截器 | OTel → metrics → auth → protovalidate | fleetwork 标准 | 沿用 |
| grpc-gateway + 自定义错误/编解码 | 复用 fleetwork 模式 | fleetwork 标准 | 沿用 |
| 认证注解 | `proto/shared/authz.proto` 驱动 | fleetwork 标准 | 沿用 |
| Principal / AuthContext | `shared.Principal` + contexts | fleetwork 标准 | 沿用 |
| 动态文档层 | Utopia-style Postgres adapter（shared tables + `_tenant` + `_perms`） | Appwrite 实际做法 | 对齐 |
| 多租户 | 按 project sequence 做 tenant id | Appwrite 实际做法 | 对齐 |
| Redis 队列/缓存/广播 | Redis Streams/Lists/PubSub | Appwrite 用 Redis Queue；fleetwork 用 Kafka，但结构可替换 | 可行 |
| S3 兼容存储 | 元数据 PG + 二进制 S3/MinIO | Appwrite 实际做法 | 对齐 |
| Functions 执行 | Docker SDK / Open Runtimes 方向 | Appwrite Executor | 对齐 |
| 目录分层 | `api / app / domain / infra` | fleetwork Clean Arch | 对齐 |

---

## 2. 关键偏差与需要补充的事项

### 2.1 Session / 认证模型

| 偏差点 | Appwrite | 我们当前设计 | 影响 |
|--------|---------|------------|------|
| Session 存储位置 | session 是 `users` document 内嵌 `sessions` 数组中的文档，cookie secret 与该数组比对 | 独立 `sessions` SQL 表，`secret_hash` 字段 | 实现更简单，但必须补齐 session 列表、设备信息、factors、过期检查等字段 |
| Cookie 内容 | `Store` token 存 user id + raw secret | 计划用 JWT 或 session token | 需明确 cookie 里到底存什么；建议 cookie 存 signed session id，服务端查表 |
| Console 登录 | 复用 `/v1/account/sessions/email` + `x-appwrite-project: console` | 专用 `/v1/console/auth/sign-in` | 无强兼容要求，可接受；但需让 console 前端知道走独立路径 |
| API key 类型 | 标准 key、ephemeral JWT、organization key、account key、dev key、dynamic key | 只有标准 project API key | P0 够用；function/preview/VCS 所需的 ephemeral/dev key 需要 P1 补充 |
| Token 种类 | access JWT、refresh JWT、function JWT、resource token、preview cookie | 未分类 | 需定义 token taxonomy；P0 至少区分 access/refresh/API key |

**建议补充**

1. 明确 P0 cookie 格式：`TORCHWOOD_session_{project_id}` 存 signed session id；服务端用 `sessions` 表验证。
2. 定义 `TokenKind` 枚举或 JWT `aud` claim，避免不同用途 token 混用。
3. 在 `sessions` 表中保留 `provider`, `user_agent`, `ip`, `country`, `factors`, `expire_at`, `created_at` 等字段。

### 2.2 系统表权限模型

| 偏差点 | Appwrite | 我们当前设计 | 影响 |
|--------|---------|------------|------|
| users/teams/sessions/keys/buckets/files 权限 | **全部是 document**，有 `$permissions` 和 `_perms` 表 | 计划用静态 bun 表 + RBAC/scopes | 最大架构偏差；若 P1 要支持 per-user ACL 或 Appwrite SDK 兼容，需重写 |
| API key 权限 | 标准 key 调用 `authorization->setDefaultStatus(false)` 绕过 document auth | 检查 `api_keys.scopes` | 对动态文档需决定 API key 是 bypass `_perms` 还是作为 `keys` role |
| 管理员模式 | `mode=admin` 关闭 document auth | console admin role 调用 server API | 需确保 admin 请求不走正常 document filter |

**建议补充 / 决策**

- **P0 接受偏差**：系统表用静态 RBAC，动态文档用 `_perms`。
- 在 `users` 表中显式加入 `owned_by` / `created_by` 等字段，作为静态表的粗粒度权限依据。
- 对动态文档，API key 默认作为 `keys` role，不 bypass `_perms`（更安全）；如需 bypass，单独加 `api_keys.bypass_document_auth` 标志。
- admin 请求在 auth middleware 中标记 `IsAdmin`，数据库层跳过 `_perms` join。

### 2.3 动态数据库层细节

| 偏差点 / 缺失 | Appwrite | 我们当前设计 | 影响 |
|--------------|---------|------------|------|
| 元数据表 | 每个 namespace 下有 `databases`、`collections`、`attributes`、`indexes` 文档集合 | 只定义了 `DocumentDatabase` 端口，未给出元数据表 schema | P0 必须创建这些元数据表，否则无法管理 collection/attribute/index |
| namespace 策略 | 默认 namespace-per-project（`_{sequence}_{collection}`），可选 shared tables | 计划 schema-per-database + shared tables with `_tenant` | 需固定一种策略，否则 DDL 生成复杂 |
| tenant 值 | project `$sequence`（数字） | 未明确 | 必须用 sequence，不能是 public project_id |
| attribute/index 子集 | 支持 15+ 类型和 8 种索引 | 未明确 P0 范围 | 需要裁剪，否则工作量大 |
| relationships | 一/多对多等 | P0 未提及 | 建议 P1 实现 |

**建议补充**

1. 元数据表 schema：
   - `document_databases(id, project_id, name, ...)`
   - `document_collections(id, database_id, project_id, name, ...)`
   - `document_attributes(id, collection_id, key, type, size, required, default, ...)`
   - `document_indexes(id, collection_id, type, attributes, orders, ...)`
2. 固定 namespace 策略：**schema-per-database**（一个 database 一个 PG schema），表名 `{collection}`，带 `_tenant` 列做项目隔离。
3. P0 attribute 子集：`string`, `integer`, `float`, `boolean`, `datetime`, `email`, `url`, `json`。
4. P0 index 子集：`key`, `unique`, `fulltext`。

### 2.4 Worker / 事件 / 队列

| 偏差点 / 缺失 | Appwrite | 我们当前设计 | 影响 |
|--------------|---------|------------|------|
| 队列数量 | 14+ 队列（database/deletes/audits/mails/functions/webhooks/...） | 只声明 `EventPublisher` / `Queue` 端口 | P0 至少需要具体实现 audit/deletes/outbox |
| 消息结构 | 每个队列有固定 message 结构 | 未定义 | P1 无法消费 |
| 事件触发 | 路由 label `event`/`audits.event`，shared API init 入队 | 未设计 | 业务操作无法触发 webhook/audit |
| Bus 监听 | Log/Mails/Usage listeners | 未设计 | 无内部事件总线 |

**建议补充**

- P0 至少定义以下队列/消息（其余 P1）：
  - `audit`：审计日志
  - `deletes`：级联删除
  - `outbox`：可靠事件投递（fleetwork 模式）
- 在 app 层统一调用 `EventPublisher.Publish(ctx, "audit", msg)` 而不是每个 handler 直接写队列。
- 用 Redis Streams（或 Watermill Redis adapter）做 transport，保留 CloudEvents 信封。

### 2.5 Storage

| 偏差点 / 缺失 | Appwrite | 我们当前设计 | 影响 |
|--------------|---------|------------|------|
| 文件元数据 | `bucket_{sequence}` collection 中的 document，有 `$permissions` | bun `files` 表 | 丢失 per-file ACL |
| 文件特性 | 加密、分片、signature、chunksTotal/Uploaded | 未设计 | P0 只能做简单上传下载 |
| 预览 | `/preview` 图像变换 + cache collection | 未设计 | P1 |
| 资源 token | JWT resource token 允许公开访问 | 未设计 | P1 |

**建议补充**

- P0 Storage 范围限定为：认证上传、S3 预签名下载、元数据 CRUD。
- 在 `files` 表加 `bucket_id`, `path`, `size`, `mime_type`, `metadata`, `encryption`（预留空）, `created_by`。
- 如需 per-file ACL，P1 考虑把文件元数据迁移到动态文档 store。

### 2.6 Functions

| 偏差点 / 缺失 | Appwrite | 我们当前设计 | 影响 |
|--------------|---------|------------|------|
| 构建/执行流程 | Functions worker → Builds worker → Executor → Executions worker | 只有 `FunctionExecutor` 端口 stub | P0 无 functions 业务能力 |
| 触发源 | DB/audit events、schedules、manual | 未设计 | P1 |
| API key 类型 | ephemeral JWT key 注入 function 环境 | 未设计 | P1 |

**建议补充**

- P0 定义 domain structs：`Deployment`, `BuildResult`, `ExecutionResult`。
- `FunctionExecutor` 接口至少包含：
  - `Build(ctx, deployment) (BuildResult, error)`
  - `Execute(ctx, deployment, env, payload, timeout) (ExecutionResult, error)`
- 实现 stub 返回 not implemented，但接口稳定。

### 2.7 Realtime

| 偏差点 / 缺失 | Appwrite | 我们当前设计 | 影响 |
|--------------|---------|------------|------|
| 独立 WS server + Redis pub/sub fanout | 有 | 计划 P1 | P0 不实现，但端口应预留 |
| 频道规则 | `account`, `account.{userId}`, `teams`, `documents`, `files`, ... | 未设计 | P1 需详细 spec |

**建议补充**

- P0 不实现 realtime，但在 `internal/domain/shared` 预留 `RealtimePublisher` 端口。
- 数据库事件写入 outbox 时，预留 realtime topic。

### 2.8 Health / Audit / Usage / Migrations

| 偏差点 / 缺失 | Appwrite | 我们当前设计 | 影响 |
|--------------|---------|------------|------|
| Health 端点 | DB、cache、storage、queue、pubsub 等 | 只有基础 health | P0 应至少暴露 DB/Redis health |
| Audit | Audit worker 写 project DB | P1 | P0 可在 app 层写 audit outbox |
| Usage/Stats | StatsUsage / StatsResources workers | P1 | P0 无 |
| Migrations | 版本化 PHP migration 类，含数据迁移 | 只用 golang-migrate SQL | 数据迁移（如 tenant 重映射）需要额外机制 |

**建议补充**

- P0 Health 增加 `/health/db` 和 `/health/redis`。
- 设计 data-migration 机制：版本化的 Go job，可重复执行、幂等，与 golang-migrate 分开。

### 2.9 fleetwork 模式必须遵循的补充

| 事项 | 说明 |
|------|------|
| 每个 proto RPC 必须有 authz 注解 | `grpc.go::collectMethodsByAccess` 会扫描所有 proto，缺失注解导致启动失败 |
| `Principal` 必须从 context 取 | 用 `internal/pkg/contexts` 或类似包，不要传 raw claims |
| 列表接口必须走 `pkg/crud` | 不要手拼 SQL filter |
| 所有 migrations 用 golang-migrate | `task migrate` |
| Wire 变更后必须 `task wire-all` | 包括 server / cli / tests |
| 用 CloudEvents 信封 | `pkg/pubsub` 接口已定义 |
| Outbox + DLQ | 可靠事件投递模式 |

---

## 3. 需要你确认的关键决策

1. **系统表权限模型**
   - A. 维持静态 RBAC（users/sessions/files 等用 bun 表，不走 `_perms`）。
   - B. 从一开始就把 users/teams/files 等放入动态文档 store，保留 `$permissions`。
   - 推荐 A（P0 复杂度可控），但请确认。

2. **动态文档 namespace 策略**
   - A. schema-per-database（一个 database 一个 PG schema）。
   - B. namespace-per-project（所有表在一个 schema，表名前缀 `_<sequence>_`）。
   - 推荐 A，与数据库概念更一致。

3. **P0 动态文档能力子集**
   - attribute 类型：string/int/float/bool/datetime/email/url/json。
   - index 类型：key/unique/fulltext。
   - 是否 OK？

4. **Session cookie 内容**
   - A. cookie 存 signed session id，服务端查 `sessions` 表。
   - B. cookie 直接存 JWT。
   - 推荐 A，便于吊销和列表管理。

5. **API key 是否 bypass 动态文档 `_perms`**
   - A. 不 bypass，API key 作为 `keys` role 参与 `_perms`。
   - B. bypass，API key 可访问 project 内所有动态文档。
   - 推荐 A（更安全），但 function 执行等场景 P1 可能需要 B 的变体。

6. **P0 队列范围**
   - 至少 audit、deletes、outbox 三个队列是否足够？

7. **文件元数据是否静态表**
   - A. P0 用 bun `files` 表，per-file ACL  deferred。
   - B. 文件元数据直接进动态文档 store。
   - 推荐 A。

8. **Console admin 模型**
   - P0 先单一全局 owner + `project_members` 表是否可接受？P1 再引入 organization/team。

---

## 4. 下一步建议

待上述 8 个问题确认后，建议按以下顺序补充设计并进入编码：

1. 更新 `p0-foundation-design.md`：
   - 明确 session/token taxonomy
   - 补充元数据表 schema
   - 定义 P0 队列/消息结构
   - 定义 `Deployment`/`BuildResult`/`ExecutionResult` structs
   - 更新 health/migration 策略
2. 生成初始 config proto、bun migrations、Wire skeleton。
3. 开始 P0.1 编码。