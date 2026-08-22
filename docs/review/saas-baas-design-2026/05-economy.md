# 经济子系统独立设计评审：payments / assets / subscriptions / billing

> 切片：给 **SaaS 产品用的 BaaS** 是否应内建「支付 / 订阅 / 积分资产 / 平台用量计费」。  
> 方法：以代码为真相，忽略设计文档里的既定决策。词汇：module、interface、depth、seam、adapter。  
> 范围：`internal/domain/{payments,assets,subscriptions,billing}`、对应 `internal/app/*`、`internal/infra/payments/*`、`internal/infra/billing`、`proto/{client,server}/v1/{payments,assets,subscriptions,billing}.proto`、`internal/infra/projectschema/migrations/000004–000007`、`db/migrations/000013–000019`。  
> 日期：2026-08-22。只读，不改代码。

---

## 总判断：不该整包进 BaaS 内核

面向「SaaS builder 把 Torchwood 当后端」时，这四包其实是 **两个产品** 被同一棵树养着：

| 受众 | 代码里实际在做什么 | 是否内核 |
|------|-------------------|----------|
| **Torchwood 向 builder 计量** | `billing`：按 *project* 计 `api_calls` / `storage_bytes` / `function_duration_ms`，月账单文档 **不出票、不收款** | **计量点属于内核**（BaaS 必须知道租户用量）。向 builder 收款（价目、税、催收、发票）是 **第二产品 / 后期** |
| **Builder 向其终端用户收款** | `payments` + `subscriptions` + `assets`：Client JWT 下单、IAP、Stripe Checkout、游戏式四类资产 | **第二产品（Commerce / Economy add-on）**。现在却按内核深度在做：四渠道 adapter、订单状态机、资产账本、双模订阅 |

Firebase / Supabase / Appwrite 类 BaaS 的内核是 Auth、数据、文件、函数。端用户收银是 Stripe Billing / RevenueCat；游戏库存是 PlayFab。把后者做成一等模块，会把内核拖成「半成品支付公司 + 半成品游戏后端」，同时 **仍然服务不了正经 SaaS 收银**（见 F8、F9）。

**建议切分**

- **留在内核（浅）**：平台用量 meter（Redis 小时桶 + 按项目查询）。不要和端用户订单/资产放同一上下文。
- **做成第二产品（深，且要换模型）**：Commerce。必须是 *builder 自己的商户号*（Stripe Connect / 按项目凭据），必须有 **服务端定价的 SKU 目录**，订阅以渠道为事实源，资产最多保留「服务端权威的 credit ledger」。
- **不进内核、也不该以当前形状进第二产品**：`instance` / `tradable` / `upgradeable` / FEFO 分桶的游戏库存；平台代收的全局 Stripe/微信/支付宝/Apple 商户；自研 platform 周期扣款。

以下 12 条是对 **现有模块** 的判断，不是「按文档还差哪些 PR」。

---

## 现状（代码里的四个模块）

### Payments：深模块，渠道 adapter 像样

- **领域**：订单状态机 `created → paying → paid|failed|closed`，`paid → refunding → refunded`（`internal/domain/payments/order.go:9-94`）。`PurposeKind` 只有 `topup | item_purchase | subscription`（同文件 `25-32`）。
- **端口**：`PaymentProvider`（CreatePayment / VerifyCallback / Refund）+ `ReceiptVerifier` + `ProviderRegistry` + `Fulfiller`（`provider.go:176-203`，`repo.go:76-83`）。渠道差异收敛在 adapter，这一刀是对的。
- **Adapter**：Stripe Checkout（`mode=payment` 与 `mode=subscription` 两套）、微信 Native、支付宝 precreate、iOS 验票 / ASN V2；凭据来自进程级 `TORCHWOOD_PAYMENTS_*`（`internal/pkg/config/config.proto:198-255`，`internal/infra/payments/provides.go:32-77`）。
- **用例链**：Client `CreateOrder` → 渠道下单 → `POST /v1/payments/callbacks/{provider}` 裸 HTTP 验签（`internal/api/serverhttp/payments_handler.go:37-62`）→ `HandleCallback` 同事务：幂等登记事件、`FOR UPDATE` 翻转、`Fulfiller.Fulfill`、outbox（`internal/app/payments/callback.go:39-145`、`155-241`）。iOS 走 `VerifyReceipt` 复用 `applyPaid`（`receipt.go:19-21`）。
- **Worker**：超时关单（`cmd/worker/payment_closer.go`）。

### Assets：深模块，游戏库存引擎

- **领域服务**（不是 CRUD）：五动词 Grant / Consume / Transfer / Mutate / Expire，跨 Def + Holding + Ledger；幂等键、OCC、FEFO、class 矩阵（`internal/domain/assets/write.go`，`class.go:10-18`、`132-182`）。
- 四类别：`currency | stack | instance | entitlement`。`OwnerType` 列有 `user|group|project`，一期强制 `user`（`class.go:29-36`、`118-129`）。
- 写入口只在 Server API + System principal（`internal/app/assets/authz.go:12-25`）；Client 只有 List defs / holdings / ledger（`proto/client/v1/assets.proto:58-71`）。这条红线是对的。
- 支付履约经 `orderFulfiller` 调 Grant（`internal/app/assets/fulfiller.go:13-40`）。

### Subscriptions：中等深度，双模合同

- 状态机 `trialing → active ⇄ past_due → canceled|expired`（`internal/domain/subscriptions/subscription.go:10-80`）。
- 明确「订阅不是资产，是产生资产的合同」（同文件 `131-132`）：激活/续期调 Grant/Mutate（`internal/app/subscriptions/benefits.go:14-78`）。
- `ModeHosted`：Stripe Billing Checkout，webhook 为事实源（`hosted.go` domain + `internal/app/subscriptions/hosted.go`）。
- `ModePlatform`：worker `RunBillingCycle` 用余额 Consume 或再开一笔一次性 Stripe 单（`internal/app/subscriptions/billing.go`、`subscribe.go:159-207`）。

### Billing：浅模块，平台侧计数器

- 三个硬编码 metric（`internal/domain/billing/billing.go:11-15`）。
- 写入：gRPC interceptor 对带 `project_id` 的 RPC `api_calls+1`（`pkg/grpc/interceptor/usage.go:46-69`）；函数执行写 `function_duration_ms`；worker 扫文件 `storage_bytes` 快照（`internal/app/billing/billing.go:77-112`）。
- 读出：Server `GetUsage` / `ListRollups` / `ListStatements`（`proto/server/v1/billing.proto:61-72`）。**没有 Client 面。Console 没有 billing 路由。**
- 账单是 JSON 明细文档，状态 `draft|final`，「一期不出票、不收款」（domain `118`；migration 注释同义）。

### 它们坐在哪：`tw_<project>` vs `public`

运行时账本 **全部在项目数据面** `tw_<project.id>`：`projectschema` 的 `000004_payments` / `000005_assets` / `000006_subscriptions` / `000007_usage_billing`，仓储一律 `Scoped(..., projectID, table)`（`internal/infra/bun/bunrepo/project_table.go:51-65`）。

Webhook 无项目头，定位靠 **`public.provider_resource_index`**（`db/migrations/000019_provider_resource_index.up.sql:1-8`，`internal/app/payments/callback.go:314-361`）。这是唯一合理的跨租户 seam。

同时 `db/migrations/000013–000017` 仍在 **`public` 建了同名表**，从未 DROP；bun model 注释仍写 `public.payment_orders`（`internal/infra/bun/model/payments.go:10-11`）。代码已不读这些表。双份 DDL，控制面与数据面边界被注释和历史 migrate 说乱。

---

## Findings

### F1. 双受众叠在同一棵经济树上 — 这不是一个 bounded context

**设计问题。** 「谁被计费」在代码里有两个答案，且没有类型把它们分开。

1. **平台 → builder**：`UsageInterceptor` 用 principal 的 `ProjectID` 给 **项目** 加 `api_calls`（`pkg/grpc/interceptor/usage.go:57-63`）。`GetUsage` 的 scope 是项目，不是终端用户（`internal/app/billing/query.go:30-34`、`208-214` in `billing.go` 的 `projectScope`）。被计量的是 *SaaS builder 消耗了多少 Torchwood*。
2. **Builder → 其用户**：`CreateOrder` 要求 Client JWT 的 `UserID`（`internal/app/payments/orders.go:354-360`）。订单/订阅/持有都挂 `user_id`。被收银的是 *SaaS 的终端用户*。

同一套 worker 轮转（关单、到期资产、订阅扣款、用量 rollup）都按 `projects.ListProjects` 扫租户，表面像一个「economy」子系统。实际上 billing 从不调 `Fulfiller`、不写 ledger、不看 `PurposeKind`；payments 也不读 metric。**两个受众、零共享模型。**

包名还互相污染：`internal/app/subscriptions/billing.go` 是向终端用户扣款；`internal/app/billing` 是向 builder 计数。

**内核：** 平台计量留内核，改名/换包，别叫成和 IAP 一家。端用户收银移出内核。现在这样会让 builder 以为「接入 Torchwood 就能向我的用户收订阅费」，而平台自己连价目表都没有。

---

### F2. 四个包，不是一个经济上下文；唯一 seam 是无类型 JSON + Fulfiller hook

**设计问题。** 深度各自为政，没有共享的「钱 / 货 / 合同」模型。

| 包 | 核心类型 | 与邻接的缝 |
|----|----------|------------|
| payments | `Order` + `PurposeKind` + `json.RawMessage Purpose` | `Fulfiller.Fulfill(order)` |
| assets | `Def` / `Holding` / `LedgerEntry` | Grant 的 `DefCode` 字符串 |
| subscriptions | `Plan.Benefits` 快照 + `Subscription` | 再调 Grant/Mutate；hosted 事件复用 `payments.CallbackEvent` |
| billing | `Bucket` / `Rollup` / `Statement` | **无** |

履约把 purpose 当广告牌解析（`internal/app/assets/fulfiller.go:65-90`）：

- `topup` → `{currency_code, amount}`
- `item_purchase` → `{asset_code, quantity}`
- `subscription` 在资产 fulfiller 里直接 `Unimplemented`（`35-36`），再由订阅包的 `compositeFulfiller` 分流（`internal/app/subscriptions/fulfiller.go:13-33`）

没有 SKU 实体、没有「付了 1999 分该给哪一个 Def、给多少」的约束。金额在订单上，数量在 purpose JSON 里，**两边独立、客户端可同时撒谎**（见 F4）。

事件信封也不是一个 domain：`payments`（`order.go:51`）、`economy`（`assets/class.go:84-86`）、`subscriptions`（`subscription.go:58-59`），只是碰巧都推 `accounts.{userId}`。这是三个上下文共用一个 realtime 频道，不是统一经济模型。

**已够深：** `Fulfiller` 作为支付→履约端口（`repo.go:76-83`）形状正确。缺的是端口后面的 **目录（catalog）**，不是再加第四个状态机。

**内核：** 内核不需要这条缝。第二产品里应把 SKU 变成一等类型，订单只引用 SKU id，Fulfiller 不再解析自由 JSON。

---

### F3. 订单状态机是真深度；商户模型是假多租户

**已够深（状态机 / 幂等 / adapter）。** 订单迁移表、三锚点幂等（建单键、`provider_event_id`、履约 `(order_id, purpose_kind)`）、金额/币种校验、验签失败 401 不落行、index miss 503 让渠道重试（`callback.go:48-66`、`173-178`、`handler.go:50-56`）——这是支付模块该有的深度，不是 CRUD。

`PaymentProvider` 作为渠道端口也干净：use-case 只见归一化 `CallbackEvent`，Stripe HMAC、微信 RSA+AES-GCM、支付宝 RSA2、Apple JWS 都在 adapter 内。这是正确的 adapter 边界。

**设计问题：凭据是进程全局的，不是项目的。** `Payments.Stripe.secret_key` 等挂在 `AppConfig` 上（`config.proto:198-255`），Wire 每个进程一个 adapter（`provides.go:32-38`）。所有租户共用 **一个** Stripe 商户、一个微信 mch_id、一个支付宝 app、一个 iOS `bundle_id`。

结果：

- 终端用户付款进入 **Torchwood 的** 结算账户，不是 builder 的。这是平台代收，不是「给 SaaS 用 Stripe」。
- 没有 Stripe Connect、没有 per-project webhook secret、没有按项目轮换证书。
- `provider_resource_index` 的「他项占用 → PermissionDenied」（`domain/payments/index.go:18-19`）是在 **共享商户号** 前提下防止串租户，不能代替 Connect。

对 BaaS-for-SaaS 这是产品级错位：builder 需要的是 *他们的* 商户号打到 *他们的* 银行账户，税务主体是他们。当前 adapter 深度服务的是「Torchwood 自己当收款方」。

**内核：** 不进。第二产品若做，adapter 接口可留，但 `Get(name)` 必须变成 `Get(projectID, name)`，凭据进项目配置。

---

### F4. Client 自定价：没有商品目录的收银

**设计问题（安全 + 模型）。** `CreateOrderCommand` 让终端用户同时提交 `Amount`、`Currency`、`PurposeKind`、自由 `Purpose`（`orders.go:22-32`、`59-80`）。服务端只校验金额为正、币种三字母、幂等键；**不查 Def、不查计划、不校验「付 19.99 只能换 100 金币」**。

履约时 `purpose.amount` 直接变成 Grant 数量（`fulfiller.go:65-77`）。于是一条合法路径是：`amount=1`（1 分）+ `purpose.amount=10^12` 金币。`applyPaid` 只保证 **渠道到账 == 订单.Amount**（`callback.go:175-178`），不保证到账 == 货值。

公开 CreateOrder 还故意拒绝 `purpose_kind=subscription`（`orders.go:64-67`），订阅必须走 Subscribe——说明作者知道用途不能乱开，却对 topup/item 放手。

对比订阅 hosted：计划上有 `stripe_price_id`，Checkout `line_items` 用渠道 Price，金额以 Stripe 为准（`subscribe.go:140-151`，`stripe.go:501-502`）。**一次性支付反而比订阅更裸。**

也没有「商品」RPC：Server 资产面是 Def CRUD + 五动词，不是货架。Client 支付面没有 ListProducts。

**能力缺失：** 服务端定价的 SKU / Price（金额、币种、履约指令）是 SaaS 收银的最小内核；现在完全没有。

**内核：** 若 Commerce 作为第二产品，SKU 是第一张表。当前 CreateOrder 形状不能给 SaaS 用。

---

### F5. Assets 是游戏库存，不是 SaaS seats / credits

**已够深（作为游戏账本）。** 领域服务把不变式收在一处：class 矩阵、Grant 数量/有效期、FEFO 整单失败、`unique_per_owner`、ledger `InsertIfAbsent` 重放、holding OCC、到期与 Expire 同一引擎（`write.go`、`expire.go`、`class.go:184-232`）。Client 无写入口。对账 RPC 能重放流水（`reconcile.go:26-37`）。这不是浅 CRUD。

**设计问题：模型拟合的是 RPG，不是 SaaS。**

| 现模型 | SaaS 实际需要 |
|--------|----------------|
| `instance` 每行一个个体、`bucket_key = id`（`def.go:52-55`，`write.go:116-133`） | 很少需要「编号道具」 |
| `tradable` + 原子 Transfer（`write.go:273-307`） | SaaS 席位不能在用户间自由转让；要的是 admin 调拨 |
| `entitlement` **强制** `expires_at`（`class.go:198-204`） | 「Pro 功能开关」经常是非期限 / 与订阅同期，不是另一套绝对时间 |
| `OwnerType` 有 group/project 但 `NormalizeOwnerType` 拒绝非 user（`class.go:118-129`） | 席位、组织配额、项目级 credit pool 都挂在 org/project 上 |
| FEFO 按到期分桶（`write.go:162` 注释） | API credit 通常 FIFO/不分期，或按自然月一口桶 |
| `KindAdjust` 写在枚举和表 CHECK 里（`class.go:73`，`000005_assets.up.sql` ledger CHECK） | **没有任何动词实现 Adjust**；纠错/退款回收没有引擎 |

用 entitlement + `level=tier` 硬模拟套餐，续期靠订阅去 `Mutate` 到期时间（`benefits.go:44-61`）。ForceCancel / ForceExpire **只改订阅行，不回收持有**（`lifecycle.go:101-112`、`137-179`）。取消「立刻失效」要等 `expires_at` 撞上 `ExpireDue` worker——而 `expires_at` 被设成 `periodEnd`，中途 ForceExpire 在资产侧是空操作。

**内核：** 一等游戏资产系统 **不应** 进 SaaS BaaS 内核。第二产品若只服务 SaaS，应收缩为：一种 credit（bigint + 幂等 Grant/Consume）+ 一种「订阅绑定的功能标记」（随合同走，不独立过期矩阵）。`instance`/`tradable`/`upgradeable` 是另一条产品线。

---

### F6. 订阅双模里，platform「周期扣款」不是 recurring

**现状。** Hosted：`CreateCheckout` `mode=subscription` + `customer.subscription.*` / `invoice.paid|payment_failed` 归一化（`stripe.go:487-525`、`323-353`）。平台不主动扣款，镜像状态 + 发 benefits。对「builder 用自己的 Stripe Billing」这条，方向对（但仍用全局 sk，见 F3）。

Platform：试用/免费直接发货；有 `BillingAssetCode` 则 Consume 余额；否则 `createBillingOrder` 再开一笔 **一次性** Stripe Checkout（`subscribe.go:159-207`、`217-228`）。

**设计问题。** `tryCharge` 在无余额码时：

```128:138:internal/app/subscriptions/billing.go
func (s *Subscriptions) tryCharge(...) (bool, error) {
    // ...
    if sub.BillingAssetCode == "" {
        cycle := strconv.FormatInt(sub.CurrentPeriodEnd.UTC().Unix(), 10)
        _, _, err := s.createBillingOrder(ctx, sub, plan, cycle)
        if err != nil {
            return false, err
        }
        return false, nil
    }
```

创建订单后 **恒返回未扣款** → `billOrPastDue` 立刻 `markPastDue`（`114-126`）。所谓续费是：丢给用户一张新 Checkout，同时合同变 past_due。没有保存 payment method、没有 off-session charge、没有 Stripe Invoice、没有 dunning 重试序列。`grace_days` 只是「past_due 后多久变 expired」（`subscription.go:195-201`），不是催收。

Hosted 与 platform 共用一个状态机，但事实源不同（webhook vs worker）。`CanTransition` 允许同态 no-op（`subscription.go:84-86`），订单状态机不允许——两个模块对「幂等迁移」的定义不一致。

互斥只按 `(user, plan)` 非终态（`subscribe.go:109-117`）。没有 org 级一份订阅、没有 seat quantity、没有 add-on 作为独立 line item（hosted Checkout `quantity` 写死 1，`stripe.go:502`）。

**内核：** 自研扣款循环 **不要** 进内核。第二产品只保留 hosted 镜像（渠道合同为真相），platform 模式删掉或降成「用 credit 兑换周期」而不是再包一层伪 Stripe。

---

### F7. 退款 / 取消与账本断开；履约占位仍能「完成」

**能力缺失。** 退款路径写得很清楚：只翻订单 + 事件，**不回收已发放资产**（`internal/app/payments/refund.go:20-21`；`callback.go:260-261`）。`KindAdjust` 不存在写路径，反向流水只能靠人调 Consume——还没有「按 order_id 把那次 Grant 对冲」的接口。

`ManualFulfill` 把履约行标 `done`，**不调用 `Fulfiller`**（`refund.go:101-173`）。支付异常兜底变成「管理员点完成」，货可以永远不发。

部分退款：`RefundInput.Amount` 可小于全额（`provider.go:159-165`），订单仍进同一个 `refunded` 终态，没有 `amount_refunded` 字段。部分退款与全额在状态机上不可区分。

订阅 ForceCancel 在 hosted 下会 `CancelNow` 渠道（`lifecycle.go:167-170`），本地资产仍留到过期（F5）。

**已够深：** 「付款与发货同事务、失败整单回滚等渠道重推」（`callback.go:155-157`、`223-225`）这把锁是对的。缺的是 **逆向履约** 作为对称端口，例如 `Fulfiller.Reverse(order)`。

**内核：** 没有正向目录就不要做逆向。第二产品里 Reverse 必须和 SKU 一起设计。

---

### F8. 平台 billing 是计数器，不是计费；还住在租户 schema 里

**现状 / 已够深（作为 meter）。** Redis `INCRBY` + 首次 TTL 48h（`internal/infra/billing/redis.go:20-28`、`55-66`）、小时 upsert 幂等、月 `draft→final` 不回退（domain `147-148`）、GetUsage 把当前未落表小时并入（`query.go:29-68`）——计量管道完整且浅，够用。

**设计问题。**

- **没有价格。** Statement.details 是 `{metrics: {api_calls: N}, hours: H}`（`billing.go:88-92`）。没有 unit price、币种、折扣、免费额度。
- **没有收款。** 与 Stripe Customer / Invoice 无任何调用。不能用 F3 那套全局 Stripe 向 builder 收平台费——也没有另一套。
- **没有配额闭环。** 计量不驱动限流；限流是独立的 `Security.RateLimit`。超额继续服务。
- **物理位置错。** 平台对租户的用量落在 `tw_<project>.usage_rollups`（`projectschema/migrations/000007_usage_billing.up.sql:3-12`）。删项目等于删平台账本。rollup worker 必须对每个项目 `Apply` + 进租户 schema。平台计费属于 **控制面 `public`**，不是数据面。
- **计量对象含糊。** interceptor 对所有带 project 的业务 RPC +1，包括 builder 的 Server API **和** 终端用户的 Client API。GetUsage 不能回答「我的用户用了多少」vs「我作为租户用了多少」。Realtime 明确不计量（`usage.go:14-18`）。
- Console **零** billing 页面（`console/src` 无匹配）。Builder 在管理后台看不见自己的用量。

**内核：** **meter 留内核**，表迁到 `public`（或独立 billing schema），按项目聚合即可。价目/发票/扣款不要假装已经在做。现在的 `BillingService` 对外像计费 API，对内是监控计数。

---

### F9. SaaS 收银必备面：Customer / Portal / 税 / 催收 / 发票 / 席位 — 全无

**能力缺失（对照「给 SaaS 用的支付」而非对照内部设计文档）。**

| SaaS 需要 | 代码 |
|-----------|------|
| Stripe Customer 与本地 user 绑定 | 无 Customer 字段；Checkout 不传 `customer` |
| Customer Portal（改卡、发票、取消） | `HostedBilling` 只有 CreateCheckout / CancelAtPeriodEnd / CancelNow（`hosted.go:23-27`） |
| 税（Stripe Tax、VAT） | 无 |
| 发票作为一等资源 | 只把 `invoice.paid` 当订阅事件燃料（`stripe.go:335-345`），不落 invoice 行、无 ListInvoices |
| 催收（dunning、智能重试） | past_due + grace_days 定时过期（F6） |
| 按席位 / 按用量向 **终端用户** 收费 | Checkout quantity=1；billing metric 不计用户 |
| Proration / 改套餐 | 无；UpdatePlan 改金额不影响已快照的 `Subscription.Benefits` |
| 成功/失败回跳到 **builder 的应用** | 见 F10 |

把这些补进当前四包，会把内核做成残缺 Stripe Billing 克隆。正确做法是 **不要克隆**：hosted 模式把用户送到 Stripe，本地只镜像 entitlement。

**内核：** 否。第二产品也优先「镜像渠道」而不是自建 portal。

---

### F10. Checkout 回跳 seam 没接上：一次性单没有 success_url，订阅写死本机

**设计问题（adapter 用法）。** Stripe Checkout Session 的 `success_url` / `cancel_url` 对 `mode=payment` 是必填。Adapter 仅在入参非空时写入（`stripe.go:96-101`）。`CreateOrder` **从不设** `CreatePaymentInput.SuccessURL`（`orders.go:111-119` 整段无该字段）。对真实 Stripe，一次性下单会失败；单测能过是因为 mock HTTP。

订阅 hosted 自己拼回跳（`subscribe.go:209-214`）：

```209:214:internal/app/subscriptions/subscribe.go
func (s *Subscriptions) checkoutURLs() (success, cancel string) {
    base := "https://localhost"
    if s.cfg != nil && s.cfg.GetServer().GetHttp().GetPublicUrl() != "" {
        base = strings.TrimRight(s.cfg.GetServer().GetHttp().GetPublicUrl(), "/")
    }
    return base + "/?checkout=success&session_id={CHECKOUT_SESSION_ID}", base + "/?checkout=cancel"
}
```

这是 **Torchwood 的 public_url**，不是 SaaS 产品的前端。Adapter 在 SuccessURL 为空时还有 `https://example.com/success` 兜底（`stripe.go:503-510`）。收银台展示名是 `"Torchwood order " + id` / `"Torchwood subscription " + id`（`orders.go:166-168`，`subscribe.go:270`）。白牌失败。

微信/支付宝走 QR/预下单，不依赖 success_url，所以三渠道端口被「最小公约数」削成没有 return URL。Stripe 的必填项变成可选，seam 泄漏到运行时。

**内核：** 若做 Commerce，Checkout 入参必须有 builder 配置的 return URL；描述文案来自 SKU 名。现在这不是能给别人产品用的支付模块。

---

### F11. 表在租户数据面、索引在 public、历史表在 public — 三份物理故事

**设计问题（seam / 部署）。**

- **正确的那份：** 订单/资产/订阅/用量 → `tw_<project>`；无项目头的渠道引用 → `public.provider_resource_index`。Webhook 先 Lookup 再进项目 schema（`callback.go:314-315` 注释）。
- **错误的那份：** golang-migrate `000013_payments` / `000015_assets` / `000016_usage_billing` / `000017_subscriptions` 在 **public** 建全套表，带 `REFERENCES projects(id)`。无后续 migration DROP。新项目 `projectschema.Apply` 再在 `tw_*` 建一遍。
- **注释的那份：** bun model 与部分 repo 文件头仍写 `public.payment_orders`（`model/payments.go:10`，`payments_repo.go:19`），与 `Scoped` 实现相反。

`payment_callback_events` 的去重 UNIQUE `(provider, provider_event_id)` 现在是 **每个项目 schema 一份**。路由正确时等价于全局；index miss 期间事件不可落（503），避免了串租户，但也让「平台级回调审计」不存在。

平台用量放租户 schema（F8）与「经济表都是项目账本」被同一迁移策略打包，说明当时把四包当成一个「v3 经济」整体搬下数据面，没有问「这张表的受众是谁」。

**内核：** 平台 meter 应回 public。端用户账本留 `tw_<project>` 可以（租户数据）。public 幽灵表应删。这与「模块是否内核」独立，但是双受众没切开的物理证据。

---

### F12. 深度分配倒置：游戏账本和四渠道很深，SaaS 商品层为零

**设计问题（depth 用错地方）。**

已经很深、维护成本高的：

- 四渠道验签与下单（stripe / wechat / alipay / iosiap 整包）
- 订单状态机 + 三幂等锚点 + provider index
- 资产五动词 + 四类矩阵 + FEFO + 对账
- 订阅双模 + hosted webhook 映射 + platform worker

浅到不存在的（SaaS builder 第一天就要的）：

- SKU / Price 目录（F4）
- 每项目商户凭据（F3）
- Customer Portal / 发票 / 税（F9）
- 组织席位与 credit pool（F5）
- 平台侧价目与收款（F8）
- 退款对冲（F7）

Console 现状符合这个倒置：有订单列表+退款按钮、资产 Def CRUD、计划 CRUD（`console/src/routes/{payments,assets,subscriptions}`），**没有** 商品、没有用量、没有 Connect onboarding。

这不是「再做三个 PR 就齐」的缺口，是 **模块选错了深度方向**。

---

## 流程对照（代码路径，不是设计文档）

1. **CreateOrder → provider → callback → fulfill**  
   Client JWT 自带金额/purpose → `InsertCreatedOrder` + public index → `CreatePayment`（Stripe 无 success_url）→ webhook 验签 → 按 index 进 `tw_<project>` → 状态机 paid → `compositeFulfiller` Grant。货量来自客户端 JSON。
2. **Subscribe → recurring**  
   Hosted：本地合同先落成 trialing，Stripe 周期扣款，webhook 发 benefits。  
   Platform：有余额则 Consume；否则每期开一次性单并立刻 past_due。没有真正的 recurring。
3. **Grant / Consume**  
   仅 admin / API key / System。支付与订阅履约注入 System principal。终端用户不能自己扣自己的币——对防作弊是对的；对「SaaS 用户消耗 included credits」则必须走 builder 的 Server API，没有按请求自动扣的中间件（billing interceptor 扣的是平台 api_calls，不是用户资产）。
4. **Usage rollup / GetUsage**  
   **被计费的是 SaaS builder 的 project**，不是其终端用户。且只计数、不收费。

---

## 是否应在 BaaS 内核里（按模块）

| 模块 | 深度 | 当前形状 | 内核？ |
|------|------|----------|--------|
| **billing（meter）** | 浅，够 | 平台项目计数器，误放数据面、误与支付同树 | **是（只留计量）**。迁 public，改名 usage。收款另议 |
| **billing（向 builder 出票收款）** | 不存在 | — | **第二产品** |
| **payments** | 深 | 全局商户 + 客户端定价 + 游戏 IAP 渠道 | **否。** 第二产品需 Connect + SKU，微信/支付宝/IAP 按市场再加 |
| **assets** | 深 | 游戏四类库存 | **否（整包）。** 内核最多一个极浅的 server-authoritative credit ledger；当前包是第二条产品线 |
| **subscriptions** | 中 | 双模；platform 伪周期 | **否（自研扣款）。** Hosted 镜像可随 Commerce 第二产品；不要两个事实源 |

**一句话：** 现在的「v3 经济」是一个做得过深的游戏内购后端，外加一个名不副实的平台账单查询，外加一个不能给 SaaS 用的 Stripe 薄封装。作为 **BaaS for SaaS** 的内核，应拆掉；作为 **独立 Commerce 产品**，应先换商户模型与商品层，而不是继续加渠道和动词。
