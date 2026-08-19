# v3 实施派发稿（给第三方 agent）

把下面 **「总则」+ 当前要做的那一张 PR** 整段复制到新 session。一次只做一张 PR，合入（或至少自测绿）后再派下一张。

顺序：**PR0（前置，另行派发）→ PR1 → PR2 → PR3 → PR6**；PR4 在 PR1 后可并行；PR5 独立可最早并行。

规格唯一来源：`docs/design/v3-payments-economy.md`（已批准，Open Questions 全部拍板为 D12–D19）  
执行切片：`docs/design/v3-execution-plan.md`  
仓库约定：`AGENTS.md`、`docs/developer/09-api-guide.md`  
v2 先例：`docs/design/v2-events-realtime-transactions.md`（outbox / Realtime / 事务复用方式）

不要重开产品决策。不要改动 v2 已合入的行为。

---

## 总则（每张 PR 都贴）

你在仓库 `D:\Codes\qiulin\torchwood`（或当前 clone）实施 Torchwood v3 经济系统的**一张** PR。

1. 先读 `AGENTS.md`，再读批准设计里与本 PR 对应的章节（切片见 `docs/design/v3-execution-plan.md`）。
2. Clean Architecture：handler 薄、use-case 编排、端口在 domain、适配器在 infra。
3. 改 proto 后执行 `task generate-proto`，禁止手改 `genproto/**`。
4. 改 Wire provider 后执行 `task wire-all`。
5. 改 Console 后先 `task console-build` 再 `task build`。
6. 对话、commit message、PR 说明用简体中文。
7. 本 PR 范围以外的文件不要改。不要顺手重构。
8. **金额与数量一律 bigint 最小单位**；出现 float 即打回。
9. **终端用户无资产写 API**；use-case 层断言 system / admin-key principal。
10. 资产变动 / 订单翻转 / 订阅迁移与 outbox 事件同一 `sql.Tx`；禁止另开连接。
11. 幂等三锚点：客户端幂等键、`(provider, provider_event_id)`、履约 ref。
12. 做完后在回复里给出：变更文件列表、自测命令与退出码、**与设计的偏差**（无则写「无偏差」）。不要声称「已审查通过」。

完成后停下来，等仓库 owner 审查。不要自行开始下一张 PR，除非派发内容明确包含多张。

---

## Prompt：PR1 支付骨架（Stripe）

```
实施 Torchwood v3 PR1：支付骨架（订单 + PaymentProvider 端口 + Stripe + 回调 + 幂等 + 事件）。PR0（限流/审计）必须已合入。

规格：docs/design/v3-payments-economy.md §1、§5.1、§6
切片：docs/design/v3-execution-plan.md ## PR1
约束：docs/prompts/implement-v3.md 总则 + AGENTS.md

范围（必须做完）：
- migration 000013 起：payment_orders / payment_callback_events / payment_fulfillments。
- internal/domain/payments：PaymentProvider 端口、归一化 CallbackEvent、订单状态机
  （created → paying → paid | failed | closed → refunding → refunded）。
- internal/infra/payments/stripe adapter：CreatePayment（Checkout Session）+ Webhook 验签。
- POST /v1/payments/callbacks/stripe 落 internal/api/serverhttp/，原始 body 直读验签；
  验签失败 401 不落库；渠道回执格式在 adapter 内处理。
- proto：client/v1/payments.proto（CreateOrder 带幂等键 + purpose、GetMyOrder、ListMyOrders）、
  server/v1/payments.proto（ListOrders、GetOrder、Refund、ManualFulfill 占位）；
  method_auth 注解 + adminRoleMethodRules 登记 + scope 规则。
- config.proto 新增 Payments/Stripe 节；secret 走环境变量；task generate-config。
- 回调 paid：订单 FOR UPDATE → 状态翻转 → fulfillments 行（本期履约端口就位、实际发放留 hook，
  PR2 联通 topup/item_purchase）→ outbox 事件 payments.orders.*，同一事务。
- worker：超时未付关单（paying 超 expires_at → closed）。

禁止：微信/支付宝/iOS、订阅、资产表、对 fulfillments 的真实发放。

验收：
- 假签 / 篡改 body → 401 且无落库行
- 同 event.id 重放 → 幂等 200，状态机不重入
- 建单幂等键重复 → 返回原单
- paid 翻转 + fulfillments + outbox 同事务；任一失败整体回滚
- go vet ./... 与 go test -short ./... 绿；swagger / method_auth 测试绿
```

---

## Prompt：PR2 资产系统

```
实施 Torchwood v3 PR2：统一资产系统（代币/物品/权益）。PR1 必须已经在当前分支上。

规格：docs/design/v3-payments-economy.md §2（含四类别矩阵、FEFO、有效期切割）
切片：docs/design/v3-execution-plan.md ## PR2
约束：docs/prompts/implement-v3.md 总则 + AGENTS.md

范围：
- migration：asset_defs / asset_holdings / asset_ledger_entries
  （holdings 含 owner_type 一期恒 user、version；UNIQUE(owner_type, owner_id, def_id, expires_at)；
  ledger append-only 含 idempotency_key UNIQUE、quantity_after、tx_id 预留列）。
- internal/domain/assets：class(currency|stack|instance|entitlement) 与属性合法性矩阵校验。
- 五动词 Grant/Consume/Transfer/Mutate/Expire，全部幂等键必填；
  Transfer 原子暴露（transfer_out/transfer_in 双侧 entry 共享 ref_id）。
- Consume 按 FEFO 扣桶；同到期并桶、不同到期拆桶；数量不足整体失败。
- 每笔变动：SELECT ... FOR UPDATE → 矩阵校验 → INSERT entry → 改/建/删 holding（version+1），同一事务；
  消耗/过期删行不留尸体。
- outbox 事件 economy.assets.*（granted|consumed|transferred|mutated|expired）同事务。
- proto：client/v1/assets.proto 只读三个方法；server/v1/assets.proto Def CRUD + 五动词
  （scope economy.write；method_auth + adminRoleMethodRules 登记）。
- 履约联通：PR1 订单 paid 的 topup/item_purchase 真实发放（同事务）。
- worker：到期扫描产 expire 流水并删行；对账任务（手动触发）校验流水重放 = holdings。
- use-case 层断言资产写仅 system/admin-key（红线 D6）。

禁止：订阅、用户间自由转让（Transfer 仅 Server/Functions）、掉率/配方/升级规则
（升级 = 调用方组合 Consume + Mutate，平台不内置规则）。

验收：
- 四类别矩阵：currency 带 expires_in 建 def 拒；entitlement Grant 无 expires_at 拒；
  instance 恒 quantity=1；stack 分桶/并桶正确
- FEFO 顺序消耗；负余额/超 max_quantity 拒绝且无部分扣减
- 幂等键重试不增 ledger 行
- 对账任务零漂移；人为改 holdings 后对账报漂移
- 履约联通：订单 paid 后资产到账与订单同事务
- go vet ./... 与 go test -short ./... 绿（含四类别 + FEFO + 幂等 + 对账集成测试）
```

---

## Prompt：PR3 订阅

```
实施 Torchwood v3 PR3：订阅（双模统一状态机）。PR1 + PR2 必须已经在当前分支上。

规格：docs/design/v3-payments-economy.md §3
切片：docs/design/v3-execution-plan.md ## PR3
约束：docs/prompts/implement-v3.md 总则 + AGENTS.md

范围：
- migration：subscription_plans（含 grace_days，D16）/ subscriptions。
- 状态机 trialing → active ⇄ past_due → canceled | expired；past_due 带 grace_until。
- hosted 模式：Stripe Billing webhook 驱动镜像迁移，平台不主动扣款；webhook 重放幂等。
- platform 模式：cmd/worker 注册 subscription_biller；到期扫描 → 余额 Consume 或生成支付订单 →
  成功续期 + benefits 履约（调资产系统 Grant/Mutate，同一事务）→ 失败进 past_due + 事件。
- benefits JSONB：grants + entitlements；entitlement 续期 = Mutate 延长 expires_at。
- proto：client/v1/subscriptions.proto（ListPlans/Subscribe/GetMySubscription/Cancel 期末生效）、
  server/v1/subscriptions.proto（Plan CRUD、ListSubscriptions、强制 Cancel/Expire）。
- 事件 subscriptions.*（activated|renewed|past_due|canceled|expired）同事务。

禁止：自建 entitlement 服务、渠道侧未托管的新扣款通道、Messaging 通知。

验收：
- 扣款失败 → past_due → 宽限内重试成功恢复 active / 超期 expired（grace_until = plan.grace_days）
- 续期 + benefits 履约同一事务；履约失败状态不前进
- hosted webhook 重放幂等；Cancel 期末生效
- 进程启动（AssertAdminRoleWriteCoverage 不 panic）；swagger / method_auth 测试绿
```

---

## Prompt：PR4 渠道补齐

```
实施 Torchwood v3 PR4：微信支付 / 支付宝 adapter + iOS IAP 验票。PR1 必须已经在当前分支上。

规格：docs/design/v3-payments-economy.md §1.1–1.4（渠道差异矩阵）
切片：docs/design/v3-execution-plan.md ## PR4
约束：docs/prompts/implement-v3.md 总则 + AGENTS.md

范围：
- internal/infra/payments/wechat、alipay adapter：下单 + 回调验签归一化为 CallbackEvent；
  回执格式（XML/JSON/纯文本）各自正确处理。
- iOS：ReceiptVerifier 端口实现（App Store Server API / verifyReceipt）+
  client/v1/payments.proto 的 VerifyReceipt；ASN V2（JWS）接入回调端点。
- 回调端点泛化为 /v1/payments/callbacks/{provider}。
- config.proto 补 WeChat/Alipay/IosIap 节；secret 走环境变量。

禁止：新状态机、新事件目录（全部复用 PR1）；iOS 退款（渠道不支持，引导找 Apple）。

验收：
- 四渠道 VerifyCallback → 同一 CallbackEvent 形状；伪造签名/证书用例全部 401
- iOS transactionId 全局唯一防重放；receipt 跨用户领取拒绝
- 渠道重推（同流水号）幂等
```

---

## Prompt：PR5 平台用量计费

```
实施 Torchwood v3 PR5：用量计量 + 账单文档。无前置依赖，可并行。

规格：docs/design/v3-payments-economy.md §4
切片：docs/design/v3-execution-plan.md ## PR5
约束：docs/prompts/implement-v3.md 总则 + AGENTS.md

范围：
- 计量点：API 调用（gRPC/gateway 拦截器按 project 计数）、存储字节（复用 SumDocumentField 聚合）、
  函数执行时长（executions.duration）。不计量 Realtime（D18）。
- Redis INCRBY (project, metric, hour_bucket)，键 TTL ≥ 48h。
- worker 每 5min 扫上一完整小时 bucket → usage_rollups 幂等 upsert。
- 月聚合 billing_statements（draft → final，JSONB 明细）。
- proto：server/v1/billing.proto（GetUsage、ListRollups、ListStatements；scope billing.read / console admin）。

禁止：向 owner 收款、发票 PDF、配额/限流挂钩。

验收：
- 同小时 bucket 重跑不翻倍
- statement 明细与 rollups 对账一致
- worker 停机 1 小时重启后 bucket 不丢
```

---

## Prompt：PR6 Console / SDK

```
实施 Torchwood v3 PR6：Console 页面 + TS/Go SDK 封装。PR1–PR3 必须已合入。

规格：docs/design/v3-payments-economy.md §5.2、§6
切片：docs/design/v3-execution-plan.md ## PR6
约束：docs/prompts/implement-v3.md 总则 + AGENTS.md

范围：
- Realtime 频道扩展：parseChannel 改派发表，新增单一 accounts.{userId} 频道（D17），
  校验 = 本人或 platform admin；出站事件无 acl。
- Console：订单列表/详情、资产 defs 管理与用户资产查询、订阅计划与订阅列表（仅 platform admin）。
- sdk/typescript 与 sdk/go：payments / assets / subscriptions 的 Client 读 + Server 写封装。
- demo 站点最小流程：建单 → 支付模拟 → 资产到账 → Realtime accounts.{uid} 收到事件。
- task console-build && task build（Go embed 必须吃到新前端）。

验收：
- SDK 单测 + contract 测试；Console 组件测试
- accounts.{uid} 频道端到端：他人 uid 订阅拒；事件按 domain 分流可见
- task console-build && task build 绿
```

---

## 全部完成后给审查方的回执模板

```
分支：
PR 范围（1/2/3/4/5/6）：
偏差（对照 docs/design/v3-payments-economy.md）：
自测：
- go vet ./...
- go test -short ./...
- （如有）go test ./sdk/go/...
- （如有）sdk/typescript 测试
- （如有）task console-build && task build
红线自查：
- 金额/数量无 float
- 终端用户无资产写入口
- 每笔资产变动可追 ledger entry
未跑的测试与原因：
```
