# 06 模块架构 / 缝 / domain vs app vs infra

> 独立设计审查。代码为真，忽略已文档化决策。
> 产品：面向 SaaS 产品的 BaaS。按模块（而非 Clean Architecture 文件夹）评判。
> 词汇：**module, interface, depth, seam, adapter, leverage, locality**。
> 日期：2026-08-22。只读，不改代码。

**读法**

- Deep = 小 interface 后面大量行为；删除该模块会失去不可替代的不变量。
- Shallow = 透传；删除后调用方可直接打下一层，行为几乎不变。
- 一个 adapter = 假想缝；两个及以上 adapter = 真缝。
- `internal/{domain,app,infra}` 分层本身不构成 depth。

---

## 1. 现状模块图（从代码画出，不是文档抄的）

实际图不是「Auth / Documents / Billing」垂直切片，而是 **进程 × 分层桶 × 能力散点**：

```
cmd/server.ProviderSet                    cmd/worker.ProviderSet
  ├─ api.ProviderSet（gRPC/HTTP 表面）      ├─ 手挑 app.{functions,payments,
  ├─ app.ProviderSet  ─────────┐            │     assets,subscriptions,billing,storage}
  ├─ infra.ProviderSet ───────┤            ├─ 手挑 bunrepo.* / events / payments /
  └─ domain.ProviderSet（空）  │            │     functions.Docker / queue / storage
                               │            └─ 刻意不拉 Account / gRPC / documentdb
                               ▼
        ┌──────────── 分层桶（按目录，不按能力）────────────┐
        │ api/{clientgrpc,servergrpc,consolegrpc,serverhttp,realtime}
        │ app/{client,server,console,documents,storage,functions,
        │      payments,assets,subscriptions,billing}
        │ domain/{auth,users,groups,projects,databases,storage,
        │         functions,payments,assets,subscriptions,billing,
        │         events,shared,idgen,messaging,audit}          │
        │ infra/{auth,bun/bunrepo,documentdb,clients,payments/*,
        │        storage,functions,events,realtime,messaging,
        │        idgen,billing,queue,projectschema,server}      │
        └───────────────────────────────────────────────────────┘
```

调用链的真实形状（Documents 路径，三层都浅）：

```
clientgrpc.DatabasesService  --proto 映射-->
  app/client.Databases       --principal + sentinel 守卫-->
    app/documents.Documents  --grant / MapDocumentDBError-->
      databases.DocumentDB   --24 方法神端口-->
        documentdb.postgresDocumentDB   （唯一 adapter，~2590 行）
```

经济路径的真实形状（有 depth）：

```
api/*grpc.AssetsService  --薄映射-->
  app/assets.Assets      --鉴权 / gRPC 错映 / 指标-->
    domain/assets.Service  --五动词 + 矩阵 + 幂等 + uow.Run-->
      DefRepo / HoldingRepo / LedgerRepo / EventPublisher
        bunrepo + events.outbox     （Postgres 一条）
```

**不是模块的东西**

| 目录 | 实际角色 |
|---|---|
| `internal/domain/provides.go` | 空 Wire set（`// domain-level services wired here when needed`） |
| `internal/app/provides.go` | 能力构造器的桶 |
| `internal/infra/provides.go` | 全部 adapter Bind 的桶 |
| `internal/domain/shared` | 跨切端口垃圾袋（Queue / EventPublisher / Realtime*）+ Principal |
| `internal/infra/bun/bunrepo` | 所有能力的 Postgres 适配器堆在一个包 |
| `internal/app/client` | Account 神模块 + Databases 浅包装 + Groups 再包一层 `app/server` |

**SaaS-BaaS 该有、代码里实际长出来的能力**（按产品，不按文件夹）：

| 产品能力 | 今天落在 |
|---|---|
| 租户 / 项目 | `domain/projects` + `app/server/projects.go`（直接碰 `clients.Database` / `projectschema`） |
| 终端用户认证 | `app/client.Account`（21 依赖）+ `domain/auth` 一打端口 + `infra/auth` Redis |
| 用户 / 组 | `domain/users`、`domain/groups`；Client Groups 仍返回 `databases.Document` |
| 文档 BaaS 核 | `domain/databases` 端口 + 一个巨大 Postgres adapter |
| 对象存储 | `domain/storage` 端口 + MinIO 一个 adapter；use-case 在 `app/storage` |
| 函数 | `domain/functions` 端口 + Docker 一个 adapter |
| Realtime | `domain/shared` 端口 + `infra/realtime` + `api/realtime` |
| 经济（支付/资产/订阅） | 三个 domain 包，相对最深 |
| 平台计费 | `domain/billing` + Redis counter |
| 传输表面 | Client / Server / Console 三套 proto × 三套 app 包 |

---

## 2. 深模块 / 浅模块（对照四个切片）

### 2.1 深：`domain/assets.Service`（对照 1）

公开面是五动词 + 命令结构体：

```11:13:internal/domain/assets/write.go
// Grant 发放资产（幂等键必填）。
func (s *Service) Grant(ctx context.Context, scope Scope, cmd GrantCommand) (*OpResult, error) {
```

后面是：类别矩阵、FEFO、幂等重放、持有并桶、ledger append-only、`uow.Run` 包住 def/holding/ledger/outbox。仓储接口按聚合切开（`DefRepo` / `HoldingRepo` / `LedgerRepo`），不是一个 24 方法端口。`app/assets` 只做鉴权、指标和错映，写路径几乎是 `a.svc.Grant(...)`。

删除测试：删掉 `Service`，Grant/Consume/Transfer 的不变量无处安放。这是 depth。

同族深模块：`domain/payments.Order.Transition` 状态机（`order.go:73-119`）、`domain/subscriptions` 状态机、`domain/databases` 的 ACL 展开（`permissions.go`）、`domain/users.CanAuthenticate`。这些才是 leverage：一处规则，Client / Server / worker / 回调共用。

### 2.2 浅：gRPC handler + `app/client.Databases`（对照 2）

```19:46:internal/api/clientgrpc/databases.go
type DatabasesService struct {
	clientv1.UnimplementedDatabasesServiceServer
	databases *client.Databases
}
// ...
	doc, err := s.databases.CreateDocument(ctx, req.GetDatabaseId(), req.GetCollectionId(), req.GetDocumentId(), data, perms)
	if err != nil {
		return nil, err
	}
	return mapClientDocument(doc)
```

handler 把 proto 拆开再拼回去，错误原样上抛（use-case 已是 `status.Error`）。下一层：

```17:25:internal/app/client/databases.go
type Databases struct {
	projectRepo projects.Repository
	docDB       databases.DocumentDB
	docs        *documents.Documents
}

func NewDatabases(projectRepo projects.Repository, docDB databases.DocumentDB) *Databases {
	return &Databases{projectRepo: projectRepo, docDB: docDB, docs: documents.New(docDB)}
}
```

`CreateDocument` 做 sentinel 拒绝、系统集合写保护、默认 owner ACE，然后交给 `documents.Documents`，后者再 `docDB.CreateDocument`。`app/server.Databases` 是同一神端口的另一层皮肤（DDL + 特权 grant）。

删除测试：删掉 `client.Databases`，handler 可直接调 `documents.Documents` + 几行 principal。浅。这不是「文档模块」——文档的行为在 `postgresDocumentDB` 里，interface 却薄到只是方法清单。

### 2.3 宽而不深：`app/client.Account`（对照 3）

`Account` 是 21 个构造参数的上帝对象：

```53:75:internal/app/client/account.go
func NewAccount(
	cfg *config.AppConfig,
	projectRepo projects.Repository,
	oauthProviders projects.OAuthProviderRepository,
	sessions domainauth.SessionService,
	otp domainauth.OTPChallengeStore,
	oauthState domainauth.OAuthStateStore,
	tokens domainauth.AccountTokenStore,
	loginThrottle domainauth.LoginThrottle,
	rotation domainauth.RefreshRotationStore,
	idGen domainidgen.Generator,
	mailer messaging.Mailer,
	sms messaging.SMSSender,
	rateLimiter domainauth.RateLimiter,
	roles domainauth.UserRoleResolver,
	mfa domainauth.MFAService,
	mfaChallenges domainauth.MFAChallengeStore,
	oneTimeTokens domainauth.OneTimeTokenStore,
	auditRepo audit.Repository,
	usersRepo users.Repository,
	identities domainauth.IdentityRepository,
	sessionRepo domainauth.SessionRepository,
) *Account {
```

行为散落在 `account.go` / `oauth2.go` / `mfa.go` / `email_otp.go` / `phone_otp.go` / `wechat.go` / … 同一类型上。领域规则（邮箱归一化、密码强度、MFA 钩子、登录时序）和传输（`status.Error(codes.InvalidArgument, …)`，见 `account.go:186`）写在一起。OAuth 适配器不经 Wire，use-case 里现场 `infraauth.NewOAuthAuthenticator(...)`（`oauth2.go:173`）。`domain/auth` 几乎全是端口文件，没有与 `assets.Service` 对等的领域服务把 SignUp/MFA/OAuth 收成小 interface。

有行为，所以不是空壳；但 interface 巨大、locality 差、删除任一个认证协议文件都要先读完 21 依赖图。这是 **wide module**，不是 deep module。

### 2.4 缝对了半步：`pkg/uow` + `infra/clients`（对照 4）

```9:12:pkg/uow/uow.go
// Runner 执行 fn：已在工作单元内则加入，否则开启新单元。
type Runner interface {
	Run(ctx context.Context, fn func(ctx context.Context) error) error
}
```

这是教科书式小 interface。`domain/assets.Service` 依赖它。测试里 `memStore` 实现同一接口并可回滚内存快照（`app/assets/assets_test.go:23-48`）——**两个 adapter，真缝**。

然后 app 把缝撕开：

```62:64:internal/app/assets/assets.go
// NewAssets 构造 use-case 聚合（Wire：*clients.Database 满足 uow.Runner）。
func NewAssets(
	db *clients.Database,
```

`payments` / `subscriptions` / `server.Users` 同样注入 `*clients.Database`。真正的连接传递是 ctx 里的 `bun.Tx`（`infra/clients/tx.go:14-25`）。`RunInNewTx`（两段式建单）只挂在 `Database` 上，不在 `uow.Runner` 上。领域注释写「不得把驱动类型写进端口」，实现仍靠 driver 走私。

---

## 3. Findings

### F1 — Lynx+Wire 组合根是对的**进程缝**，错的**模块缝**

`cmd/server` 把 `api` + `app` + `infra` + 空的 `domain` 四个桶一次注入（`cmd/server/provides.go:30-35`）。`domain.ProviderSet` 为空（`internal/domain/provides.go:4-7`），领域模块没有组合身份。

worker 自己写明了桶包的代价：

```35:37:cmd/worker/provides.go
// ProviderSet 只装配作业端口与其适配器，避免 app/infra 桶包把
// Account / gRPC / documentdb 拉进进程依赖图。
```

然后手抄一份 bunrepo / events / payments Bind。启动钩子 `projectSchemaEnsureHook` 在 server 与 worker **各写一遍**（`cmd/server/provides.go:140-157` 与 `cmd/worker/provides.go:121-138`）。

组合根应该按**进程需要的能力模块**拼图，而不是按 layer 文件夹。server vs worker 分进程是对的 locality；layer `ProviderSet` 是错的 grain。Lynx 生命周期（grpc → gateway → realtime → metrics）是服务缝，不是领域缝。

### F2 — 文件夹分层 ≠ 模块；能力被切成三份还要再按表面切

同一「Groups」能力：

- 不变量 / 端口：`domain/groups`
- Server 用例：`app/server.Groups`（真正的写路径）
- Client 用例：`app/client.Groups` **直接依赖 `*server.Groups`**（`groups.go:16-22`）
- 两个 gRPC 包再映射一遍

Documents 同样：`app/client.Databases` 与 `app/server.Databases` 复制 `ensureCollection` / `RejectExternalDatabaseID` / `documents.New(docDB)`。Auth 则是 Client Account vs Console Auth vs interceptor JWT，三套表面没有一个「Auth 模块」边界。

SaaS-BaaS 的公共模块图应该是垂直切片（见 §4）。今天是 `layer × surface × capability` 三维矩阵，改一处规则要在三个目录找 locality。

### F3 — `domain/` 一半是不变量，一半是端口目录

**带不变量（删了会痛）**

| 包 | 行为 |
|---|---|
| `assets` | 四类别矩阵、五动词、幂等、过期 |
| `payments` | 订单状态机、purpose、渠道归一化类型 |
| `subscriptions` | 双模状态机 |
| `databases` | ACL 展开、系统集合名单、Document 形状 |
| `users` | 邮箱归一化、状态、匿名标签 |
| `groups` | membership 状态/角色校验 |
| `events` | 信封 / ACL 快照 / ClientPayload |
| `projects` | OAuth 回调白名单 |
| `billing` | 小时/月 bucket、metric 名单 |
| `shared` | ActorKind / Principal（其余是端口袋） |

**几乎只是端口（interface 文件 + DTO）**

`idgen`、`messaging`、`audit`、`functions`（repo+executor，无状态机）、`storage`（注释还写着 metadata 在动态文档库，`object.go:9`）、`auth`（十几个 `*Store` / `*Service` 接口，规则在 app）。

端口目录不是模块。它们把 adapter 可替换性假装成 depth。多数只有一个实现（见 F7）。

### F4 — `DocumentDB` 是 24 方法神端口；唯一 adapter 把 BaaS 核做成假想缝

`domain/databases/repository.go` 把 Catalog / SchemaApplier / Documents 切开后又嵌回：

```49:54:internal/domain/databases/repository.go
// DocumentDB 嵌入三端口，现有注入点多数不用改签名。
type DocumentDB interface {
	Catalog
	SchemaApplier
	Documents
}
```

合计 24 个方法。实现只有 `postgresDocumentDB`（`documentdb/provides.go` 只 `NewPostgresDocumentDB`）。`postgres.go` 到 2591 行，外加 `postgres_permissions.go`；DDL、OCC、查询编译、`$id` 别名、sentinel schema、outbox、gRPC `status.Error` 全在一个类型里。

一个 adapter = 假想缝。Interface 没有让我们换成 Mongo 或第二引擎；它只强迫每个测试实现 24 个方法，其中 20 个 `return nil`（`api/realtime/handler_test.go:121-184`，`app/documents/documents_test.go` 的 `memDocDB`，`api/clientgrpc/account_test.go:32-200`）。

Account 早已改走 bun `users.Repository`，但 `fakeDocDB` 仍被 `setupClientGRPC` 造出来，测试只用挂在它身上的 `usersRepo` / `sessionRepo`（`account_test.go:281-282, 367-389`）。神端口的编译税活得比用例更久。

文档 BaaS 的 depth 在 Postgres 适配器内部（查询下推、`_perms` SQL、OCC），不在 `DocumentDB` 这个 interface 上。小接口应该是 `Documents`（CRUD）和 `SchemaApplier`（DDL）**分开注入**，而不是再嵌回去图省事。

### F5 — 跨层导入：app 生产代码打穿 infra；传输缝落在 app 而不是 api

domain **不** import infra，也 **不** 返回 gRPC status（`assets/service_test.go:71-94` 有守卫）。这条线是干净的。

app 生产代码打穿 infra（非测试）：

| 文件 | 打穿 |
|---|---|
| `app/client/oauth2.go:173` | `infraauth.NewOAuthAuthenticator`（工厂在用例里） |
| `app/client/email_otp.go:68` / `phone_otp.go:61` | `infraauth.GenerateOTP` |
| `app/client/wechat.go:43` | `infraauth.ExchangeWeChatMiniProgramCode` |
| `app/client/mfa.go:380` | `auth.ParseSessionTime`（infra 的文档时代残留） |
| `app/server/projects.go:13-15,87-101` | `bun/model`、`*clients.Database`、`projectschema.Apply`、裸 `CREATE SCHEMA` |
| `app/assets` / `payments` / `subscriptions` / `server/users` | `*clients.Database` |

同时几乎整个 `internal/app` import `google.golang.org/grpc/status`。`api/*grpc` handler 对错误 `return nil, err` 原样上抛。传输 adapter 的真实位置是 **app**，`internal/api` 只是 proto 编解码。Clean Architecture 文件夹让人以为缝在 api/app 之间；代码里缝在 app/domain 之间，且 app 还反向抓住 infra。

`documentdb/postgres.go` 自己 `status.Error`（38 处，如 `:208`、`:1520`、`:1527`）。adapter 直接说 gRPC 方言。HTTP 回调 / worker 若碰到这些错误，也带着 `codes.InvalidArgument`。

### F6 — 真缝很少；多数 Bind 是单实现仪式

**真缝（≥2 adapter，或明显会有第二条）**

- `payments.PaymentProvider`：Stripe / 微信 / 支付宝 / iOS IAP 四个 adapter，注册表注入（`infra/payments/provides.go:20-28,114-122`）。这是本仓库最干净的模块缝。
- `uow.Runner`：`*clients.Database` + 测试 `memStore`。
- `idgen`：uuid / snowflake / random / seq 策略在一个 Service 内分支，算半个真缝。
- `OAuthAuthenticator`：Google/GitHub/… 多 provider，但工厂从 use-case 调用，没进组合根。

**假想缝（一个 adapter，Wire Bind 仍在）**

Mailer、SMS、ObjectStore（MinIO）、Executor（Docker，`functions/provides.go` 只有 `NewDockerExecutor`）、DocumentDB、几乎全部 bunrepo、全部 Redis `*Store`。

假想缝不是原罪——端口能让 domain 不 import bun。代价是：interface 一旦涨到 20+ 方法，测试必须造神 fake，而你永远不会换掉 Postgres。`FunctionRepo` 把 function/deployment/variable/execution/recover/prune 塞进一个接口（`domain/functions/repo.go:9-35`），测试 `mockRepo` 再实现一遍（`app/functions/mocks_test.go`）。

### F7 — 测试打到深 interface 的，只有经济切片；其余在打神端口的 fake

对比：

- **打得对**：`app/assets/assets_test.go` 的 `memStore` 实现 `DefRepo`+`HoldingRepo`+`LedgerRepo`+`uow.Runner`，直接跑五动词。`domain/payments/order_test.go` 测状态机，零 fake。
- **打得贵**：Documents / Account 集成测试几乎一律 `documentdb.NewPostgresDocumentDB(db, nil)`（全仓库 100+ 处）。单测则复制 24 方法 nop。
- **Account 测试**：`NewTestAccountWithDeps`（`app/client/test_helpers.go:29-72`）亲手 new 一串 Redis/bun 适配器，等于在测试里重写 Wire。21 依赖使「只测 SignUp 不变量」必须先装配 OTP/MFA/OAuth/mailer。

可测试性跟着 interface 的宽度走，不跟着文件夹走。

### F8 — Appwrite 克隆残留是持续的设计税，不是注释

代码里仍在付的税：

1. **sentinel `_`**：`pkg/ident.ProjectDataPlaneID = "_"`（`ident.go:18`）。对外 `RejectExternalDatabaseID`（`app/shared/database_id.go:25-32`），对内 `documentSchema` 映射一段式 `tw_<project>`。每条 DDL/CRUD 都要分岔。这是「系统资源曾经是 default 库里的集合」留下的寻址幽灵。
2. **系统集合 ID 名单**：`users/sessions/identities/groups/memberships/buckets/files`（`domain/databases/system_collections.go:13-21`）。静态表已迁 bun（`projectschema` 000008/000009），名单仍驱动写保护、敏感列、只读测试。`users.CollectionID = "users"`（`user.go:15`）还在。
3. **Teams → Groups 未切完**：Client/Server Groups API 仍返回 `*databases.Document`，再 `groupAsDocument` / `membershipAsDocument` 把 bun 行塞回 `Data map`（`app/server/groups.go:505-527`）。gRPC 再从 map 掏字段（`api/clientgrpc/groups.go:25-30`）。领域已有 `groups.Group`，传输还活在 Appwrite document 宇宙。
4. **`$id` / `$createdAt` 别名**：`postgres.go:2280-2292` `mapQueryField`。查询 DSL 仍讲 Appwrite 方言。
5. **storage 注释说谎**：`domain/storage/object.go:9`「metadata lives in the dynamic document DB」——元数据已是 bun 表。
6. **测试化石**：Account 的 `fakeDocDB` 注释仍写「users/sessions 集合」（`account_test.go:32-33`）。

每一项都在模块边界上打洞：系统表既不是 Document 模块的一部分，又没能离开 Document 模块的寻址/ACL/测试夹具。SaaS-BaaS 的「内建用户/文件」本应是独立模块，现在是文档引擎里的特区。

### F9 — `app/server.Projects` 把租户模块的缝放在了错误的层

建项是控制面最深的用例之一（schema、第一业务库、级联删），却写在 app 里直接操作 driver：

```87:101:internal/app/server/projects.go
	err := s.db.RunInTx(ctx, func(txCtx context.Context) error {
		if err := s.projectRepo.CreateProject(txCtx, p); err != nil {
			return fmt.Errorf("insert project: %w", err)
		}
		schema, err := ident.ProjectSchemaName(p.ID)
		// ...
		if _, err := s.db.Conn(txCtx).ExecContext(txCtx, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s`, quoteIdent(schema))); err != nil {
			return fmt.Errorf("create project schema: %w", err)
		}
		if err := projectschema.Apply(txCtx, s.db, p.ID); err != nil {
			return fmt.Errorf("apply project schema: %w", err)
		}
		if err := s.docDB.CreateDatabase(txCtx, p.ID, firstDBID, firstDBID); err != nil {
			return fmt.Errorf("create first database: %w", err)
		}
```

没有 `Tenancy` 端口（EnsureProject / DropProject）。`projectschema` 是 infra 细节，被 app 和 `documentdb.EnsureCatalog`（`postgres.go:1453-1461`）和 cmd 启动钩子三处调用。租户生命周期没有单一模块、没有小 interface，locality 在 cmd / app / documentdb / projectschema 之间碎掉。

### F10 — 经济切片证明「模块按能力长」是可行的；不要用它为分层文件夹辩护

payments / assets / subscriptions 已经接近垂直切片：

- domain：状态机 + 窄仓储 + `PaymentProvider` / `Fulfiller`
- app：鉴权、worker 扫描、错映
- infra：四渠道 adapter + bunrepo
- 跨模块协作走端口（`SubscriptionCallbackHandler`、`Fulfiller`），不走互相 import 实现

这是 leverage：同一 `Order.Transition` 服务 Client 建单、HTTP 回调、worker 关单。文件夹仍然叫 `domain/payments` + `app/payments` + `infra/payments`，但**能力边界对齐了**。Account 和 Documents 没有对齐。目标不是「把所有包挪进 domain/」，而是让每个产品能力看起来像 payments，而不是像 `app/client`。

---

## 4. 建议的目标模块图（短）

面向「SaaS 产品的 BaaS」，公共模块应按**租户内能力**切，每块自带不变量、窄端口、adapter、use-case；组合根只拼模块，不拼 layer。

```
cmd/server、cmd/worker          进程组合根（Lynx 留在这里）

tenancy     项目 / API key / 数据面 schema / 级联删除
authn       会话、OTP、OAuth、MFA、JWT   ← 拆开今日 Account
identity    users + groups/memberships  ← 不再返回 Document
documents   目录 DDL ∥ 文档 CRUD+ACL+OCC  ← 两个小端口，两个接口
storage     bucket/file 元数据 + ObjectStore
functions   部署/执行（Executor 真缝可后补）
realtime    hub + transport
economy     payments + assets + subscriptions（已接近）
billing     平台用量
audit / messaging / idgen     真正的共享内核（保持小）

pkg/uow, pkg/query, pkg/crud, pkg/ident   跨模块内核，不是业务模块
```

传输（Client/Server/Console proto、gRPC/HTTP）是每个模块的 **adapter**，不是平行的 `app/client` vs `app/server` 世界。`status.Error` 停在传输 adapter。

**不需要做的**

- 不需要再加深 Clean Architecture 文件夹。
- 不需要为 DocumentDB / Mailer / MinIO 假装第二实现。
- 不需要把 `domain/auth` 再拆十个端口文件；需要一个 Authn **服务**小接口，把 SignUp/SignIn/Refresh 的不变量从 Account 神对象里取出。

**值得做的缝**

1. 组合根按上表模块拼，worker 不再手抄桶包。
2. `DocumentDB` 停止嵌入；CRUD 测试只 fake `Documents`。
3. Groups/Users 的公开类型离开 `databases.Document`。
4. `uow.Runner` 成为 app 构造函数的类型，`*clients.Database` 留在组合根。
5. OAuth 工厂进 Authn 模块的端口，离开 `oauth2.go` 的 infra 调用。
6. 系统集合名单 / sentinel `_` 收到 tenancy+identity 内部，Documents 模块不再认识 `users`。
