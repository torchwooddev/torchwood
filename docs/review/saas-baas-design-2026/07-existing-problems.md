# 现有功能问题清单

> 从「SaaS 的 BaaS、先能用」收口：只列**已经实现的能力**里行为不对、接口撒谎、或半截接不上的问题。
>
> **本清单不包含**（高级能力，后做）：Org/环境、出站 webhook、关系/join、跨文档事务、schema 演化、预签名/CDN/配额、函数 HTTP/cron/事件触发、品牌事务邮件、Stripe Connect / 税票 / Portal / 席位。
>
> 证据来自代码审查 `01`–`06`，以源码为准。未改实现。

---

## A. 用了会错（正确性）

现有产品路径上，按文档/SDK 正常调用就会得到错误结果或静默失败。

| ID | 功能 | 现象 | 证据 |
|---|---|---|---|
| A1 | 文档集合默认 ACL | 空 permissions 展开含 `read:any`；Client `List/Get/Count` 为 `ACCESS_PUBLIC`。Server 建行若空 ACE，回落到集合 ACL → **guest 可读**。SaaS 应用数据默认被做成公开读。 | `permissions.go` `DefaultCollectionPermissions`；`proto/client/v1/databases.proto` List/Get/Count；Server `parseOptionalPermissions` 空 → nil |
| A2 | Client 下单 / 资产履约 | 终端用户自带 `Amount` + 自由 `purpose` JSON。渠道只校验「到账 == 订单金额」，Grant 数量取 `purpose.amount`。**付 1 分可发天量资产。** | `app/payments/orders.go` CreateOrder；`app/assets/fulfiller.go` |
| A3 | Stripe 一次性支付 | `CreateOrder` 不设 `SuccessURL`；Stripe Checkout `mode=payment` 必填该字段。真 Stripe 会失败，单测因 mock 过。 | `orders.go` 无 SuccessURL；`infra/payments/stripe` 仅非空才写入 |
| A4 | Platform 订阅续费 | 无余额码时 `createBillingOrder` 后 **恒返回未扣款** → 立刻 `past_due`。所谓续费是再丢一张一次性 Checkout，不是 off-session 扣款。 | `app/subscriptions/billing.go` `tryCharge` |
| A5 | 退款 / 人工履约 | 退款只翻订单，**不回收已 Grant 资产**。`ManualFulfill` 把履约标 `done`，**不调 Fulfiller**（钱完了货可以永远不发）。部分退款与全额共用 `refunded`。 | `app/payments/refund.go`；callback 退款分支 |
| A6 | Realtime 多实例 | Hub 是进程内 map；Subscriber 用 Redis **consumer group**（每条只给一个消费者）。多 server 时 WS 在 A、事件被 B 领走，**A 上的订阅静默**。单机看不出来。 | `infra/realtime/hub.go`；`subscriber.go` XREADGROUP |
| A7 | 异步函数执行 | Redis List BRPOP 取出即离开队列。worker 中途崩溃 → **消息丢失**，启动 1h 扫描只把滞留行标 failed，**不重放**。 | `infra/queue/redis_queue.go`；`cmd/worker/worker.go` |
| A8 | 文件权限 | `CreateFile`/`CompleteUpload` 接收 `Permissions` 但不写入；`Get/List/DeleteFile` 的 `principal` **不参与判定**。bucket 行上 `permissions` JSONB 是死列。调用方以为按 ACL，实际只有 public 布尔 / file token / 有凭证即可。 | `app/storage/storage.go` CreateFile；`uploads.go` CompleteUpload |
| A9 | 文件上传入口 | gRPC Storage 是 API Key/admin；HTTP `POST /v1/storage/...`（无 `/server`）接受**端用户 JWT**，且不走 admin 角色表、不写 audit。正式表面 Server-only，实际端用户能直传。 | `serverhttp/file_handler.go` Register/authorize；对比 Functions HTTP 拒端用户 |

---

## B. 半截功能（有表面，行为空或反的）

表、字段、RPC 已经有了，但产品路径接不上，或行为与名称相反。

| ID | 表面 | 实际 |
|---|---|---|
| B1 | `admin_projects` + member/viewer | 生产 `Grant` 只在 bootstrap Setup。`CreateAdmin` 不绑项目；无邀请/列表 RPC。`owner`/`admin` 不查这张表。非超管 `ListProjects` **恒空**。受限同事账号基本不可用。 |
| B2 | `CreateUserToken`「模拟登录」 | 走与 SignIn 相同的 `CreateSessionAndTokens`，JWT 仍是普通 `end_user`。客服接管与用户登录不可区分。 |
| B3 | 资产 `KindAdjust` | 枚举和表 CHECK 有，**没有任何写路径**。退款对冲只能手调 Consume，且对不上 order_id。 |
| B4 | Billing `GetUsage` / 月账单 | 计数器能跑；Console **无页面**；用量在 `tw_<project>`，**删项目等于删平台账本**。不限流、不收款。对外像计费 API，对内是监控计数。 |
| B5 | public `payment_*` / `asset_*` 等（000013–000017） | 运行时账本在 `tw_<project>`。public 同名表未 DROP，bun 注释仍写 public。部署心智是两套表。 |
| B6 | 文档 List 的 `permissions` | Get 会挂 ACE；List **恒空**。做授权 UI / 批量编辑缺数据。 |
| B7 | Bulk / Upsert 的 `_version` | Update/Delete 强制 OCC；Bulk `SkipVersion`、Upsert 更新支盲写仍 +1。同一集合三种并发语义。 |
| B8 | `DeleteAttribute` | 丢列并删 catalog，**不清理**依赖的 `document_indexes`。 |
| B9 | 订阅 hosted `success_url` | 拼的是 Torchwood `public_url`（缺省 `https://localhost`），不是构建方前端。收银台文案写死 `Torchwood order/subscription`。 |
| B10 | 端用户 session cookie | `SessionService` 每次登录都产出 HMAC cookie；gRPC SignUp/SignIn **丢掉**第三返回值。Realtime **拒绝** end-user cookie。只有 OAuth HTTP 回调会 SetCookie。Web 主路径不是 cookie 会话。 |
| B11 | OpenAPI「API Key 需 X-Torchwood-Project」 | 从 account swagger 抄到几乎每个文件。运行时 key 的项目来自行，**header 改不了**；该头只对 admin 会话生效。 |

---

## C. 同一功能两套语义（能跑，调用方会踩）

不是新能力，是已经暴露的两扇门对不齐。

| ID | 两边 | 差异 |
|---|---|---|
| C1 | 文档空 `permissions` | Client 空 → owner ACE；Server 空 → 无文档 ACE（回落集合，叠 A1 则公开）。 |
| C2 | Client 文档写入门禁 | 拦截器：`ACCESS_AUTHENTICATED` ⇒ 角色 `"users"`，拒 API key。用例 `clientActorOK`：EndUser \| Admin \| Service。直调用例或漏网比 gRPC 更宽。 |
| C3 | Groups | Client 要求 `UserID != ""`（admin 401）；Server 同一 `*server.Groups`，admin/key 可写。 |
| C4 | 查询双栈 | `queries[]string` 无 OR，有 cursor/isNull；proto `Query` 有 OR，无 cursor/isNull。两套同时带谓词 → InvalidArgument。Users 列表又是第三套白名单 DSL。 |
| C5 | 存储 URL | 元数据 RPC：`/v1/server/storage/...`；真上传下载：`/v1/storage/...`。TS SDK 同一方法族跨两棵树。 |
| C6 | 平台用量 interceptor | Client 终端用户 RPC 与 Server 构建方 RPC 都 `api_calls+1`。GetUsage 分不清「我的用户」vs「我这个租户」。Realtime 不计量。 |
| C7 | `"users"` 一词四用 | 端用户角色标签、系统集合名、API Key scope 资源名、Account `permissions: ["users"]`。`ACCESS_AUTHENTICATED` 读起来像「已登录」，实际要这枚角色。 |

---

## 不在本清单（明确后做）

Org/Workspace、Environment、出站 Webhook、文档关系与跨文档事务、collection rename/改类型、对象预签名与 CDN、函数触发器与 KMS、Realtime presence/自定义频道、租户品牌邮件、Stripe Connect / Customer Portal / 税 / 发票 / 席位、服务端 SKU 目录。

Account 上帝对象、DocumentDB 单适配器、Client/Server 文档 RPC 克隆、Principal 字段袋——属于结构税，**现有路径还能用**，不阻塞「先能用」，未列入上表。

---

## 建议的修序（仍只修现有功能）

1. **数据别默默公开**：A1、C1（集合默认私有；Server 空 ACE 不要回落成公开读）。
2. **钱别算错**：A2、A3、A4、A5、B9（下单必须服务端定价或至少金额与履约数量绑定；Stripe URL；退款/人工履约语义；续费不要假 recurring）。
3. **现有实时/异步别丢**：A6、A7（Hub 前一跳改广播；函数队列至少一次或与 outbox 对齐）。
4. **文件别两套谎**：A8、A9、C5（权限要么落地要么从 API 删掉；上传路径与鉴权对齐）。
5. **控制面半截补上或删掉**：B1、B2、B10、B11（member 要就能用，否则不要暴露角色；cookie/文档与运行时一致）。
6. **死表死字段**：B3、B5、B7、B8（Adjust / public 幽灵表 / OCC 旁路 / 删列丢索引）。
