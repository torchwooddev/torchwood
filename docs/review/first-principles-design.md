# Torchwood 第一性原理设计评审

> 日期：2026-08-20（2026-08-21 评审修订；同日 owner 逐项表态）  
> 基线：`main` @ `202211d`（落笔时工作区干净；提交 SHA 以 git 为准）  
> 状态：**判断已表态的规划底稿**（事实经两轮独立抽查；2026-08-21 评审接受总体判断与 K 清单，修正 13 处事实细节——见附录 B。同日 owner 完成逐项 accept/修正——见附录 C。同日终审——见附录 D。施工计划：`docs/review/first-principles-plan.md`。系统表化是方向而非已排期施工）  
> 方法：不考虑既有预设前提与已拍板决策，只从最优设计评估当前代码。已标明的另案（系统表化方向、Agent overlay）在判断里分开写，避免当成未规划的永久错误，也避免当成已有执行规格。  
> 读者：按已接受的判断重排与施工。只认带前缀的稳定 ID，不按段落语序。

本文不是 Round 1–3 那种分模块代码审计，也不替代 `docs/design/` 下已批准的实施方案。它回答的是：

> 如果目标是一个多租户、Agent 可调用、带支付/资产的 BaaS，模块该怎么切、接口该多深、缝该落在哪？当前代码离这个目标差在哪里？

词汇（与 codebase-design 一致）：

| 词 | 含义 |
|---|---|
| **模块** | 有接口与实现的任意尺度单元（函数、包、跨层切片） |
| **接口** | 调用方必须知道的一切：类型、不变式、顺序、错误、配置、性能 |
| **深度** | 接口上的杠杆：小表面后面大量行为 |
| **缝** | 接口所在的位置，可在此处替换行为而不改调用方 |
| **适配器** | 在缝上满足接口的具体物 |
| **假缝** | 既无第二种生产故事、测试也不走该接口的端口（纯间接）。测试 fake / 内存实现算正当缝。 |
| **真缝** | 生产变异（第二厂商/协议）**或**非 PG 单测适配器 |
| **局部性** | 变更、缺陷、知识与验证集中在一处，而不是散到所有调用方 |

ID 前缀（不与 Round 1–3 的 F/G/H 批次碰撞）。**`P-` 是产品定位，不是审计优先级 P0/P1。** 文内「v3 D1」是经济设计决策编号，不是发现 **D-1**。

| 前缀 | 用法 | 验证时做什么 |
|---|---|---|
| P/M/S/D/A/R/C | 发现 | 先核**事实**（属实/过时/部分属实），再对**判断**表态（接受/修正/驳回+原因） |
| K | 保留项 | 核「是否仍成立」，再表态是否保留。驳回即允许在还债时改动 |
| T / E | 规划提案 | 方向已接受（附录 C）；可调波次与切口，**不得**推出附录 C 禁读 |
| V | 代表路径（抽样） | 用来判断「改完是否更深」，不单独 accept/reject |

每条发现分两类陈述：

- **事实**：可对照源码核对（路径、方法数、适配器个数）。
- **判断**：在事实成立的前提下，从最优设计给出的结论。验证阶段先核事实，再决定是否接受判断。

§0 总判不是可验收发现，只作阅读入口。独立核对记录见 [附录 A](#附录-a独立核对记录)；owner 逐项表态见 [附录 C](#附录-cowner-逐项表态)。

---

## 0. 总判

工程完成度高、安全补丁密、经济子系统已经摸到正确形状。但产品脊柱（身份、用户、文档、API 表面）是 **Appwrite 资源克隆 + 文件夹式分层**，不是深模块。

Agent-native 目前是声明（Protobuf + OpenAPI + scoped API Key），不是工具表面设计。最大的设计税不是「不该同时做 BaaS + 经济 + Agent」，而是：**User/Session 走了文档引擎，Client/Server 对同一资源克隆了两套 API，身份是字段袋。** sentinel、`businessSchema`、启动期 scope 对齐是数据面迁移与 fail-closed 的结构保险（K-9、K-13、K-15），还债时不要在系统表化完成前拆掉。

一句话：Torchwood 是一个**实现充分、身份与系统资源抽象过载**的系统。最优设计不是更多分层或把 201 个 RPC 砍掉，而是承认——**用户不是文档、Documents 不需要两套产品 API、Agent 需要叠一层工具而不是替换 CRUD 表面**——然后把已经在经济子系统里验证过的深模块手法，应用到身份和系统资源上。

---

## 1. 分类总表

后续规划只认本表及 §1.9 / §1.10 的 ID。交叉从属见各条正文，规划时合并以免双工：

- 系统表化史诗：M-2 + D-1 + D-3 + E-5（M-2 是领域语言，D-1 是存储形态）。**E-5 未出独立设计前不排进施工**
- Agent overlay：P-2 + D-4 + R-2 + R-5 + T-4 + E-7（不删产品 RPC）
- 拆端口不删引擎：M-3 + E-6
- 独立缺陷（Wave 0，不绑史诗）：A-5 admin refresh、A-4 `secret_hash`、D-5 `Array`、D-5 `_version` 迁入 `projectschema`、R-6 建单入口
- 产品默认（非 E-5）：D-9 用户 collection `read:any`

### 1.1 产品定位

| ID | 标题 | 冲击 | 章节 |
|---|---|---|---|
| P-1 | 三身份对接口要求不同；冲突发生在 User/Session 仍走文档引擎 | 高 | §2 |
| P-2 | Agent-native 是挂钩（OpenAPI/Key），还不是工具表面 | 高 | §2 / §7 |

### 1.2 领域模型

| ID | 标题 | 冲击 | 章节 |
|---|---|---|---|
| M-1 | `internal/domain` 多数是端口目录；有不变式的是订单/订阅/ACL/资产 class | 中 | §3 |
| M-2 | 系统实体是无类型文档（无 User/Session/Group 聚合） | 高 | §3 |
| M-3 | 用户文档引擎是产品；`DocumentDB` 端口是神对象 | 高 | §3 / §5 |
| M-4 | 资产五动词应收进 Assets 领域服务（不放 Holding） | 中 | §3 |
| M-5 | Database 无独立类型（`GetDatabase` 返回 `*Collection`） | 中 | §3 / §5 |
| M-6 | 事件信封超载（文档事件 + 经济事件） | 中 | §3 |

### 1.3 模块深度与缝

| ID | 标题 | 冲击 | 章节 |
|---|---|---|---|
| S-1 | 死端口与过宽假缝定义；Redis 动物园部分可合并 | 中 | §4 |
| S-2 | SQL 锁语义抬进领域接口（经济正确性仍依赖这些临界区） | 中 | §4 |
| S-3 | app 生产代码 import infra；domain/pkg 返回 gRPC status | 中 | §4 |
| S-4 | 事务缝是 ctx 魔术，不是模块接口 | 中 | §4 |
| S-5 | `domain.ProviderSet` 为空（M-1 的诊断信号，非独立债） | 低 | §4 |

### 1.4 数据面

| ID | 标题 | 冲击 | 章节 |
|---|---|---|---|
| D-1 | 系统资源仍是文档（形态错；系统表化是方向，尚无独立设计） | 高 | §5 |
| D-2 | 三层 schema 已买到隔离；过渡税是 sentinel 寻址与双 catalog | 中 | §5 |
| D-3 | `_perms` 接口过深，系统路径用 SystemPrincipal 绕过 | 高 | §5 |
| D-4 | 查询是 Appwrite 字符串，不是类型化 AST | 高 | §5 / §7 |
| D-5 | 三件独立缺口：读路径 migrator、`Array` 不落地、懒 ALTER `_version` | 高（应拆开） | §5 |
| D-6 | Staged transactions：**已删除**（内测无兼容） | 中 | §5 |
| D-7 | catalog 双份（public 幽灵表 + 项目 schema） | 中 | §5 |
| D-8 | Client/Server Databases 用例分叉（R-1 的实例） | 中 | §5 / §7 |
| D-9 | 用户 collection 默认 `read:any` 是产品默认，不是系统表化遗产 | 中 | §5 |

### 1.5 身份与授权

| ID | 标题 | 冲击 | 章节 |
|---|---|---|---|
| A-1 | `Principal` 是字段袋，不是封闭身份 | 最高 | §6 |
| A-2 | 两套 Principal；Client Databases 投影丢 `PlatformAdmin` | 高 | §6 |
| A-3 | 六套授权词汇重叠且碰撞 | 高 | §6 |
| A-4 | CredentialType 在验证边界被改写；`sessions.secret_hash` 未哈希 | 高 | §6 |
| A-5 | 三套签发栈；真正的洞在 admin refresh 不读库 | 高 | §6 |
| A-6 | `Account` 上帝对象；`domain/auth` 端口过多（旋转/限流不要折进 nonce） | 高 | §6 |
| A-7 | JWT Claims 与 Principal 双轨 | 中 | §6 |
| A-8 | 传输层三处认证 + System actor 假扮 API key（两条，表态时分开） | 高 | §6 |

### 1.6 API 与用例

| ID | 标题 | 冲击 | 章节 |
|---|---|---|---|
| R-1 | Documents 等 Client/Server 资源克隆（对照 K-8 / 经济单用例） | 高 | §7 |
| R-2 | 201 RPC 对 Agent 过大；对 Console/CLI/SDK 是完整产品 API | 中 | §7 |
| R-3 | gRPC handler 九成是浅映射 | 中 | §7 |
| R-4 | scope/角色表是第二份 API 规格（120 条；fail-closed 见 K-9） | 中 | §7 |
| R-5 | 自定义 HTTP / Realtime 对 Agent 不可见 | 中 | §7 |
| R-6 | CreateOrder 仍拒绝 `PurposeSubscription`，Subscribe 自行建同类订单 | 低 | §7 |
| R-7 | Storage/Functions 保持 Server-only（v1 产品决定） | 低 | §7 |

### 1.7 进程与装配

| ID | 标题 | 冲击 | 章节 |
|---|---|---|---|
| C-1 | server/worker 切的是运维，不是模块 | 中 | §8 |

### 1.8 该保留（不要在还债时推翻）

| ID | 标题 | 章节 |
|---|---|---|
| K-1 | 支付四渠道归一 + 订单状态机 + 三锚点幂等 | §9 |
| K-2 | 资产 defs/holdings/ledger；禁止走 `_perms` | §9 |
| K-3 | 文档 ACL 纯函数可测 | §9 |
| K-4 | outbox 与写同 COMMIT | §9 |
| K-5 | 项目 schema 作为租户物理容器 | §9 |
| K-6 | 用户 collection 列存（非 JSONB 大表） | §9 |
| K-7 | Console 走 Server API，不建第三套资源协议 | §9 |
| K-8 | Groups「一个核心 + Client 策略包装」 | §9 |
| K-9 | 启动期 scope/角色表与 proto 对齐（fail-closed） | §9 |
| K-10 | `ObjectStore` 作为外部对象存储缝 | §9 |
| K-11 | `OAuthAuthenticator` 多厂商适配 | §9 |
| K-12 | 订阅状态机 | §9 |
| K-13 | `pkg/ident` charset 与一段式/两段式不相交 | §9 |
| K-14 | `projectschema.Apply`（embed、advisory lock、dirty、同 Tx） | §9 |
| K-15 | `documentSchema` vs `businessSchema` DDL 分叉 | §9 |
| K-16 | `RefreshRotationStore` 原子旋转与 reuse 检测 | §9 |
| K-17 | HMAC file token + `jwtparser` purpose 域分离 | §9 |
| K-18 | `provider_resource_index`（无项目头回调定位） | §9 |
| K-19 | `billing.HourBucket` / 小时 rollup | §9 |
| K-20 | `pkg/crud` AIP 列表/分页 | §9 |
| K-21 | 测试适配器（`MemObjectStore`、支付 fake、`fakeDocDB`） | §9 |
| K-22 | 端用户 Session 热路径（上限驱逐、rotation key） | §9 |
| K-23 | 经济表物理位置以数据面方案为准（`tw_<project>`，不是过时的「必须 public」） | §9 |

### 1.9 目标架构与演化（规划提案，不核「当前事实」）

| ID | 标题 | 章节 |
|---|---|---|
| T-1 | 目标模块图 | §10 |
| T-2 | 目标身份：Actor（EndUser \| Admin \| Service \| System）× Credential × Grant | §10 |
| T-3 | 目标数据面 | §10 |
| T-4 | 完整 RPC 保留；Agent 工具目录是 overlay（量级约 20，非规格） | §10 |
| E-1 | 抽出 User/Session 聚合 | §11 |
| E-2a | 合并 Documents 为单一 use-case（非 breaking） | §11 |
| E-2b | 合并 client/server `message Document`（breaking，单独版本策略） | §11 |
| E-3 | 一个 Authenticate + Actor ADT（`Service` 不是 `Agent`） | §11 |
| E-4 | Query AST 进 proto；字符串 DSL 降为 codec | §11 |
| E-5 | 系统表化（先写独立设计）；删除 sentinel 与系统集合守卫 | §11 |
| E-6 | 拆 DocumentDB 为 Catalog / SchemaApplier / Documents | §11 |
| E-7 | Tool catalog overlay；201 RPC 留作完整 API | §11 |
| E-8 | 收 Redis 挑战存储、删死端口、gRPC status 退出 domain | §11 |

### 1.10 代表路径（抽样，不单独 accept/reject）

| ID | 路径 | 章节 |
|---|---|---|
| V-1 | CreateUser / SignUp | §12 |
| V-2 | CreateDocument | §12 |
| V-3 | CreateOrder | §12 |
| V-4 | CreateExecution | §12 |

---

## 2. 产品定位

### P-1 三身份对接口要求不同；冲突发生在 User/Session 仍走文档引擎

**事实**

仓库同时陈述三件事：

1. Appwrite-inspired BaaS（认证、动态文档、存储、函数、Client/Server/Console）。见 `README_ZH.md`、`docs/developer/01-overview.md`。
2. AI/Agent-Native（Protobuf + OpenAPI、scoped API Key、SDK）。见 `docs/roadmap.md` §0。
3. 经济平台（支付、资产、订阅、用量计费）。见 `docs/design/v3-payments-economy.md`。

这三件事对接口的要求互相冲突：

| 身份 | 它要求的接口 |
|---|---|
| Appwrite 兼容 BaaS | 大 CRUD 表面、字符串 DSL、`_perms`、Client/Server 克隆、系统资源当 collection |
| Agent-native 后端 | 小工具目录、类型化查询、稳定资源契约、可预测错误、scope 即能力 |
| 经济平台 | 状态机、幂等、账本、履约与支付同事务、禁止用户直写 |

经济模块明确拒绝了文档层（v3 D1：资产/订单/订阅不放动态文档）。身份与用户没有做同样的拒绝。Account 热路径把用户当文档写：

- `internal/app/client/account.go`：`ListDocuments` / `CreateDocument` 作用于 `SystemDatabaseID` + `"users"`。
- `internal/app/server/users.go`：`CreateUser` 同样 `docDB.CreateDocument(..., "users", ...)`。

**判断**

BaaS + 经济 + Agent 叠层本身合法（Firebase/Appwrite 类产品就是大 CRUD；经济已拒绝文档层，见 v3 设计 D1）。冲突点不是「不该同时做三件事」，而是 **User/Session 仍走 `DocumentDB`，经济没有。** Agent 表面是 roadmap P3 叠加，不是第三套资源模型。

最优设计仍钉：

> Project 是租户。User / Session / File / Order / Holding 是一等资源。Collection 只装开发者文档。Agent 看到的是资源树 + 一层工具 overlay，不是三套 RPC 目录。

### P-2 Agent-native 是挂钩，还不是工具表面

**事实**

已具备：`proto/` 单一事实来源、`genproto/**/*.swagger.json`、Server API + `api_keys.scopes`、`method_auth` 注解、Go/TS SDK、`sdk/go/server.InvokeJSON`。

缺口：

- MCP / Tool Schema / Agent Key 模板仍在 roadmap P3。
- 查询是 `equal("email","a@b.com")` 字符串（`pkg/query`），proto 无 Filter AST。
- 自定义 HTTP（上传、OAuth 回调、webhook、`GET /v1/realtime`）不进 OpenAPI。
- RPC 规模精确为 Client 69 + Server 122 + Console 10 = **201**（`proto/**/*.proto` 中 `rpc` 计数）。

**判断**

OpenAPI 与 scoped key 是正确挂钩，现有 SDK 有价值。roadmap 里「仅凭 API Key + OpenAPI/Swagger 可完成列出用户、CRUD 文档、上传文件」已经达到**挂钩级**验收，不是当前声明作假。缺口是下一层：机器填不出类型化查询、非 unary 半边不进 swagger、没有高杠杆工具 overlay（「约 20 个」是规划量级，不是规格）。不要把这件事写成「把产品 API 砍到 20 个动词」——完整 RPC 留给人类 / Console / CLI（E-7 / T-4）。详见 D-4、R-2、R-5。

---

## 3. 领域模型

### M-1 `internal/domain` 多数是端口目录；有不变式的是订单/订阅/ACL/资产 class

**事实**

- `internal/domain/provides.go` 的 `ProviderSet` 为空（注释 “when needed”）。
- 多数包是结构体 + CRUD/KV 接口，行为在 app 或 infra。
- 有真正不变式的少数模块：`databases/permissions.go`（ACL 纯函数）、`payments/order.go`（`Order.Transition`）、`subscriptions/subscription.go`（`Subscription.Transition`）、`assets/class.go`（class 矩阵）。

删除测试：删掉 `domain/users` 或 `domain/groups`，复杂度几乎不消失（只是校验函数挪地方）。删掉 `DocumentDB`，则 Account、Users、Groups、Storage、Session、Databases 全部倒塌。

**判断**

这是组织方式，不是「必须先拆文件夹才能继续做产品」。演进目标是把身份做成与订单/ACL 同样形状，而不是先删掉 `internal/domain`。S-5（空 ProviderSet）是同一诊断信号，不单独立债。

### M-2 系统实体是无类型文档

**事实**

| 概念 | 领域类型 | 实际存储 |
|---|---|---|
| User | 无。仅 `ValidateStatus` / `CanAuthenticate` / `ValidatePasswordStrength` | `users` 系统集合文档，`Data map[string]any` |
| Session | 无聚合。`SessionService` 是端口 | `sessions` 文档 |
| Identity | `auth.Identity` 结构体无行为、无仓储 | `identities` 文档 |
| Group / Membership | 常量 + `ValidateStatus`/`ValidateRole` | `groups` / `memberships` 文档 |
| Bucket / File | DTO，元数据「住在动态文档层」 | `buckets` / `files` 文档 |
| Admin | 结构体挂在 `domain/projects` | `public.admins` 表（bun） |
| API Key | 结构体在 `domain/projects` | `public.api_keys` 表 |

系统集合名单：`internal/domain/databases/system_collections.go`（users、sessions、identities、groups、memberships、buckets、files）。属性规格在 `internal/infra/documentdb/system_collection_specs.go`。无 FK：`sessions.user_id` 不是引用 `users._id`。

`domain/users/password.go` 与 `domain/groups/membership.go` 返回 `google.golang.org/grpc/status`。

**判断**

系统集合作为存储技巧可以暂时存在，但不能成为领域语言。User 应有邮箱唯一、状态、密码、匿名属性等不变式；Session 应有签发/轮换/吊销；Membership 应有 owner 不能离开等规则。这些今天全在 use-case 的 `map[string]any` + DSL 里。

与 D-1 同一事实的两端：M-2 是领域语言缺失，D-1 是存储形态。系统表化在 `docs/design/project-data-plane-schema.md` 里是**方向**（该稿仍为 Draft；K2 拍的是「本方案只搬位置，形态另案」），**没有**独立设计、执行计划或 ADR。不要再立一张「未规划的永久错误」新单，也不要把本文当成 E-5 规格。第一性原理贡献的是 **User 聚合**（E-1），不是再发明一次搬 schema。

### M-3 用户文档引擎是产品；`DocumentDB` 端口是神对象

**事实**

`internal/domain/databases/repository.go` 的 `DocumentDB` 把四件事焊在一个接口上：

1. Database / Collection / Attribute / Index（catalog + DDL）
2. Document CRUD / upsert / bulk / count / sum
3. `EnsureSystemCollections` 引导
4. 每个文档方法都带 `Principal`（ACL 下沉到 adapter）

生产适配器只有 `postgresDocumentDB`（`internal/infra/documentdb/postgres.go`）。该类型还持有连接、outbox publisher、以及五套进程缓存（internalID、bootstrap、keys 清理、`_version` 已提交列、本事务内 `_version` ALTER 标记）。

调用方包括：Account、Users、Groups、Storage、SessionService、Client/Server Databases、billing 引导。

**判断**

对**用户 collection**，动态 schema + CRUD + 可选 ACL **就是** BaaS 产品，不要删「文档产品」。问题是：(1) 系统资源不该走这条口（D-1）；(2) catalog / DDL / 文档 / 引导焊在一个接口上。拆 Catalog / SchemaApplier / Documents（E-6）。规划语言分开「引擎」和「端口」——S-1 表里不要把 DocumentDB 当成「假缝所以可删」。

### M-4 资产五动词应收进 Assets 领域服务（不放 Holding）

**事实**

领域有 `Def` / `Holding` / `LedgerEntry` 与 class 校验（`NaturalUniquePerOwner` / `RequiresExpiry` 在 `assets/def.go`，`ValidateDefMatrix` 在 `assets/class.go`）。Grant / Consume / Transfer / Mutate / Expire 实现在 `internal/app/assets/write.go`。仓储接口带 `GetByCodeForShare`、`GetByIDForUpdate`、`ListExpiredInProject`（`FOR UPDATE SKIP LOCKED`）。

**判断**

class 矩阵已经是深模块。五动词应成为单一 **`Assets` 领域服务**的接口，不要放上 `Holding` 实体——Grant / Consume / Transfer 跨 Def + Holding + Ledger，放实体上是反向的 Feature Envy。仓储只负责持久化；锁策略按 S-2 留在模块内部，不暴露给所有调用方。冲击低于 User-as-document，因为经济路径至少有不变式函数可测。

### M-5 Database 无独立类型

**事实**

`DocumentDB.GetDatabase` / `ListDatabases` 返回 `*Collection` / `[]Collection`（`repository.go`，adapter `postgres.go`）。不存在 `Database` 类型。

**判断**

领域模型还没切开 Database vs Collection。这是 M-3 的类型层症状，修复随 DocumentDB 拆分一起做。

### M-6 事件信封超载

**事实**

`internal/domain/events/envelope.go`：一个 `Envelope` 同时服务文档写事件与 v3 经济事件（`Domain` / `Channel` / `Attrs`）。出站用 `ClientPayload()` 剥 ACL。表名仍是 `document_events_outbox`。

`AccountsChannel()` 在 `assets`、`payments`、`subscriptions` 各写一份同构函数（不是包级常量）。

生产 publisher 一个（outbox）。`EventPublisher` 注释要求感知 `bun.Tx` / `clients.Conn`。

**判断**

复用同一条 outbox 管道是对的（K-4）。把文档专用字段和经济专用字段塞进同一个类型，让所有调用方理解两套形状，是浅接口。应收成 `events.Publisher` + 分域 payload，频道命名一个函数。表可改名，也可暂时不改。

---

## 4. 模块深度与缝

### 真缝与假缝（S-1 事实表）

生产适配器 + 仓库里实际使用的测试适配器。**测试 fake 算正当缝。**

| 端口 | 适配器 | 裁决 |
|---|---|---|
| `payments.PaymentProvider` | Stripe、WeChat、Alipay、iOS IAP；测试有 `fakeProvider` | **真缝** |
| `payments.CallbackAcker` | WeChat、Alipay、iOS（Stripe 未用） | **真缝**（协议变异） |
| `auth.OAuthAuthenticator` | Google、GitHub、WeChat 家族 | **真缝**（工厂在 infra，app 直接调） |
| `payments.ReceiptVerifier` | iOS only | 窄真缝 |
| `storage.ObjectStore` | MinIO + `testutil.MemObjectStore` | **真外部缝**（K-10、K-21） |
| `messaging.Mailer` / `SMSSender` | 各一（厂商 + dev log 内嵌） | 近假缝 |
| `subscriptions.HostedBilling` | Stripe only | 单适配器；等第二家再升端口（同 K-12） |
| `databases.DocumentDB` | postgres + 多处 `fakeDocDB` | **引擎是产品**（M-3）；端口过载。测试 fake 正当（K-21） |
| 带锁/幂等契约的经济 bun `*Repo` | 各 1 + 集成测走 PG | 接口承载 `FOR UPDATE` / 幂等 insert，**不要当空 CRUD 删** |
| 空 CRUD bun 端口 | 各 1 | 可收；测试若只走接口可留 |
| Redis 一次性挑战（OTP、OAuth state、one-time JWT、account token、MFA challenge） | 各 1 | 可合并为 `NonceStore` |
| `RefreshRotationStore` | Redis CAS + reuse 检测 | **正当缝**（K-16），不是 nonce |
| `RateLimiter` / `LoginThrottle` / `UploadSessionStore` | 各 1 | 语义不同，单独留 |
| `shared.EventPublisher` | outbox only | 实现细节；管道本身保留（K-4） |
| Realtime Transport / Fanout / Hub | Redis Streams + 进程内 Hub | 单生产故事；注释「日后可换」尚未发生 |
| `functions.Executor` | Docker only | 单适配器；T-1 预留第二运行时再升端口 |
| `idgen.Generator` | 一个 Service（内部策略切换） | 内部策略，不必对外端口 |
| `auth.SessionService` / `MFAService` / `UserRoleResolver` | 各 1 | **错位的 use-case**，不是缝 |
| `projects.ProjectResolver` | **零个引用**（仅接口定义） | **死缝，该删** |

### S-1 死端口与过宽假缝定义

**判断**

假缝 = 既无第二种生产故事、**测试也不走接口**。Go 里「一个生产适配器 + 测试 fake」是正当缝（K-21）。不要执行「一律删 bun 端口、Account 单测必须起 PG」。

该做的：删 `ProjectResolver`；合并真正的一次性挑战存储。**不要**把 `RefreshRotationStore`、限流、分片上传会话折进 `NonceStore`。经济仓储上的锁与幂等是契约，不是仪式（对照 S-2）。

### S-2 SQL 锁语义抬进领域接口

**事实**

`GetByIDForUpdate`、`GetByCodeForShare`、`ListExpiredInProject` / `ListDueForBillingInProject` / `CloseExpiredInProject` 出现在 assets / payments / subscriptions 端口上。注释写明 `FOR UPDATE` / `FOR SHARE` / `SKIP LOCKED`。staged-transaction 仓储的 `LockPending`（曾在 `domain/databases/transaction.go`）已随 D-6 删除。

**判断**

锁策略泄漏是浅接口；经济正确性依赖这些临界区（worker 排空、回调锁单）。收口时把临界区收进模块内部可以，**不要换成无锁的假仓储**。生产只有 PG，这些路径的测试本来就用本地 PG。

### S-3 分层仪式：app import infra，domain 返回 gRPC

**事实**

app 生产代码 import infra（不只是测试）：

- `internal/app/client/account.go` → `infra/auth`、`infra/documentdb`
- `internal/app/server/projects.go` → `infra/bun/model`、`infra/clients`、`infra/projectschema`
- `internal/app/assets`、`payments`、`subscriptions` → `infra/clients`（`RunInTx` / `Conn(ctx)`）
- `internal/app/client/email_otp.go`、`phone_otp.go`、`mfa.go`、`oauth2.go` → `infra/auth` / `infra/documentdb`

domain / pkg 泄漏传输：

- `domain/users/password.go`、`domain/groups/membership.go` → `status.Error`
- `pkg/ident/ident.go` → gRPC codes 报非法 id
- `PaymentProvider.VerifyCallback(http.Header, []byte)`（`domain/payments/provider.go`）
- `RateLimiter` 约定返回 `codes.ResourceExhausted`

文档曾把「用例层映射为 gRPC status」写成约定（`docs/developer/09-api-guide.md`）。

gRPC handler 与 201 个 RPC 基本 1:1，典型形状是取 Principal → 调 use-case → `mapXxx`。真正赚到协议适配价值的：`serverhttp/file_handler.go`、`functions_handler.go`、`oauth_handler.go`、`payments_handler.go`、`realtime/handler.go`、console cookie。

**判断**（两条，表态时分开）

1. app 生产代码 import infra、domain/pkg 返回 gRPC status：该修。领域错误应是 sentinel（支付包已做到：`ErrSignatureInvalid`）；映射留在 api。
2. 「四层文件夹是仪式」只适用于空 CRUD 端口。v3 有用的是实体上的 `Order.Transition` / class 矩阵，外加仓储上的锁与幂等——那不是仪式。E-8 不要套到 `OrderRepo` / `HoldingRepo`。

### S-4 事务缝是 ctx 魔术

**事实**

`EventPublisher.Publish` 注释：必须感知 ctx 中的 `bun.Tx`（`clients.Conn`）。写路径在同一事务内调用；未在事务中则自行短事务插入。经济写路径同样靠 `clients.Database.RunInTx` + ctx 传递连接。

**判断**

调用方必须知道「我现在在不在事务里」。对外缝应是 `uow.Run(ctx, func(tx Tx) error)`，适配器接收 `Tx`，而不是在领域端口注释里写 bun。落地从宽：实现仍可用 ctx 传连接，不必一夜重写所有 bun 调用点；先消灭端口注释里的 `bun.Tx` 契约。

### S-5 `domain.ProviderSet` 为空（M-1 的诊断信号，非独立债）

**事实**

`internal/domain/provides.go` 只有空 `wire.NewSet`。

**判断**

与 M-1 同源：领域没有可装配的服务。本身不是独立问题，是诊断信号。

---

## 5. 数据面

### 当前物理布局（事实）

| 层 | 实现 | 职责 |
|---|---|---|
| `public` | `db/migrations/` + bun | 控制面 + 事件脊柱（projects、api_keys、outbox）。000001 仍建了一套 `document_*` catalog，运行时已不用。 |
| `tw_<project>` | `projectschema.Apply` + DocumentDB | 项目 catalog、账本、Functions、系统文档表、`_perms` |
| `tw_<project>_<database>` | `CreateDatabase` → `CREATE SCHEMA` | 用户 collection + 该库 `_perms` |

命名：`pkg/ident` 的 `ProjectSchemaName` / `SchemaName` / `ProjectDataPlaneID = "_"`。系统集合寻址：`databaseID == "_"` 时映射到一段式项目 schema，不建 `tw_<p>_`。

DDL 分叉（真不变式）：`documentSchema` 允许 sentinel → `ProjectSchemaName`；`businessSchema`（Create/DeleteDatabase）永不映射一段式。见 `internal/infra/documentdb/postgres.go`。

### D-1 系统资源仍是文档（形态错；系统表化是方向，尚无独立设计）

**事实**

users / sessions / identities / groups / memberships / buckets / files 经 `CreateCollection` 建动态表，属性来自 `system_collection_specs.go`，读写走 DSL。

为防 Databases API 摸到系统表，出现一整支守卫：

- sentinel `"_"`、`RejectExternalDatabaseID`、`isWriteProtectedSystemCollection`
- Server 脱敏字段表（`app/server/databases.go` 的 `serverSensitiveCollectionFields`）
- Client 敏感字段黑名单
- `cleanupKeysWritePerms`
- `EnsureSystemCollections` 挂在 Client `loadProject`、Account、Storage、billing 上

Account 注册/登录走 `ListDocuments` + `query.BuildEqual("email", …)`，不是 `GetUserByEmail`。

`docs/design/project-data-plane-schema.md` 自己写明：系统集合保持文档形态是**临时决策**，后续将系统表化。本方案只搬了物理位置。

**判断**

形态仍错（无 FK、Account 走 `ListDocuments`+DSL、`SystemPrincipal` 旁路）。物理位置搬到 `tw_<project>` 已做对（K-5）。系统表化是数据面文档写明的**方向**，载体仍应是 `projectschema`（K-14 / E-5），但该另案尚无独立设计——未设计前不排进施工。第一性原理贡献的是领域语言（M-2 User 聚合），不是再发明一次搬 schema。退役 sentinel 与 Databases 系统集合守卫是 E-5 的验收项，完成前不要拆 `businessSchema` / `RejectExternalDatabaseID`（K-15）。

### D-2 三层 schema 已买到隔离；过渡税是 sentinel 寻址与双 catalog

**事实**

- catalog-only 行 `document_databases(id='_')`，List 再滤掉（`server/databases.go`）。
- project.id / database.id 禁 `_`，以免 `tw_shop` 与 `tw_shop_app` LIKE 陷阱。
- 业务表仍 `PRIMARY KEY (_tenant, _id)` 且 tenant 默认 `internal_id`；schema 已按项目切开后，`_tenant` 是恒真谓词。
- Worker 必须 `ListProjects` 再进各 schema；跨项目回调靠 `provider_resource_index`。

**判断**

三层已经买到：`default` 可删、系统表不在业务库、`businessSchema` 防止 `DeleteDatabase("_")` DROP `tw_shop`。`"_"` **不是**假 database（不建 `tw_<p>_`），是过渡寻址（数据面设计 K4）。多余的税是 sentinel 寻址本身、双 catalog、残留 `_tenant`（owner 已决定 `_tenant` 留到系统表化）。不要把结构保险写成「没买到隔离」。

### D-3 `_perms` 接口过深，系统路径用 SystemPrincipal 绕过

**事实**

领域函数 `AllowsDocumentAccess` / `ValidateGrantablePermissions` / `ListAccessDenied`（`permissions.go`）把 documentSecurity、系统集合 OR vs 用户集合覆盖、grantability 写成可测函数。

每个 DocumentDB 文档方法都要 `Principal`。Account 热路径几乎全是 `SystemPrincipal`（`access.go`：`PlatformAdmin || __system__` 旁路）。

默认集合权限见 **D-9**（用户 collection 产品默认，不是本条的系统表化债）。

List 用 `_perms` 相关子查询；Get 才 `attachDocumentPermissions`——同一 `Document` 类型两种形状。

**判断**

领域函数集中是好的（K-3）。`Principal` 出现在端口每一个方法上是泄漏。系统调用方用 System 绕过，说明模型不服务他们。系统资源需要服务级授权（session 属主、bucket policy），不是文档 ACE。文档 ACE 应是用户 collection 的可选项，不进入 Account。

### D-4 查询是 Appwrite 字符串，不是类型化 AST

**事实**

`pkg/query.Parse` 把 `equal("a","b")` 收成 `Filter{Op, Attribute, Values[]string}`。无 `or`/`and`/`not` 分组。值全是 string。SQL 编译在 `postgres.go` 的 `buildAppwriteQuery`（`contains` → `ILIKE`）。

proto `ListRequest` 同时有 AIP `filter`/`order_by` 与 Appwrite `queries`（`proto/shared/v1/common.proto`）；文档类资源 handler 只接线 `queries`。例外：`servergrpc/projects.go` 的 ListProjects **把** AIP `filter`/`order_by` 传进 use-case，但 `ListProjects` 只做全量拉取 + offset 切片，**并不应用** Filter/OrderBy（`crud.ParseListParams` 只解析分页）。对 E-4 这是 handler 接线先例，**不是**可抄的 AIP-160 实现。

分页：DSL `offset`/`cursor*` 与 AIP `page_token` 叠两套。

**判断**

对人类抄 Appwrite 友好，对 Agent 不友好。应升格为 proto `Query` AST（eq/in/range/text/and/or + keyset page）；`pkg/query` 变成 Appwrite codec。集合 schema 作为工具描述。这是 P-2 在数据面上的具体化。ListProjects 的 AIP 字段接线不能当过滤/排序已落地。

### D-5 三件独立缺口：读路径 migrator、`Array` 不落地、懒 ALTER `_version`

**事实**

CreateCollection/Attribute/Index 与 catalog 同事务（正向写路径正确）。缺口是三件独立的事，不要绑成一条架构债：

- 只对系统集合单向补列（`reconcileSystemCollectionAttrs`）。用户 collection 无 physical↔catalog 对账。
- `Attribute.Array` 写入 catalog，DDL 忽略（`attributeColumnSQL` 无 array 类型；`pgTypeFor` 不产出 array）。
- `_version` 不在 catalog，靠写路径 `information_schema` + `ALTER TABLE ... ADD COLUMN`（AccessExclusiveLock）。
- 每次 `GetCollection` 先 `projectschema.Apply`（`ensureProjectCatalog`）。`Apply` 取 advisory lock 并 `CREATE SCHEMA IF NOT EXISTS`；已迁移版本会跳过，**不是**每次读都跑完整 DDL，但仍把 migrator 焊进读路径。

**判断**

可查询字段用真列是对的（K-6）。三件事拆开处理：

| 事 | 性质 | 建议 |
|---|---|---|
| `Attribute.Array` catalog 写入、DDL 忽略 | **产品撒谎**，独立缺陷（Wave 0） | 实现计划锁定：**拒绝** `array=true`（实现 PG array 另开产品单） |
| 写路径懒 `ALTER _version`（AccessExclusiveLock） | **运维地雷**，独立缺陷（Wave 0） | **不要**新 `projectschema` SQL（用户表名来自 catalog，静态迁移写不出）。新建表继续 CREATE TABLE 带列；存量缺列做一次 catalog 驱动 reconcile；禁止写热路径 ALTER。**禁止**给系统集合加 `_version` |
| `GetCollection` → `projectschema.Apply` | 架构 | 随 E-6：SchemaApplier 只走写路径 / 启动对账。已迁移版本会 skip，冲击低于前两件。**不进 Wave 0** |

### D-6 Staged transactions 是兼容层，不是数据面原语

**已执行**：整面删除 staged API + 表 + 信封 `transaction_id`；热路径 = 单 RPC + `uow.Run`。

对外只保留 Documents CRUD（含 BulkUpdate/BulkDelete）。内部保留 `pkg/uow`、`clients.Database.Run`/`RunInTx`、outbox 与文档写同事务。不新造跨文档原子 API。

### D-7 catalog 双份

**事实**

`db/migrations/000001_init_tables.up.sql` 在 `public` 建 `document_*` catalog。运行时 catalog 在 `tw_<project>.document_*`（`internal/infra/projectschema/migrations/`）。

**判断**

幽灵表增加心智负担。已核对：全仓 catalog 读写均带项目 schema 限定（`?.document_*`），public 的四张 `document_*` 无任何运行时读路径，删除安全——系统表化或一次独立迁移即可删。

### D-8 Client/Server Databases 用例分叉

**事实**

Database/Collection DDL 用例仅在 Server（`app/server/databases.go`）。分叉的是**文档**用例：`internal/app/client/databases.go` 与 `internal/app/server/databases.go` 各自实现文档 Create/List/Get/Update，系统集合守卫、OCC version、grant 校验两边各写。Client 额外：guest 读、owner 默认 ACE、敏感字段过滤。Documents 没有抽成共用核心。

proto `message Document` 在 client 与 server 各一份。

**判断**

这是 R-1 的实例。应学 Groups（K-8）与经济：一个 Documents 核心 + 策略对象（client：owner 默认权限、guest 读；server：脱敏、platform admin）。Handler 只做投影。抄的是组合模式，不是 Groups 仍返回 `*databases.Document` 的领域形状（K-8 / E-2a）。proto 消息合并是 breaking，单独做（E-2b）。

### D-9 用户 collection 默认 `read:any` 是产品默认，不是系统表化遗产

**事实**

`DefaultCollectionPermissions`（`internal/domain/databases/permissions.go`）对未声明权限的**用户集合**给：

- `read:any`（公开读）
- `users` / `keys` / `admin` 的 create/update/delete

Server `CreateCollection` 在 `len(perms)==0` 时套用这组默认（`app/server/databases.go`）。这是用户 collection 的产品默认，不是系统集合寄居留下的疤。

**判断**

系统表化（E-5）之后这组默认仍然在。不要塞进 E-5 退役清单。

- `read:any` 是产品脚枪：集合默认公开读。是否改为 `read:users`（或创建时强制显式权限）是产品决定，独立于表形态。
- `keys` / `admin` 全开是 Server / Agent / Console 能不逐集合授权的前提，应显式承认，不能当 Appwrite 遗产删掉。

---

## 6. 身份与授权

### 身份概念地图（事实）

| 概念 | 实际是什么 | 落点 |
|---|---|---|
| End user | 项目数据面 `users` 文档 | `app/client/account.go`；spec 在 `system_collection_specs.go` |
| Anonymous | 仍是 End user：假邮箱 + `labels=["anonymous"]` + 正常 JWT/session | `app/client/anonymous.go` |
| OAuth identity | `identities` 文档 | `app/client/identity.go` / `oauth2.go` / `wechat.go` |
| Session（端用户） | `sessions` 文档 + access/refresh JWT + Redis rotation；另有 HMAC cookie | `domain/auth/session.go`；`infra/auth/session_service.go`；`session_cookie.go` |
| Admin | 控制面 `admins` 表，挂在 **projects** domain | `domain/projects`；签发 `app/console/auth.go` |
| Admin「session」 | 无 session 文档。Access JWT 进 `TORCHWOOD_session_console` cookie + 全局 revoke + 按 adminID rotation | `api/consolegrpc/cookies.go`；`domain/auth/admin_token.go` |
| API key（Service actor；Agent 是其消费者之一） | `api_keys`；`ActorKind=service`，`Roles=["keys"]`，`Permissions=scopes` | `infra/auth/validator.go` |
| System / worker | 伪造 `ActorKind=service` + 空 `APIKeyID` | `app/assets/authz.go` |
| JWT claims | 平行结构：`akd`/`uid`/`sid`/`rls`/`scp`/`one_time` | `pkg/jwtparser/jwt.go` |
| Document principal | 第二套 Principal：只有 `Roles` + `PlatformAdmin` | `domain/databases/access.go` |

`domain/auth/` 几乎没有身份聚合：全是 Redis/会话端口和 provider 常量。用户在 documents 里，管理员在 projects 里，API key 也在 projects 里。Identity 没有一个家。

### A-1 `Principal` 是字段袋，不是封闭身份

**事实**

`internal/domain/shared/principal.go`：

- 可选字段：`ActorID`、`ActorKind`、`CredentialType`、`IsPlatformAdmin`、`ProjectID`、`UserID`、`APIKeyID`、`SessionID`、`Roles`、`Permissions`、`Email`。
- `IsAuthenticated` = `UserID != "" || APIKeyID != ""`，不看 `ActorKind`。
- Admin 的 id 放进 `UserID`（`validator.go`）。
- `HasPermission` = `HasRole || HasScope`（console 角色、文档角色、API scope 同一集合）。
- `HasAnyPermission([])` **fail-open**，注释依赖启动期 `collectMethodsByAccess` 守门。

`requireUser`（Account）只看 `UserID != ""`，类型系统挡不住 admin Principal。

Worker 注入：`ActorKind=service` + `CredentialType=api_key` + 无 `APIKeyID`。`shared.Principal` 本身没有 `IsSystem` 方法，「系统」判据有两套且互不一致：assets 的 `OperatorSnapshot.IsSystem` 用 `ActorKind==service && APIKeyID==""`（`internal/app/assets/assets.go:210`），databases 的 `Principal.IsSystem` 用 `PlatformAdmin || HasRole("__system__")`（`internal/domain/databases/access.go:40`）——同一概念三处形状不同，是本条与 A-2 的直接证据。

Anonymous 不是 kind：带 `label:anonymous` 的全权 end_user。

**判断**

方向（ActorKind / CredentialType 两轴）对，粒度不够，也没有被强制。正确形状是封闭和类型：

```text
Actor = EndUser { ProjectID, UserID, SessionID?, Anonymous }
      | Admin   { AdminID, PlatformRole, ProjectBinding? }
      | Service { ProjectID, KeyID, Scopes }
      | System  { Reason }
```

禁止 `UserID` 复用。投影到文档 ACL 是 Actor 的方法，不是各 handler 手写 `Roles: p.Roles`。`Service` 对应今日 `ActorKind=service`（API key）；Agent 是 Service 的一种消费者（走 T-4 overlay），不是第四种身份。

### A-2 两套 Principal，投影时丢语义

**事实**

`databases.Principal` 只有 `Roles` + `PlatformAdmin`。注释：避免 databases 依赖 shared——然后 `domain/shared/ports.go` 又 import `databases`（`RealtimeConn.DocPrincipal`）。

Client Databases（`app/client/databases.go` 的 `resolveProject`）只传 `Roles`，不传 `PlatformAdmin`。**Groups 没有丢**：`client/groups.go` 传 `PlatformAdmin: p.IsPlatformAdmin`。Server gRPC/HTTP 带上 `PlatformAdmin`。`app/client/transactions.go` 曾同样丢 `PlatformAdmin`，已随 D-6 删除。

**判断**

这是包依赖驱动的身份分裂，不是领域需要。同一主体在 Databases 与 Groups 上变成不同文档权限。应一个 identity 类型；ACL 视图在 use-case 边缘派生。

Client 丢 `PlatformAdmin` 是这条的实例，但生产影响低：Console 打 Server（K-7），端用户 Principal 没有 `IsPlatformAdmin`。当作 E-3 的回归用例，**不要当独立 P0 缺陷单**。

### A-3 六套授权词汇重叠且碰撞

**事实**

从外到内：

1. proto `method_auth`（`PUBLIC | AUTHENTICATED | PERMISSION | API_KEY`）。`ACCESS_AUTHENTICATED` 在方法自身 permissions 为空时被改写成 `permissionMethods[method] = ["users"]`（`internal/infra/server/grpc.go`）。Account 热路径许多方法本来就是 `ACCESS_PERMISSION` + `["users"]`。
2. gRPC interceptor（`pkg/grpc/interceptor/jwt.go`）：public / API_KEY（key 或 admin）/ permission map / admin 角色表。
3. `apiKeyScopeRules`（`pkg/grpc/interceptor/apikey_scope.go`，120 条方法名）与 `adminRoleMethodRules`（`admin_roles.go`，72 条）。启动 panic 对齐 proto。
4. app 层 `RequirePlatformAdmin` / `RequireAdminActor` / `RequireServerWriteActor`（`app/shared/authz.go`）。`RequireServerWriteActor` 只认 ActorKind admin|service，**不看** viewer vs owner、不看 scope。
5. 文档 `_perms`：`users` / `user:` / `keys` / `admin` / `any` / `guests` / `group:` / `label:`。
6. HTTP（`serverhttp/auth.go`）与 Realtime（`realtime/handler.go`）各自 extract + validate；Realtime 直接禁 API key。

同一字符串 `admin` 同时是：ActorKind、console 角色、文档 ACL 角色、proto permission。`owner` 能过文档 ACL 是因为 `IsPlatformAdmin` bypass，不是角色模型对齐。文档 ACL 认 `admin` 不认 `owner`。

`ACCESS_API_KEY` 名义是 agent 面，实现上 admin cookie 可以进 Server API。产品上合理，模型上「API_KEY」不再描述凭证类型。

**判断**

系统能工作，是因为拦截器表 + 大量纵深 `if` + 启动断言。作为授权模块，它是 Appwrite 风格角色字符串，外加谁都能改写的 Principal 袋。

三面应是三种 Grant，不是三套身份栈：

| 面 | Actor | Grant |
|---|---|---|
| Client | EndUser（anonymous 为属性） | 文档/存储 ACL |
| Server | Service 或 Admin（显式委托） | 资源 scope：`databases.write` 等 |
| Console | Admin | 控制面 RBAC |

拦截器只做 authenticate + 匹配 principal class + 匹配 grant。use-case 调同一 `authz.Check(ctx, Grant)`，不再复制 `RequireServerWriteActor`。`HasPermission` 大 OR 应删除。`Roles` 只服务文档 ACL；scopes 只服务 Service（含 Agent 消费者）；console RBAC 只服务 Admin。

### A-4 CredentialType 在验证边界被改写

**事实**

拦截器把 `TORCHWOOD_session_console` 标成 `CredentialTypeSession`。Validator 对 session 类型先当 JWT 解析；`principalFromJWT` 对 admin 固定写 `CredentialTypeToken`。

于是传输层以为是 session，身份对象以为是 token。后续 `if CredentialType == Session` 不可靠。

端用户还有第三种凭证：HMAC opaque cookie（`session_cookie.go`），与 JWT session、console JWT cookie 并存。

`sessions.secret_hash` 写入明文 UUID，查找只按 document ID（`session_service.go`）。字段名是 hash，值是 secret，**从未按 hash 校验**。

**判断**

Cookie 只是运输，不能改变凭证类型。console cookie 运 access token，解析结果仍是 AccessToken。Session 模型没闭环（死字段 `secret_hash`）说明领域没有 Session 聚合。

### A-5 三套平行签发/刷新/吊销栈

**事实**

| | 端用户 | Console admin | API key |
|---|---|---|---|
| 签发 | `SessionService.IssueTokens*` | `console.Auth.issueAdminTokens*` | 创建时一次 secret |
| 持久会话 | `sessions` 文档 | 无 | 无 |
| 刷新 | 按 `project+session` rotation；reuse → 删 session | 按 `admin+adminID` rotation；reuse → revokeBefore | 无 |
| 吊销 | 删 session 文档 | Redis `RevokeBefore(iat)` | enabled/expire |
| 角色 | 验证时重查 memberships | 验证时重查 admin 表；**刷新用 JWT 里的 role** | scopes 在 key 行 |

Admin refresh **不读库**：`firstRole` 空则默认 `"admin"`。删除的 admin、改过的角色，refresh 仍可能出新 token；access 验证才会失败。端用户 refresh 会 `EnsureActiveSession` + `ensureUserCanAuthenticate`。

console 的 `TokenPair` 是 domain `TokenBundle` 的逐字段复制；client 的 `TokenBundle` 只是类型别名（`app/client/account.go:127`），并非第二份 DTO。`checkAdminTokenRevoked` 在 Auth 与 Validator 各写一遍。

**判断**

端用户 refresh + `RefreshRotationStore` + `EnsureActiveSession` 已经是深模块（K-16、K-22），合并时不要丢掉 reuse→删 session 语义。真正的洞是 admin refresh 不读库、`firstRole` 空则 `"admin"`。签发器可归一；Admin 也要有可吊销的 session 记录，或明确「平台会话 = refresh jti + revoke epoch」一种机制。**该洞不等架构排期**：refresh 路径加一次 `GetAdmin`（存在性 + 当前角色）即可闭合被删/改角色 admin 的无限续签，应作为独立缺陷单立即修，不依赖 E-3 的 Actor ADT（见附录 B）。

### A-6 `Account` 上帝对象；`domain/auth` 是 Redis 端口动物园

**事实**

`app/client.Account` **19** 个构造依赖（cfg、projectRepo、oauthProviders、docDB、sessions、otp、oauthState、tokens、loginThrottle、rotation、idGen、mailer、sms、rateLimiter、roles、mfa、mfaChallenges、oneTimeTokens、auditRepo），方法散落 **13** 个生产文件。生产代码 import `infra/auth`、`infra/documentdb`。

`domain/auth` **13** 个接口，多数各只有 Redis 一个适配器。`SessionService` 实现在 infra，内部用 DocumentDB 写 `map[string]any` 会话文档。`MFAService` 实际只有 TOTP。`OAuthAuthenticator` 是真缝，但 app 通过 infra 工厂构造，不经注入端口。

**判断**

应拆：`IdentityRegistry`（查找、状态、`CanAuthenticate`）+ `CredentialVerifier` 策略（password / otp / oauth / totp / magic）+ `SessionIssuer` + `ChallengeStore`（OTP/MFA/OAuth state 等一次性挑战）。**不要**把 `RefreshRotationStore`、限流、上传会话折进 ChallengeStore（K-16）。

### A-7 JWT Claims 与 Principal 双轨

**事实**

`jwtparser.Claims.ActorKind` 是 `string`，不引用 `shared.ActorKind`。端用户验证**忽略** JWT `rls`，实时 `LoadUserRoles`。Admin 验证忽略 JWT roles，读 `admin.Role`。JWT 里的角色只在 admin refresh 时被当事实。每次请求：JWT 验签 + session 文档点查 + users 状态点查 + memberships 列表拼角色。

**判断**

Claims 同时承担传输身份和（半废弃的）授权快照。边界应是：JWT 只声明 Actor 指针 + token class；授权在验证时从权威存储加载（端用户已这样做；admin refresh 没有）。

### A-8 传输层三处认证 + System actor 假扮 API key

**事实**

- gRPC：`extractCredential` + interceptor
- HTTP：`serverhttp/auth.go` 自己做多 cookie、scope、`X-Torchwood-Project`
- Realtime：第三份，且禁 API key

Worker：`ActorKind=service` + `CredentialType=api_key` + 无 `APIKeyID` + `Roles=["system"]`（`app/assets/authz.go`）。

**判断**（两条，表态时分开）

1. gRPC / HTTP / WS 共享 `Authenticate(raw headers) (Principal, error)`。Realtime 禁 API key、HTTP upload 禁 end-user，是 Grant 配置，不是第三份解析器。
2. System 应是 Actor 的第四变体，不是缺字段的 API key。E-3 落地时应同时消灭三处互不一致的「系统」判据（shared 无、assets 看 `APIKeyID`、databases 看 `PlatformAdmin`/`__system__`）。

---

## 7. API 与用例

### 表面地图（事实）

RPC 计数来自 `proto/` 中 `rpc` 声明：Client **69**、Server **122**、Console **10** → **201**。HTTP 是同一套的 grpc-gateway，外加少量自定义 handler。

| 资源 | Client | Server | 关系 |
|---|---|---|---|
| Account / sessions / MFA / OAuth | 35 RPC → `app/client.Account` | — | 端用户身份，**独有** |
| Users 管理 | — | 9 RPC → `app/server.Users` | 独有，但 SignUp/CreateUser **重复文档写入** |
| Databases DDL / bulk | — | DDL+bulk | Server 独有 |
| Documents + tx | ~15 RPC | 同类 + bulk | **克隆**（tx 核心已共享） |
| Groups | 8 RPC，**包装** `server.Groups` | 12 RPC | **正确模式** |
| Storage | 无 | 12 RPC + HTTP multipart | Server-only |
| Functions | 无 | 16 RPC + zip HTTP | Server-only |
| Payments | 4 | 4 | **一个** `app/payments.Payments` |
| Assets | 3 读 | 13 defs+ops | **一个** `app/assets.Assets` |
| Subscriptions | 4 | 9 | **一个** `app/subscriptions.Subscriptions` |
| Billing | — | 3 | Server + worker |
| Projects / API keys / OAuth providers / Health | — | Server | 独有 |
| Console auth/admins | — | — | `app/console`；Console UI 资源页打 `/v1/server/*` |

Console **不是**第三套资源 API。`console/src/api/client.ts` 使用 `baseURL: "/v1"` + `X-Torchwood-Project`。Console proto 只负责 bootstrap 与 admin 身份。

app 包图实际是两套习惯并存：

- 按 API 产品（遗产）：`app/client`、`app/server`、`app/console`
- 按领域（v3 + functions/storage）：`app/payments`、`assets`、`subscriptions`、`billing`、`functions`、`storage`、`shared`

### R-1 Client/Server 资源克隆

**事实**

平行 `CreateDocument`：`app/client/databases.go` 与 `app/server/databases.go`。相同 `message Document` 两份 proto。staged Transactions 曾在共享核之上再复制 prepare/filter（`client/transactions.go` vs `server/transactions.go`），已随 D-6 删除。

Groups 已经是「一个核心 + Client 策略包装」。经济也是一个 use-case 服务两面 proto。

**判断**

信任边界（用户 JWT vs 项目 key vs 平台 admin）是对的产品缝。Documents 克隆是真税（D-8）；Groups（K-8）和经济单用例（R-6 对照）已经示范正确模式。不是「整平台脊柱都要推倒」，是把 Documents 抄成 Groups 那样。Client 作为用户信任边界要留；目标是一套资源消息 + 策略投影，不是删掉 Client API。

### R-2 201 RPC 对 Agent 过大；对 Console/CLI/SDK 是完整产品 API

**事实**

见上表。Server Databases 一服务 **30** 个方法。Account 一服务 **35** 个方法（OTP/OAuth/MFA/magic/recovery 等认证流，几乎不是 Agent 用 API Key 打的面）。`apiKeyScopeRules` 有 **120** 条。

**判断**

BaaS 产品就是大 CRUD。Agent 用 API Key 打 Server，几乎不调 `CreateEmailOTP`。Console 已经打 Server（K-7）；CLI 靠 `InvokeJSON` 覆盖 Server unary。工具层应**叠在上面**（高杠杆工具 + 类型化查询，roadmap P3 MCP；「约 20 个」是规划量级），不是把产品 API 砍到 20 个动词。E-7 与 T-4 必须一致：201 RPC 留作完整 API。

### R-3 gRPC handler 九成是浅映射

**事实**

典型：`servergrpc/databases.go` `CreateDocument`——取 projectID、audit、`AsMap()`、parse permissions、调 use-case、`mapDocument`。Client 同构。

**判断**

传输层作为协议适配器，浅映射是可接受的——前提是下面的 use-case 深。问题不在 handler 薄，而在薄 handler 下面仍是薄 use-case（再调神端口）。真正该厚的协议适配已经存在（multipart、webhook、WS），应作为这一层的样板。

### R-4 scope/角色表是第二份 API 规格

**事实**

每个新 Server RPC 必须改：proto `method_auth` **和** `apiKeyScopeRules`（120 条）**和** `adminRoleMethodRules`，否则进程启动 panic。这是 fail-closed（K-9），但是方法名注册表，不是资源策略。

**判断**

抽象应是 Grant（`scope:databases.write`、`console.admins.write`），从表驱动方法名改为资源策略。启动对齐可以留到过渡期。

### R-5 自定义 HTTP / Realtime 对 Agent 不可见

**事实**

grpc-gateway 注册全部 Client+Server+Console，再 overlay 自定义 HTTP（`internal/infra/server/grpc_gateway.go`）。上传、OAuth 回调、webhook、`GET /v1/realtime` 不在 swagger。`InvokeJSON` 是 Server unary only，且显式排除 `APIKeysService`（`sdk/go/server/invoke.go:61`）——Agent 经逃生舱连 key 管理也不可达；这是有意的安全设计，但 T-4 工具目录规划时应显式决定 Agent 是否需要任何 key 管理类工具。

**判断**

Agent 用 swagger / InvokeJSON 操作不了产品的非 unary 半边。上传/回调可以保持自定义 HTTP，但应有 OpenAPI 补充或工具封装（「创建 upload session」已是 RPC，分片 HTTP 对 Agent 可隐藏在 SDK 后）。Realtime 应有可发现的订阅描述。

### R-6 v3 经济内部形状对，API 边缘未闭合

**事实**

一个 use-case 服务两面 proto（正确）。`Subscriptions` 依赖 `app/assets` 命令（app-to-app）。Billing 是给运营商的用量汇总，不是对客户扣款产品（与 v3 范围声明一致）。

`CreateOrder` **仍拒绝** `PurposeSubscription`（`internal/app/payments/orders.go`，`"subscription orders are not supported yet"`），而 `Subscribe` 的 `createBillingOrder` 自行构造 `PurposeKind: PurposeSubscription` 的订单并**直接 `orders.Insert` 落库**（`internal/app/subscriptions/subscribe.go:228-258`，幂等键 `sub:{subID}:{cycle}`）——绕过 `payments.CreateOrder` 入口，其校验与审计路径对订阅订单整体不走。

**判断**

模块缝基本对。不要在还债时按 Documents 的克隆模式去「拆」经济。边缘未闭合是一个具体缺陷，验证阶段开单或直接修，不挡架构规划。验收点：要么 `CreateOrder` 接受 `PurposeSubscription`，要么 `Subscribe` 复用同一建单入口——不允许第三条插单路径长期存在。

### R-7 Storage/Functions 保持 Server-only（v1 产品决定）

**事实**

端用户文件走 Server + file token；函数无用户侧 invoke RPC。当前 `sdk/README.md` 能力表已同时列 Client JWT 与 Server API Key。

**判断**

v1 **决定**：Storage / Functions 保持后端专用。端用户下载走 file token（K-17）；不提供 Client 直传 / 用户侧 `CreateExecution`。这不是「没抄完 Appwrite」，是后端优先。需要移动端直传或用户侧 invoke 时再加 Client 投影，届时另开产品单。

---

## 8. 进程与装配

### C-1 server/worker 切的是运维，不是模块

**事实**

worker 负责：函数队列、outbox、支付关单、资产过期、订阅扣款、用量汇总（`cmd/worker/`）。WS fanout 留在 server（`cmd/server/realtime_subscriber.go`）。

worker Wire 仍拉齐 Docker 执行器、全部支付 adapter、MinIO、整套经济。两边都跑项目 schema ensure hook。

**判断**

作业切分方向对。进程边界没有变成模块边界：worker 应只依赖 `app/{functions,payments,assets,subscriptions,billing,storage}` 的端口，而不是整份 `app.ProviderSet` 心智模型。Lynx + Wire 作为装配可留。

---

## 9. 该保留

还债时不要推翻这些——它们已经买到深度或真变异。

### K-1 支付四渠道归一 + 订单状态机 + 三锚点幂等

`PaymentProvider` 四适配器。`Order.Transition` 锁定 `created → paying → paid|failed|closed` / `paid → refunding → refunded`。幂等：建单键、`(provider, provider_event_id)`、履约 ref。履约与订单翻转同一 PG 事务。

### K-2 资产 defs / holdings / ledger；禁止走 `_perms`

D1 拒绝文档层是正确的产品判断。class 矩阵可测。账本 append-only。

### K-3 文档 ACL 纯函数

`AllowsDocumentAccess` 等集中在 `permissions.go`，可单测，不依赖 Postgres。

### K-4 outbox 与写同 COMMIT

文档写与经济事件共用管道。worker XADD → server Stream → Hub。至少一次投递模型清晰。

### K-5 项目 schema 作为租户物理容器

`tw_<project>` 对应产品身份（删除/导出/账本隔离）。`businessSchema` 禁止 DDL 打到一段式项目 schema。

### K-6 用户 collection 列存

catalog 驱动真列，而不是 JSONB 单表。可查询 BaaS 的正确策略。

### K-7 Console 走 Server API

Console proto 保持极小（auth + admins）。资源页打 Server。没有第三套资源协议。

### K-8 Groups 包装模式

`client.Groups` 包 `server.Groups`。Documents 应抄这个**组合**（一个核心 + Client 策略包装），而不是再克隆。不要抄 Groups 仍返回 `*databases.Document` 的领域形状——Groups 自己仍受 M-2，系统表化后才是一等资源。

### K-9 启动期 scope/角色表与 proto 对齐

fail-closed、启动 panic。抽象（方法名注册表）该换，纪律该留。

### K-10 `ObjectStore`

S3/MinIO 缝正当。Compose 约束泄漏 MinIO 是小问题，不否定这条缝。

### K-11 `OAuthAuthenticator`

Google / GitHub / WeChat 是真厂商变异。应经注入使用，不要在 app 里 new infra 工厂。

### K-12 订阅状态机

`Subscription.Transition` / 周期计算 / 计划校验在领域实体上。双模（托管 vs 平台扣款）方向对；`HostedBilling` 目前单适配器，可等第二家渠道再升为端口，或先当作 Stripe 细节藏在 adapter。与 `Executor` 同一规则：单适配器先当实现细节，不要一边在 T-1 留一边在 S-1 删。

### K-13 `pkg/ident` charset 与一段式/两段式不相交

`ValidateSchemaResourceID`、`ProjectSchemaName` 与两段式 `SchemaName` 不相交、id 禁 `_` 以防 LIKE 陷阱。无此规则，运维 `LIKE 'tw_shop%'` 或 `DeleteDatabase("_")` 可砍错 schema。

### K-14 `projectschema.Apply`

go:embed SQL、advisory lock、dirty 标志、CreateProject 同 Tx。E-5 的系统表化应继续用这个引擎，不要另起 migrate 故事。

### K-15 `documentSchema` vs `businessSchema` DDL 分叉

`documentSchema` 允许 sentinel → 一段式；`businessSchema` 永不映射一段式。K-5 只说到容器，这条是结构保险。E-5 完成前禁止拆掉。

### K-16 `RefreshRotationStore` 原子旋转与 reuse 检测

`RotateOK` / `Mismatch` / `Missing`。不能折进 NonceStore（S-1）。合并签发栈时必须保留 reuse→删 session 语义（K-22）。

### K-17 HMAC file token + `jwtparser` purpose 域分离

`CreateFileToken`、`PurposeFileToken` / `PurposeAdminJWT` / …。R-5 / T-2 可能把自定义 HTTP 当「非 Agent 表面」扔掉；token 与 key 派生要留。

### K-18 `provider_resource_index`

无项目头的渠道回调 → 定位 project。账本在 `tw_<project>` 之后，空 projectID 扫描是禁令。

### K-19 `billing.HourBucket` / 小时 rollup

48h Redis TTL 与小时桶是小而真的不变式。T-1 已列 `billing/`；还债时仍不要把用量计费当附带删除。

### K-20 `pkg/crud` AIP 列表/分页

AGENTS.md 已要求。D-4 / E-4 升 Query AST 时不要另起一套分页；AIP `page_token` 与文档 DSL 两套并存是现状，目标是收敛而不是第三套。

### K-21 测试适配器

`MemObjectStore`、支付 `fakeProvider`、`fakeDocDB`（Account / cascade / realtime 单测）。在拆神端口之前，S-1 若执行「必须起 PG」会把这些单测全改成集成测。

### K-22 端用户 Session 热路径

会话上限驱逐、rotation key=`project+session`。系统表化后可换存储，不要先删行为。`sessions.secret_hash` 存明文 UUID **不是** keep——是缺陷，**Wave 0 做哈希写入**（见实现计划）；E-5 只换存储、不重做哈希语义。若 Wave 0 未做完就进 E-5，必须在迁移里补同一写入侧闭合。

### K-23 经济表物理位置以数据面方案为准

代码已落在 `tw_<project>` 静态表（`projectschema/migrations/` 支付/资产/订阅）。v3 文稿里「必须 public」已过时。按旧 D1 原文「搬回 public」会撤销 K-5。

---

## 10. 目标架构（判断；验证阶段可改，但作为规划起点）

### T-1 目标模块图

按「深模块 = 小接口后面大量行为」重切，不按 Clean 文件夹。下图是**概念模块**，不是第一天必须切出的 Go 包；第一刀是 E-1 + E-3 的内部缝。

```text
identity/          Actor × Credential × Grant（唯一身份模块）
tenancy/           Project, API Key, 平台 Admin, audit
users/             User 聚合（邮箱唯一、状态、密码、匿名属性）
                   + OAuth identities
sessions/          签发/轮换/吊销（Client 与 Console 共用 TokenService）
groups/            Group + Membership（系统表化后的一等资源；今日仍受 M-2）
documents/         仅用户 collection：schema + CRUD + 可选 ACL
storage/           Bucket/File 元数据（表）+ ObjectStore 端口
functions/         函数聚合 + Executor 端口 + JobQueue
economy/
  payments/        订单状态机 + PaymentProvider（已接近）
  assets/          五动词在 Assets 领域服务上（不放 Holding）
  subscriptions/   周期状态机；履约=调资产
  billing/         HourBucket / 小时 rollup（K-19）
events/            Envelope + Publisher（outbox 是实现，不是注释契约）
realtime/          进程内 Hub（不属于 domain/shared）
```

外部真缝只留：`PaymentProvider`、`ObjectStore`、`OAuthAuthenticator`、`Mailer`/`SMS`、以及**当真有第二实现时**的 `Executor`（与 S-1 同一规则：单适配器先当实现细节）。

Postgres：空 CRUD 仓储可以是模块内部；带锁/幂等契约的经济仓储接口要留（S-2）。测试用本地 PG 或现有 fake（K-21）。Redis：一次性挑战可收成 `NonceStore`；旋转 / 限流 / 上传会话单独留（K-16）。

### T-2 目标身份

```text
Actor      = EndUser | Admin | Service | System
Credential = Password | OTP | Session | AccessJWT | RefreshJWT | APIKey | OneShot | FileToken
Grant      = 文档 ACL 角色  ≠  API scope  ≠  console RBAC
```

`Service` 对应今日 `ActorKind=service`（项目 API key，调用方可以是 CLI / SDK / 脚本 / Agent）。**Agent 不是 Actor 变体**，是 Service 的一种消费者（T-4 overlay）。Cookie 只是运输。三面是三种 Grant 配置。传输层只调一次 `Authenticate`。

### T-3 目标数据面

```text
public                         控制面 + 跨项目脊柱（projects, keys, outbox）
tw_<project>                   系统表（users/sessions/files… 静态 DDL + FK）
                               + 文档 catalog + 账本
tw_<project>_<database>        随 CreateDatabase 建立，只装用户 collection（不是 E-5 的 schema 合并许可；第三层已经买到，见 D-2）
```

| 模块 | 接口 |
|---|---|
| `Catalog` | Get/List Database/Collection/Attribute/Index，不跑 DDL |
| `SchemaApplier` | `Apply(diff)`，与 catalog 同 Tx |
| `Documents` | Get / Mutate **不带 Actor**；`List(ctx, q, acl Filter)` 消费 `DocACL` 派生的过滤器（`_perms` 子查询必须下推 SQL，不能先 List 再 Check） |
| `DocACL` | Check / Grant / Filter，仅用户 collection。身份袋不进 Documents 每个方法 |
| `Users` 等 | `GetByEmail`、`CreateSession`；静态表 + FK |
| `Query` | proto AST → SQL；`ParseAppwrite(string)` 只是 codec |

### T-4 完整 RPC 保留；Agent 工具目录是 overlay

两凭证 = 用户 JWT × 项目 API Key（Console admin 会话是第三种**凭证**，不是第三套资源树）。

1. **Project API**（今日 Server，**201 中的主体**）——完整资源树。Agent、Console、CLI 都打这里。DDL/bulk **不要删**。
2. **User API**（今日 Client）——策略投影，不复制 Document 类型；认证流（Account 35 RPC）留在这里。
3. **Platform API** —— admins / bootstrap，保持极小。
4. **Tool catalog（overlay，不是替换）** —— 高杠杆工具映射到内部 RPC（「约 20 个」是规划量级，不是规格）；`InvokeJSON` 当逃生舱。这是 Agent 投影，不是唯一资源树。是否暴露任何 key 管理类工具由本条显式决定（R-5：`InvokeJSON` 排除 `APIKeysService` 是有意安全设计）。

---

## 11. 建议演化顺序

不必停机重写。判断已表态（附录 C）。本顺序是规划输入，不是承诺。**拒绝**用旧 8 条「Appwrite 遗产」清单当 E-5 完工定义。

### Wave 0 — 独立缺陷（不动模块图）

落点已锁在 `docs/review/first-principles-plan.md`（拒绝 array；`_version` 走 catalog reconcile 而非 projectschema SQL；secret 只闭合字段谎言；订阅走内部建单、公开 API 仍拒）。

| 来源 | 做什么 |
|---|---|
| A-5 | admin refresh 路径加一次 `GetAdmin`（存在性 + 当前角色） |
| A-4 / K-22 | `sessions.secret_hash`：写入 SHA-256 hex；查找仍按 session ID；存量双读 |
| D-5 | `Attribute.Array`：拒绝 `array=true` |
| D-5 | `_version`：去掉写热路径 `ALTER`；存量一次 catalog reconcile |
| R-6 | 内部建单入口；公开 `CreateOrder` 继续拒绝 `PurposeSubscription` |

### Wave 1–3 — 架构

| ID | 步骤 | 依赖 | 不改物理存储即可？ |
|---|---|---|---|
| E-1 | 抽出 `User` / `Session` 聚合。验收：Account / CreateUser 调用 `GetByEmail` / `User.Register`，**不是** DocumentDB 薄包装（只转调 `ListDocuments` 则深度为零）。存储可暂同一张表 | 无 | 是 |
| E-2a | 合并 Documents 为单一 use-case + 策略投影（抄 K-8 **组合**，不抄 Groups 的 Document 领域形状） | 无 | 是 |
| E-2b | 合并 client/server `message Document` | E-2a | **否**（proto breaking，单独版本策略） |
| E-3 | 一个 `Authenticate` + Actor ADT（`EndUser \| Admin \| Service \| System`）；HTTP/Realtime/gRPC 共用；删除 `HasPermission` 大 OR | 弱依赖 E-1 | 是 |
| E-4 | Query AST 进 proto；字符串 DSL 降为 codec | 无 | 是（可双栈） |
| E-5 | **先写独立设计**，再用 `projectschema` 系统表化；删除 sentinel 与 Databases 系统集合守卫 | E-1；未设计不施工 | **否**（数据迁移） |
| E-6 | 拆 DocumentDB 为 Catalog / SchemaApplier / Documents（List 消费 DocACL Filter） | E-5 更干净，可先拆接口 | 接口先、实现后 |
| E-7 | Tool catalog 作为 Agent 表面；201 RPC 留作完整 API | E-2a、E-4 收益更大（叠在错误表面上会重做） | 是 |
| E-8 | 收 Redis 一次性挑战存储、删死端口、gRPC status 退出 domain（不含旋转/限流/经济仓储） | 随时可做 | 是 |

波次：Wave 1 = E-1、E-2a、E-3、E-8（不改物理存储，不合 proto 消息）；Wave 2 = E-4、E-6 先拆接口（不删 sentinel / `businessSchema`）；Wave 3 = E-5（先设计）、E-2b、E-7。

E-1～E-4 / E-2a 可在不改物理存储的情况下降低克隆和身份袋成本。E-5 是一次认真迁移；「脊柱轻一个数量级」是方向，不是验收指标。

**E-5 验收只含系统表化真正退役的东西**（对照数据面草案「与系统表化的衔接」；编号沿用旧清单，便于对照）：

| 旧号 | 退役项 |
|---|---|
| 1 | 系统集合必须是 collection（含历史上的 `default` 假寄居 → 今日 `"_"` sentinel） |
| 7 | Client 文档 API 的认证字段黑名单（系统集合寄居留下的疤） |
| 8 | 系统集合无 `_version`、出站 version=0（两套文档契约） |

其余旧清单条目**不属于 E-5**：

| 旧号 | 条目 | 归属 |
|---|---|---|
| 2 | `type:role` 字符串 ACE / `documentSecurity` | 用户 collection 产品，K-3 要留。E-5 只退役**系统集合**上的 ACE |
| 3 | Appwrite 查询字符串当唯一查询面 | E-4，与表形态无关 |
| 4 | 默认 `read:any` + `keys`/`admin` 全开 | D-9，用户 collection 产品默认 |
| 5 | Staged transactions | 已删除（D-6，内测无兼容）；不是 E-5 范围 |
| 6 | Upsert replace-permissions | 产品语义，不是系统表化 |

相对地，**列存 collection、schema-per-project、catalog 驱动 DDL、outbox 同事务** 不是 Appwrite 遗产，该留（K-4、K-5、K-6）。

---

## 12. 代表路径深度（抽样，供验证对照）

验证阶段可用这四条路径判断「改完是否更深」，而不是只看文件是否移动。

| ID | 路径 | 当前深度 | 证据 | 目标 |
|---|---|---|---|
| V-1 | CreateUser / SignUp | 中、重复 | 两边 `map[string]any` 写 users 文档；权限切片还出现第三份拷贝 | 一个 `User.Register` / `Users.Create` |
| V-2 | CreateDocument | 薄策略 × 2 | Client owner 默认 ACL；Server 不同 grant；都调同一 `docDB.CreateDocument` | 一个 Documents 服务 + Policy |
| V-3 | CreateOrder | **深** | 幂等插入、渠道下单、`Order.Transition` 在事务内 | 保持；闭合订阅 purpose 边缘（R-6） |
| V-4 | CreateExecution | 深编排 + 死方法 | `CreateExecution` 有记录/队列/prune，是生产 RPC 路径；`Functions.Execute` 只被单元测试调用，不是第二条已接线的执行 API | 删掉测试用薄包装，保留一个 Execute 动词 |

---

## 13. 后续怎么用本文

1. **事实**：附录 A / B 已核对；漂移时改本文事实段，不改结论假装成立。
2. **判断**：2026-08-21 owner 已逐项表态（[附录 C](#附录-cowner-逐项表态)）。正文已吸收修正。捆条 S-3、A-8 已分开接受。
3. **规划**：只把已接受的判断纳入演进计划。施工顺序见 §11 与 `docs/review/first-principles-plan.md`：Wave 0 五条独立缺陷 → Wave 1（E-1 / E-2a / E-3 / E-8）→ Wave 2（E-4 / E-6 接口）→ Wave 3（E-5 先设计、E-2b、E-7）。E-5 未出独立设计前不施工。E-1 属 Wave 1，**不等** E-5。
4. **不与 Round 1–3 混单**：那些是代码缺陷修复（`docs/review/README.md`、`round2/`、`round3/`）。本文是架构还债。两本账分开。Wave 0 缺陷可进缺陷账，但 ID 仍用本文前缀，避免与 F/G/H 碰撞。

---

## 附录 A：独立核对记录

日期：2026-08-20。三名只读子代理并行，主代理吸收后改稿。核对范围是事实准确性、分类完整性、过声称与 K 清单缺口，**不是**再次发明设计。

| 代理 | 结论 |
|---|---|
| 事实核对 | 可入库。RPC 201、Account 19 依赖、`ProjectResolver` 无引用、`HasAnyPermission([])` fail-open、admin refresh 不读库、`secret_hash` 明文、`Attribute.Array` DDL 忽略、`GetDatabase` 返回 `*Collection` 等均属实。必须改：Storage 12 RPC（原文 11）；R-6 的「若」改为已确认。 |
| 完整性 / ID | Yes with edits。§13 曾承诺附录但未写；S-1 事实表无 ID；E-1…E-8 总表一行；K-1 标题不一致。已改。 |
| 过声称 / Keep | 不可直接当执行规格。冲击评级偏高、假缝定义过宽（不计测试 fake）、D-1 与已排期系统表化双计、T-4 会被读成砍 RPC。已下调冲击、收窄假缝、补 K-13…K-23、T-4 改为 overlay。 |

主代理吸收后的硬约束（规划时勿删；附录 C 仍有效）：

- 用户文档引擎是产品；拆的是端口，不是引擎。
- 201 RPC 留给 Console/CLI/SDK；工具目录是 overlay。
- 系统表化前不要拆 `ident` / `businessSchema` / `projectschema`。
- `RefreshRotationStore` 不是 nonce。
- 经济表在 `tw_<project>`，不要按过时的 v3「必须 public」搬回去。
- `sessions.secret_hash` 明文是缺陷，不是 keep。
- Actor 变体是 `Service`，不是 `Agent`。
- 不要用旧 8 条 Appwrite 清单当 E-5 验收。

---

## 附录 B：owner 评审核对记录

日期：2026-08-21。针对当前 `main`（基线 `202211d` 之后仅 docs 提交，代码未漂移）做第二轮独立事实抽查（身份授权 / 数据面 / API 表面 / 领域模型四路并行，逐条落到 file:line），随后评审表态：**接受总体判断与 K 清单**（E/T 顺序见附录 C 修正）；以下事实细节已改入正文（均不改变判断方向）：

| 位置 | 修正 |
|---|---|
| R-2 / R-4 / A-3 | `apiKeyScopeRules` 统一为 **120** 条（原文 118 与「约 130」两处互相矛盾）；文件为 `pkg/grpc/interceptor/apikey_scope.go`；`adminRoleMethodRules` 72 条 |
| A-1 | `shared.Principal` 无 `IsSystem` 方法；「系统」判据两套且不一致（`app/assets/assets.go:210` vs `domain/databases/access.go:40`），反为 A-1/A-2 补强证据，已写入正文 |
| A-2 | 曾补 `app/client/transactions.go:37` 同样丢 `PlatformAdmin`（该路径已随 D-6 删除；Client Databases 仍丢） |
| A-5 | client `TokenBundle` 是类型别名（`account.go:127`）非复制 DTO；仅 console `TokenPair` 为逐字段复制 |
| A-6 | Account 方法散落 **13** 个生产文件（原 15） |
| D-4 | 例外：`servergrpc/projects.go` ListProjects **接线** AIP `filter`/`order_by`，但 use-case 不应用（终审收窄；E-4 只当 handler 先例） |
| D-7 | 升级为已确认：public `document_*` 无运行时读路径，删除安全 |
| D-8 | Database/Collection DDL 仅在 Server；两侧分叉限定为文档用例（与 §7 表面地图对齐） |
| M-3 | 补第 5 个进程缓存 `versionAlterTx`（本事务内 `_version` ALTER 标记） |
| M-4 | `NaturalUniquePerOwner`/`RequiresExpiry` 在 `def.go`，`ValidateDefMatrix` 在 `class.go` |
| S-2 | staged-transaction `LockPending` 已随 D-6 删除；payments 实际为 `CloseExpiredInProject` / `GetByIDForUpdate` |
| R-5 | 补 `InvokeJSON` 显式排除 `APIKeysService`（`invoke.go:61`） |
| R-6 | 补 `subscribe.go:228-258` 直接 `orders.Insert` 绕过 `CreateOrder`；验收点已更新 |

排期层修正（判断不变，拆出独立缺陷单，不绑架构史诗；附录 C 扩为 Wave 0 五条）：

1. **admin refresh 不读库**（A-5）：被删/改角色 admin 凭未吊销 refresh token 可无限续签（`app/console/auth.go:75-111`；`firstRole` 空默认 `"admin"`）。refresh 路径加一次 `GetAdmin` 即可闭合，不等 E-3。
2. **`sessions.secret_hash` 明文**（A-4 / K-22）：与上一条同批开单；修复需哈希写入与校验两半同改并处理存量会话。

其余核对项均属实：RPC 201（69+122+10）、Account 19 构造依赖、`ProjectResolver` 零引用、`HasAnyPermission([])` fail-open、`Attribute.Array` DDL 忽略、`GetDatabase` 返回 `*Collection`、public catalog 无读路径、Groups 包装模式、client 丢 `PlatformAdmin`、`ACCESS_AUTHENTICATED→["users"]` 改写、console cookie `CredentialType` 矛盾、realtime 禁 API key、app 生产代码 import infra 清单、worker Wire 拉起全量、经济表落在 `tw_<project>`（K-23）等。逐项判断表态见附录 C。

---

## 附录 C：owner 逐项表态

日期：2026-08-21。针对附录 B 核对后的正文做判断层表态。**没有整条驳回的发现**；修正已吸收进正文。施工只认本表 + §11 波次。

### 总体

| 对象 | 表态 |
|---|---|
| §0 总判 | **接受**。冲突点钉在 User/Session，不是「不该同时做 BaaS+经济+Agent」 |
| K-1…K-23 | **全部保留**。尤其 K-5/K-13/K-14/K-15（系统表化前勿拆）、K-8（组合模式，不是 Groups 领域终点）、K-9、K-16、K-21、K-23 |
| T / E 方向 | **接受**，切口与顺序按正文修正（E-2 拆步、T-2 `Service`、T-3 List 下推 Filter、E-5 先设计） |

### 发现

| ID | 表态 | 备注 |
|---|---|---|
| P-1 | 接受 | |
| P-2 | 接受 | 挂钩级 roadmap 验收已达到；缺口是下一层工具表面 |
| M-1 | 接受 | S-5 只当诊断 |
| M-2 | 接受 | 与 D-1 分语言/形态 |
| M-3 | 接受 | 拆端口不删引擎 |
| M-4 | **修正已入正文** | 五动词进 Assets 领域服务，不放 Holding |
| M-5 | 接受 | 随 E-6 |
| M-6 | 接受 | `AccountsChannel()` 是同构函数 |
| S-1 | 接受 | `ProjectResolver` 进 E-8 |
| S-2 | 接受 | 不可换成无锁假仓储 |
| S-3.1 | 接受 | app→infra、domain 返回 gRPC status |
| S-3.2 | 接受 | 空 CRUD 才是仪式；经济仓储不是 |
| S-4 | **修正已入正文** | 对外 `uow.Run`；实现可暂用 ctx |
| S-5 | 接受为诊断，不立债 | |
| D-1 | **修正已入正文** | 系统表化是方向，尚无独立设计 |
| D-2 | 接受 | `_` 是过渡寻址 |
| D-3 | 接受 | |
| D-4 | 接受 | ListProjects 已是 E-4 先例 |
| D-5 | **修正已入正文** | 拆成 Array / `_version` / 读路径 migrator 三件 |
| D-6 | **已执行** | 整面删除 staged API + 表 + 信封 `transaction_id` |
| D-7 | 接受 | public `document_*` 删除安全 |
| D-8 | 接受 | |
| D-9 | **新增** | 用户 collection `read:any` 是产品默认 |
| A-1 | 接受（最高杠杆） | Actor = EndUser \| Admin \| **Service** \| System |
| A-2 | 接受为设计分裂 | Client 丢 `PlatformAdmin` 不当独立 P0 |
| A-3 | 接受 | 三面三种 Grant |
| A-4 | 接受 | cookie 是运输；`secret_hash` 是缺陷 |
| A-5 | 接受 | refresh 加 `GetAdmin`，不等 E-3 |
| A-6 | 接受 | ChallengeStore 不含旋转/限流/上传会话 |
| A-7 | 接受 | |
| A-8.1 | 接受 | 共享 `Authenticate` |
| A-8.2 | 接受 | System 是第四变体 |
| R-1 | 接受 | 信任边界留；克隆的是 Documents 实现 |
| R-2 | 接受 | 不砍 201 RPC |
| R-3 | 接受 | 薄 handler 可接受 |
| R-4 | 接受 | |
| R-5 | 接受 | key 管理工具由 T-4 显式决定 |
| R-6 | 接受 | Wave 0 缺陷 |
| R-7 | **修正已入正文** | v1 Server-only + file token |
| C-1 | 接受，后波 | |
| V-1…V-3 | 抽样，不单独表态 | |
| V-4 | **修正已入正文** | `Functions.Execute` 是测试用死方法 |

### 规划时不得从本文推出的读法

- 把 201 RPC 砍成约 20 个动词
- 删用户文档引擎
- 系统表化完成前拆 `ident` / `businessSchema` / `projectschema` / sentinel 守卫
- 把 `RefreshRotationStore` 折进 NonceStore
- 按过时 v3 文稿把经济表搬回 `public`
- 用旧 8 条 Appwrite 清单当 E-5 完工定义
- 把 T-2 的 Agent 当成身份种类去改 `ActorKind`
- 把 T-1 概念图当成第一天必须切出的四个 Go 包
- 把 Documents「不带 Principal」读成 List 也不下推授权过滤

施工顺序见 §11 与 `docs/review/first-principles-plan.md`：Wave 0 五条独立缺陷 → Wave 1（E-1 / E-2a / E-3 / E-8）→ Wave 2（E-4 / E-6 接口）→ Wave 3（E-5 先设计、E-2b、E-7）。C-1 后波。

---

## 附录 D：终审记录（2026-08-21）

三名只读子代理（一致性 / 事实抽查 / 可施工性）对 owner 表态后的正文做终审。

| 代理 | 结论 |
|---|---|
| 一致性 | **PASS WITH EDITS**。附录 C 与正文对齐。必须改：T-3「第三层才建」会被读成 E-5 合并 schema（与 D-2 冲突）。 |
| 事实 | **Yes with nits**。Wave 0 五条 file:line 仍准；`apiKeyScopeRules=120`、Account 13 文件、Subscribe 直插等属实。必须改：D-4 把 ListProjects AIP 接线写成「已消费」——handler 传了字段，use-case **不应用** Filter/OrderBy。 |
| 可施工性 | **Ready with planner decisions**。Wave 0 可开工，但 Array / `_version` 落点 / secret 校验对手 / R-6 入口必须在计划里锁默认，不能把 OR 留给每个 PR。 |

已吸入正文：T-3 第三层措辞、D-4 ListProjects 范围、K-22 与 Wave 0 对齐、D-5 两件 Wave 0 的落点、T/E 前缀不再写「可砍」。规划锁定见 `docs/review/first-principles-plan.md`。
