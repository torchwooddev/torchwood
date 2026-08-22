# 现有功能修复方案

> 对应问题清单：`07-existing-problems.md`。
> 范围：**只修已经实现的能力**。不做 Org/环境/webhook/关系/跨文档事务/presign/函数触发器/品牌邮件/Connect/税票/SKU 目录。
> 内测无向后兼容：默认 ACL、错误语义、死字段可以改，不必 reserved 整段 RPC。
> 对话与当前态文档用简体中文。注释只写 WHY。

施工按六条互不重叠的工作流并行；依赖写在各节。验收以代码与测试为准，不引用第一性原理附录 C。

---

## 总原则

1. **接口不许撒谎**：收了 `permissions` / `principal` 就要用；用不上就从 API/用例删掉，不要留死参数。
2. **默认安全**：未显式公开的数据，guest 与「任意登录用户」都不可读他人行。
3. **钱与数量同源**：履约数量不得由客户端写一个与订单金额无关的字段。
4. **扇出是广播，领取是队列**：Realtime Hub 前一跳必须每副本都收到；作业队列必须至少一次。
5. **半截表/字段**：要么接到产品路径，要么删掉/拒绝，不要留「看起来能用」。

---

## 工作流 1 — 文档 ACL 与写入语义

**问题：** A1、C1、B6、B7、B8、C2  
**主要路径：** `internal/domain/databases/permissions.go`、`internal/app/documents/`、`internal/app/{client,server}/databases.go`、`internal/infra/documentdb/`、相关测试。

### A1 集合默认去掉公开读

`DefaultCollectionPermissions()` 删除 `{Type:"read", Role:"any"}`。

保留：`create/update/delete:users`，以及 keys/admin 的 CRUD。  
**不要**加 `read:users`：否则无文档 ACE 时所有登录用户仍能靠集合回落读到每一行。

`read:any` 仍允许作为**显式**授予（公开集合是开关，不是默认）。`ExpandPermissionRoles` 继续注入合成角色 `any`（匹配显式 `read:any`）。

更新所有依赖「空 permissions = 匿名可读」的测试。

### C1 Server 空文档 ACE 与 Client 对齐

Client `CreateDocument` 空 permissions → `user:<id>` owner ACE，保持。

Server / API Key 空 permissions：

- 调用方有 `UserID`（少见）→ 与 Client 相同写 owner ACE。
- 否则（API key / admin）：写入**空文档 ACE 且不回落成公开读**。实现：对用户集合，空列表不再表示「无 `_perms` 行所以走集合 ACL」，而是写入一条仅 privileged 能匹配的文档 ACE，或在 `AllowsDocumentAccess` 对「显式空 ACE」（`docHasPerms=true` 且 `docPerms` 为空）返回 false（非 System）。推荐后者：Server 空 permissions 创建 `_perms` 占位或把 `docHasPerms` 视为 true 且无 ACE → 覆盖集合、无人匹配 → 仅 `IsSystem`/`PlatformAdmin` 可读。

更直的做法：Server `parseOptionalPermissions` 空切片视为「私有、无 guest/users 读」，调用 `setPermissions` 写零 ACE 行并保证 `docHasPerms=true`（例如集合级不回落）。与 Client owner ACE 一样走文档覆盖语义。

List/Get guest 对上述文档必须 不可见。补测试：默认集合 + Server 建文档 + Guest List = 空；Client 建文档仅 owner 可见。

### B6 List 回传 `permissions`

`ListDocuments` 扫描后对每篇调用已有的 `attachDocumentPermissions`（与 Get 相同）。注意 N+1：能在一次查询里带出 `_perms` 更好，最小实现按篇 attach 也可接受（先正确再聚合）。

### B7 Bulk / Upsert 不再静默跳 OCC

- `BulkUpdateDocuments` / `BulkDeleteDocuments`：**禁止** `SkipVersion`。若当前 RPC 没有 version 字段，Bulk 只允许 System/内部路径 SkipVersion；对外 Bulk 保持 LWW 则必须在 OpenAPI/proto 注释写明，并与单条 Update 的 `version_required` 区分。  
  **本方案选定：** 对外 Bulk 仍是构建方 LWW（API Key/admin），保留 SkipVersion，但 **Client 若未来暴露 Bulk 不得 Skip**。现 Client 无 Bulk。改为：Upsert 更新支若请求带了 `version` 则做 OCC；未带则盲写 +1（保持构建方同步）。单条 Update/Delete 行为不变。
- 从「三种并发语义」收敛为文档注释：单条 Update/Delete 强制 OCC；Bulk 为构建方 LWW；Upsert 更新支可选 OCC。

若时间紧，B7 以注释 + Upsert 可选 OCC 为最小；不要在没有 version 字段的 Bulk RPC 上突然 `version_required`（那会把现有 Server Bulk 全部打爆）。清单原文希望 Bulk 不要静默 LWW——折中写进测试：Bulk SkipVersion 仅 `RequireServerWriteActor` 路径。

### B8 `DeleteAttribute` 同步删依赖索引

同一 `RunInTx`：查出 `document_indexes` 中 `attributes` 含该 key 的索引 → `DROP INDEX` + 删 catalog 行，再 `DROP COLUMN` + 删 attribute catalog。补测试：有索引的列删除后 `document_indexes` 与 PG index 都不在。

### C2 Client 写路径与拦截器对齐

`clientActorOK` 对 **写**（Create/Update/Upsert/Delete）只允许 `ActorKindEndUser`。读路径（含 guest）保持现有 `resolveReadPrincipal`。Admin/API key 写文档走 Server Databases。补测试：Admin principal 直调 `client.Databases.CreateDocument` → Unauthenticated/PermissionDenied。

---

## 工作流 2 — 支付 / 订阅 / 履约

**问题：** A2、A3、A4、A5、B9（B3 不单独做 Adjust）  
**主要路径：** `internal/app/payments/`、`internal/app/assets/fulfiller.go`、`internal/app/subscriptions/`、`internal/infra/payments/stripe/`、`internal/domain/payments/`。

### A2 履约数量与订单金额同源

Client `CreateOrder`：

- `topup`：`purpose.amount` **必须等于** `Order.Amount`，否则 InvalidArgument。履约 Grant 数量用 `Order.Amount`（不再信任单独更大的 purpose.amount）。
- `item_purchase`：**Client 拒绝**（与 subscription 相同理由：无服务端定价目录）。构建方用 Server `Grant` 发货。
- `subscription`：仍拒绝公开 CreateOrder。

`parsePurpose` / fulfiller：topup 以订单金额为准。补测试：amount=1 且 purpose.amount=1e12 → InvalidArgument；amount=100 且 purpose.amount=100 → Grant 100。

### A3 Stripe 一次性 Checkout 必带 URL

`CreateOrder` 调 `CreatePayment` 时 **必须** 填 `SuccessURL` / `CancelURL`。来源：

1. 请求可选 `success_url` / `cancel_url`（proto `optional`，Client CreateOrderRequest）；
2. 否则 `server.http.public_url` + `/?checkout=success|cancel&session_id={CHECKOUT_SESSION_ID}`；
3. 再缺省不得把字段留空（Stripe adapter 空则写入 fallback 也不再用 `example.com` 藏错误——应 fail 或用 public_url）。

微信/支付宝忽略 URL。补 Stripe adapter 测试：空 SuccessURL 时 use-case 仍传入非空。

### B9 订阅回跳可覆盖

`Subscribe` / hosted Checkout：允许请求带 success/cancel URL；未带才用 `public_url`。收银台 `Description` 改为 `order {id}` / `subscription {id}`，去掉写死的 `Torchwood ` 前缀（白牌最小步）。

### A4 Platform 续费不要立刻 past_due

`tryCharge` 在 `createBillingOrder` 成功后：**不要** `return false` 导致 `markPastDue`。应视为「账期等待支付」：保持当前非终态（active/trialing），订单 paying 等回调 `applyPaid` 再发 benefits。回调失败/超时仍由关单 worker + 日后周期扫描处理。

补测试：无 `BillingAssetCode` 时跑一轮 billing cycle → 订阅仍 active（或 trialing），存在 paying 订单，不是 past_due。

### A5 退款回收；人工履约真发货；拒绝部分退款

- `Fulfiller` 增加 `Reverse(ctx, order) error`：按 `fulfill:{orderID}` 幂等键找到 Grant，再 `Consume` 同数量（idempotency `reverse:{orderID}`）。无持有则 vis-à-vis 已过期：记录错误但不阻止订单翻 refunded 若资产已不在（文档写明）。
- `Refund` 在订单翻 refunded 的同一工作单元调 `Reverse`。**部分退款**（`amount != 0 && amount != order.Amount`）→ InvalidArgument（一期只全额）。
- `ManualFulfill` **必须**调 `Fulfiller.Fulfill`，成功才标 done；失败不标完成。

不做 `KindAdjust` 动词（B3）：退款走 Reverse→Consume。CHECK 里的 `adjust` 可留，无写路径。

---

## 工作流 3 — Realtime 扇出

**问题：** A6  
**主要路径：** `internal/infra/realtime/`、`internal/infra/events/`（transport Enqueue）、outbox worker。

### 方案

Outbox → worker 领取仍是 **队列**（SKIP LOCKED，至少一次）。  
Hub 前一跳改为 **广播**：

- `RealtimeTransport.Enqueue`：Redis `PUBLISH` 频道（如 `torchwood:realtime`），payload 仍是完整信封 JSON（含 acl）。
- server `Subscriber`：`SUBSCRIBE` 该频道 → `UnmarshalEnvelope` → `Hub.Dispatch`。不再 `XREADGROUP` / 竞争消费者。
- worker 在 Publish 成功后写 `dispatched_at` / `published_at`（或保持现有两阶段字段语义，但不再 XACK Stream）。
- 产品本就不补历史：副本宕机期间的漏推可接受；**禁止**多副本抢同一条导致在线副本静默。

删掉或停用 consumer group 相关代码（`ensureGroup` / PEL / XAUTOCLAIM）。补测试：两个 Hub + 一次 Enqueue，两边 `Dispatch` 都能见到（可用内存 pubsub fake 或 miniredis）。

若 `Enqueue` 目前是 XADD：改为 Publish；Stream 表/键若无其它消费者可停止写入，避免双轨。

---

## 工作流 4 — 函数执行队列至少一次

**问题：** A7  
**主要路径：** `internal/domain/shared/ports.go` `Queue`、`internal/infra/queue/`、`cmd/worker` 函数消费者、`internal/app/functions/executions.go`。

### 方案

`Queue` 增加可靠投递（只此一个生产队列，改端口可以）：

```
Enqueue(ctx, queue, payload) error
Dequeue(ctx, queue, timeout) (payload []byte, ack Token, err error) // 无任务 payload=nil
Ack(ctx, queue, ack Token) error
```

Redis 实现用 **Stream**（XADD / XREADGROUP / XACK）或 **BRPOPLPUSH** 到 inflight list + Ack 时 LREM。推荐 Stream，与 outbox 同哲学。

- 成功 `ProcessExecution` 后 Ack。
- 失败可 Nack/不 Ack，PEL 或 inflight 超时后重投。
- worker 崩溃：消息仍在 PEL/inflight，重启后认领并重试执行（注意执行器要能承受至少一次：依赖 execution_id 幂等更新行）。

启动时「running >1h → failed」保留作卡死兜底，不要代替队列重投。

`go test` 用 miniredis 测：Enqueue 后不 Ack，新消费者仍能读到。

---

## 工作流 5 — 存储权限与入口

**问题：** A8、A9、C5  
**主要路径：** `internal/app/storage/`、`internal/api/serverhttp/file_handler.go`、`sdk/typescript/src/server/storage.ts`、proto storage 若要改路径。

### A8 权限落地（最小）

文件行已有 `owner_user_id`。 bun 模型**不要**假装 `_perms`。

读/删/改：

- `bucket.Public` → 允许（现有）；
- API key / admin（`RequireServerWriteActor` 或 platform admin）→ 允许；
- EndUser → 仅 `file.OwnerUserID == principal.UserID`（或上传者）。

`CreateFile` / `CompleteUpload`：从 principal 填 `OwnerUserID`（EndUser）；丢弃请求里未落地的 `Permissions` 切片——**从 command 和公开 proto/SDK 字段删除**（内测可删字段；若字段号要留则 reserved）。bucket 行 `permissions` JSONB 若无读路径：读路径忽略该列，注释标明未使用，Console 不要展示成 ACL。

`GetFile`/`ListFiles`/`DeleteFile` 的 `principal` **必须参与判定**。List 对 EndUser 只返回自己的或 public bucket 内文件。

### A9 / C5 入口对齐

HTTP `FileHandler.authorize`：

- EndUser：允许 upload/download/view（受 A8 所有者/public 约束）；
- API key / admin：与 gRPC Storage 相同（scope + 项目绑定 + admin 角色写方法）；
- 写操作走与 gRPC 一致的审计（至少 slog 带 actor；能调 `audit.Insert` 则插入）。

路径：保留 `/v1/storage/...` 作为**端用户直传**（与 A9「端用户 JWT 合法」对齐，但不再越权读别人文件）。元数据 CRUD 仍 `/v1/server/storage/...`。TS Server SDK 的 `uploadFile` 若用 API key，应打 **server 前缀或现有路径但 auth=apiKey**；文档写清两条入口。

最小文档：`docs/developer/07-storage.md` 写明 Client 直传 vs Server 管理。

Functions HTTP 继续拒 EndUser。

---

## 工作流 6 — 控制面半截与文档撒谎

**问题：** B1、B2、B5、B10、B11  
**主要路径：** `internal/app/server/projects.go`、`internal/app/console/admins.go`、`internal/domain/projects/admin_project.go`、`internal/api/clientgrpc/account.go`、`internal/api/realtime/handler.go`、`proto/**` swagger 文案、`db/migrations/`。

### B1 member/viewer 能进项目

- `AdminProjectRepository` 增加 `ListProjectIDs(ctx, adminID)`（或 List）。
- `ListProjects`：平台 admin 仍全表；否则返回 `admin_projects` 里的项目（不要空数组）。
- `CreateAdmin`：若当前 principal 带 `ProjectID`（Console header），对 member/viewer **Grant** 该项目；owner/admin 仍全局，可不写行。
- 可选最小 Console：不新做邀请 RPC。能用 = 超管在项目上下文创建的 member 能 List/Get 该项目。

补测试：member + Grant 后 ListProjects 非空；无 Grant 的 member List 为空且 Get 其它项目 NotFound。

### B2 模拟登录可区分

`CreateUserToken` 仍发可访问数据的 end_user JWT（否则客服无法操作），但 claims 增加 `imp`（impersonator admin id）或等价字段；session `provider=server_token` 保持。Validator 不必改 ActorKind。审计日志已有 slog，JWT 侧可查 `imp`。SDK 注释删掉「Agent 凭证」说法。

若 `pkg/jwtparser` Claims 加字段：所有签发点默认空。

### B5 DROP public 幽灵经济表

新 migration（接 000021 之后）：`DROP TABLE IF EXISTS` public 上由 000013–000017 创建、运行时已迁到 `tw_<project>` 的表（orders/assets/subscriptions/usage 等，**不要** drop `provider_resource_index`、outbox）。先 grep bun `ModelTableExpr` / 裸 SQL 确认无 public 读路径。down.sql 可不重建（内测）或从原 up 复制。

### B10 端用户 cookie

- gRPC Account SignUp/SignIn/OAuth token 成功路径：用与 Console 相同机制把 HMAC session cookie 写入 `Set-Cookie`（gateway metadata）。Cookie 名保持 `TORCHWOOD_session_<project>`。
- Realtime 握手：**接受**该 cookie（与 JWT 二选一），继续拒 API key。

补测试：SignIn 响应带 Set-Cookie；Realtime 用该 cookie 能 hello。

### B11 OpenAPI 文案

所有 proto 里 API Key 描述改为：项目绑定在密钥行上，**不需要** `X-Torchwood-Project`。该头仅 Console admin 会话切换项目。改完 `task generate-proto`。

---

## 明确不修（本轮）

| ID | 原因 |
|---|---|
| B3 KindAdjust 动词 | 退款走 Reverse/Consume |
| B4 Billing 当收款 / Console 账单页 | 计数器能用即留；不当计费产品做 |
| B7 对外 Bulk 强制 version | 无字段会打爆 Server Bulk；见工作流 1 折中 |
| C3 Groups Client 要 UserID | 产品如此：组是端用户社交图 |
| C4 查询双栈合并 | 能用；属结构税 |
| C6/C7 用量与 users 一词 | 结构税 |
| Account 拆分 / DocumentDB 拆适配器 / 文档 RPC 去克隆 | 结构税，不挡能用 |

---

## 并行与验收

| 工作流 | 可并行 | 验收要点 |
|---|---|---|
| 1 文档 ACL | 是 | 默认集合 guest List 空；owner 可见自己的文档；List 带 permissions；删带索引的属性 |
| 2 支付 | 是 | 1 分不能 Grant 天量；Stripe 有 success_url；续费不立刻 past_due；退款 Consume；ManualFulfill 真 Grant |
| 3 Realtime | 是 | 两个订阅端同收一条 Publish |
| 4 函数队列 | 是 | 不 Ack 则重启后仍能 Dequeue |
| 5 存储 | 是 | EndUser 读不到别人的文件；public bucket 仍匿名可读 |
| 6 控制面 | 是 | member List 到已 Grant 项目；swagger 文案；SignIn Set-Cookie |

禁止手改 `genproto/**`。改 proto 后 `task generate-proto`；改 Wire 后 `task wire-all`。`gofmt`。单测 `go test -short` 触及包。

提交信息用简体中文，按工作流分 commit（每个工作流至少一票）。
