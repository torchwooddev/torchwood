# v3 执行计划

> 对应批准设计：`docs/design/v3-payments-economy.md`（已批准 2026-08-19）  
> 派发稿：`docs/prompts/implement-v3.md`  
> 日期：2026-08-19

第三方 agent 按 PR 顺序实施。PR4 在 PR1 合入后可并行；PR5 基础设施就绪后可并行；其余必须串行。完成后由仓库 owner 做严格审查，不以实施方自报为准。

```
PR0 前置（限流+审计，roadmap §3.4）
  → PR1 支付骨架 ──→ PR2 资产系统 ──→ PR3 订阅 ──→ PR6 Console/SDK
              └────→ PR4 渠道补齐 ──────────────────↗
PR5 用量计费（独立，可最早并行）──────────────────↗
```

## 共同约束（每张 PR）

- 读 `AGENTS.md` 与批准设计对应章节；产品决策不重开（Open Questions 已全部拍板，见设计 Key Decisions D1–D19）。
- 禁止手改 `genproto/**`。改 proto 后 `task generate-proto`；改 Wire 后 `task wire-all`。
- 改 Console 后 `task console-build` 再 `task build`。
- 对话与 commit 用简体中文。
- 每张 PR 结束必须：`go vet ./...`、`go test -short ./...` 绿；触及 SDK 时跑 `go test ./sdk/go/...` 与 `sdk/typescript` 测试。
- 不把未完成的 PR 混进同一提交。
- **金额与数量一律 bigint 最小单位**；任何 float 出现即打回。
- **终端用户无资产写 API**；use-case 层断言 system / admin-key principal（红线 D6）。
- 资产变动 / 订单翻转 / 订阅迁移与 outbox 事件**同一 `sql.Tx`**（复用 v2 EventPublisher 管道，禁止另开连接）。
- 幂等三锚点：客户端幂等键、`(provider, provider_event_id)`、履约 ref，任一环节重试安全。
- 回调验签失败一律 401，不落库、不返回区分性错误。
- gRPC 新方法必须带 `method_auth` 注解并登记 `adminRoleMethodRules`；swagger / `method_auth` 测试必须绿。

## PR0 — 前置（非本设计交付）

roadmap §3.4 的**速率限制**与**审计日志**。规格不在本计划内，但 PR1 合入前必须已合入——收钱前没有限流与审计不可接受。

## PR1 — 支付骨架（Stripe）

**目标**：`payment_orders` / `payment_callback_events` / `payment_fulfillments` 三表（migration 000013 起）；`PaymentProvider` 端口 + Stripe adapter；`POST /v1/payments/callbacks/stripe`；建单/查单 Client + Server API；outbox 事件 `payments.orders.*`。

**不做**：微信/支付宝/iOS（PR4）、订阅（PR3）、资产履约（资产未就位——履约端口与 fulfillments 表结构就位，topup/item_purchase 履约留 hook，PR2 联通）。

**关键验收**

- 验签失败 401，不落任何行；重放（同 `event.id`）幂等 200，不重入状态机。
- 回调 `paid`：订单 `FOR UPDATE` → 状态翻转 → fulfillments 行（空履约记录）→ outbox 事件，同一事务；任一失败整体回滚。
- 建单幂等键重复返回原单，不新建。
- 超时未付关单（worker）：`paying` 超 `expires_at` → `closed`。
- 金额字段全 bigint；`amount` 负数/零拒。

**命令**：`task generate-proto`；`task wire-all`；`go test -short ./...`；回调 handler 单测（伪造签名 body）。

## PR2 — 资产系统

**目标**：`asset_defs` / `asset_holdings` / `asset_ledger_entries` 三表；Grant/Consume/Transfer/Mutate/Expire 五动词；四类别矩阵约束；FEFO 分桶；只读 Client API（ListAssetDefs/ListMyAssets/ListMyAssetLedger）；`economy.assets.*` 事件；对账任务（worker 手动触发）；PR1 的 topup/item_purchase 履约联通。

**不做**：订阅（PR3）、用户间自由转让（D13）、expire 以外的系统自动化。

**关键验收**

- 四类别矩阵：currency 带 `expires_in` 建 def 拒；entitlement Grant 无 `expires_at` 拒；instance 恒 quantity=1；stack 同到期并桶、不同到期拆桶。
- Consume 按 FEFO 顺序扣桶；余额/数量不足整体失败，无部分扣减。
- 幂等键重试：同键返回原结果，ledger 不增行。
- 流水重放 = holdings 快照（对账任务校验，含 `quantity_after` 链路）。
- Transfer 双侧 entry（`transfer_out`/`transfer_in` 共享 ref_id），与任一资产变动同事务。
- 到期：读路径懒过滤 + worker 扫描产 `expire` 流水并删行。
- 终端用户调用写方法 → PermissionDenied（proto 层无 client 写方法 + use-case 断言双保险）。
- `owner_type` 一期恒 `user`（D14），但列存在。

**命令**：`task generate-proto`；`task wire-all`；集成测试（四类别 + FEFO + 幂等 + 对账）。

## PR3 — 订阅

**目标**：`subscription_plans` / `subscriptions` 表；统一状态机；Stripe Billing hosted 镜像（webhook 驱动）；platform 模式 worker 周期扣款（余额 Consume 或生成支付订单）；benefits → 资产 Grant 履约；`subscriptions.*` 事件。

**依赖**：PR1 + PR2。

**关键验收**

- platform 模式扣款失败 → `past_due`（`grace_until` = plan `grace_days` 折算，D16）→ 宽限期内重试 → 超期 `expired`。
- 续期成功与 benefits 履约同一事务；履约失败整单回滚，状态不前进。
- hosted 模式：状态以渠道 webhook 为事实源，平台不主动扣款；webhook 重放幂等。
- Cancel 期末生效（`cancel_at_period_end`），到期转 `canceled`。
- entitlement 续期 = Mutate 延长 `expires_at`，不产生第二条持有。

## PR4 — 渠道补齐

**目标**：微信支付 / 支付宝 adapter（下单 + 回调验签归一化）；iOS `ReceiptVerifier`（VerifyReceipt Client API）+ App Store Server Notifications V2（JWS）接入回调端点。

**依赖**：PR1。**不做**：新状态机、新事件目录（全部复用 PR1）。

**关键验收**

- 四渠道 `VerifyCallback` → 同一 `CallbackEvent` 形状的归一化单测（含伪造签名攻击用例）。
- iOS：`transactionId` 全局唯一防重放；一份 receipt 绑一个 user，跨用户领取拒绝。
- 各渠道回执格式（XML/JSON/纯文本）正确应答，渠道不重复推。

## PR5 — 平台用量计费

**目标**：Redis `INCRBY (project, metric, hour_bucket)` 计量（API 调用 / 存储字节 / 函数执行时长，**不含 Realtime**，D18）；worker 5min 落 `usage_rollups`（幂等 upsert）；月聚合 `billing_statements`；Server API 查询。

**依赖**：无（基础设施就绪即可并行）。**不做**：向 owner 收款、配额/限流挂钩。

**关键验收**

- 同一小时 bucket 重复落表幂等（重跑 worker 不翻倍）。
- statement 明细与 rollups 对账一致。
- worker 挂掉 1 小时后重启，bucket 不丢（Redis 键带 TTL ≥ 48h）。

## PR6 — Console / SDK

**目标**：Console 订单列表/详情、资产 defs 管理与用户资产查询页、订阅计划与订阅列表页；TS/Go SDK 封装 payments/assets/subscriptions（Client 读 + Server 写）；demo 站点最小流程（建单→支付模拟→资产到账→Realtime 收到事件）。

**依赖**：PR1–PR3（PR4/PR5 的 UI 可随后补）。

**关键验收**

- SDK 单测 + contract 测试；Console 组件测试。
- Realtime `accounts.{uid}` 频道（D17）在 demo 端到端可见。
- `task console-build && task build` 绿。

## 完成后交给审查方

实施方提交：分支 / diff、每张 PR 的自测命令与输出、与设计的偏差清单（无则写「无偏差」）。

审查方对照批准设计逐项读源码并亲自跑命令，不以自报为准。**经济系统额外审查点**：所有金额/数量路径无 float；资产写路径无任何终端用户入口；每笔变动可追 ledger entry。
