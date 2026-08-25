# Torchwood Round 5 审查报告（含交叉验证）

> 日期：2026-08-25 ｜ 基线：`main` @ `904b4e6`
> 方法：主代理复核架构骨架与全部 P1 发现 + 5 个盲区子代理（R4 回归验证 / 并发 / 数据库性能 / 经济系统 / Functions+Storage）+ 2 个对抗式评审代理交叉验证（A=证明成立，B=找反例）。
> 级别：P0=安全漏洞/资损/数据丢失；P1=正确性缺陷/事故级/发布失信；P2=可靠性隐患/性能/纵深缺失；P3=改进建议。
> 构建：`go build ./...` ✅ `go vet ./...` ✅；集成环境（PG/Redis/MinIO）可达。

---

## 一、R4 修复回归验证结论

子代理抽样核验 50+ 项（J1–J7 全部 P1 + wrapup 文档 5 项 + 904b4e6 复审修复），**全部真实落地，无虚报、无回退**。904b4e6 的 HMAC 派生修正与 `pkg/jwtparser/keys.go:23-27` 确认同构。历史回归唯一的系统性发现集中在文档侧（见 P2-DOC 组）。

## 二、交叉验证矩阵

| # | 问题 | 初判 | 评审 A（证明成立） | 评审 B（找反例） | 最终定级 |
|---|------|------|--------------------|--------------------|----------|
| 1 | iOS legacy 验票金额脱钩 | P1 | 成立：四环节+状态机 `created→paid` 合法迁移全走通；唯二防线（MaxQuantity 默认 nil、txn 绑定需新 receipt）不构成拦截 | 维持 P1：反例全部失败；JWS 路径有金额，攻击面限 legacy（需 SharedSecret） | **P1** |
| 2 | 退款回调零金额校验（部分退款→全额回收+终态） | P1 | 成立：三家 adapter 部分退款 Amount=退款额，applyRefunded 无比对 | 降级：路径真实但需商户在渠道后台手工触发，非外部可利用 | **P2**（仍修） |
| 3 | 分片上传 complete 锁用请求 ctx 释放 | P1 | 成立：TimeoutHandler 60s 覆盖该路由，156GB/10000 片必超 | 维持且加重：超时落在删分片中段时大文件**永久无法 complete**（重试因缺片被拒） | **P1** |
| 4 | 动态 collection 默认排序无索引 | P1 | 成立：全 DDL 路径仅 PK，reconcile 只管 _version | 维持（P1/P2 边界，修复零风险高收益） | **P1** |
| 5 | offset 模式每页全量 COUNT | P1 | 成立：默认分页即 offset 链 | 降级：proto `total_count ≤0=unknown` 合法，性能项非合同违背 | **P2**（仍修） |
| 6 | Bulk Update/Delete 事务内逐条 N+1（≤1000） | P1 | 成立：~8 条/次 ≈ 8000 语句单事务 | 降级：原子性有测试承诺，无正确性问题；批量化不宜热修 | **P2**（本轮不修，记后续） |
| 7 | CHANGELOG v0.1.0 与 tag 事实矛盾 | P1 | 成立：tag 实测仍含 replace+伪版本 | 维持：无 v0.1.1，release.yml 未重跑，下游 `go get` 必败 | **P1** |
| 8 | 订阅续费订单幂等命中不查状态→永久卡死 | P2 | 成立：完整空转链核验 | 维持 P2：死锁链完整，仅人工 ForceCancel 可解 | **P2** |
| 9 | hosted 终态订阅 webhook 非法迁移→事务回滚→重推 | P2 | 成立：事件登记与处理同事务 | 维持 P2 但改措辞：Stripe 重推 ≤3 天，实质是**事件最终丢失**+已付款订阅卡 trialing | **P2** |
| 10 | 资产幂等键跨动词碰撞 | P2 | 成立：loadReplay 不校验 Kind | **推翻**：与 Stripe 全局幂等键空间语义一致，proto 未承诺分域，内部键全带前缀 | **P3**（顺手加 Kind 校验） |
| 11 | 并发 Subscribe 双开订阅 | P2 | 成立：READ COMMITTED 下检查-then-insert 无约束兜底 | 维持 P2：窗口不小（事务内含 CreateCheckout HTTP） | **P2** |
| 12 | topup purpose float 截断→paid 永久回滚 | P2 | 成立：structpb→float64→int64 截断链逐环验证 | 维持（用户已付款订单终态 closed） | **P2** |
| 13 | docker 清理无超时 Background | P2 | 成立 | 降级 P3（daemon 挂起时子系统整体已不可用） | **P3**（顺手修） |
| 14 | realtime dedup 上限失效 | P2 | 成立 | 降级 P3（窗口回落自愈、重启清零） | **P3**（顺手修） |
| 15 | versionAlterTx 只增不删 | P2 | 成立：全仓无 Delete | — | **P3** |

## 三、最终问题清单（交叉验证后）

### P1（4 项，全部双代理或主代理确证）

1. **E-P1-1 iOS legacy 验票金额脱钩，可任意放大充值**
   `internal/app/payments/orders.go:66-77`（客户端自建 ios_iap+topup 单、金额自声明、无上限）→ `internal/infra/payments/iosiap/iosiap.go:298-305`（legacy VerifiedPurchase.Amount 恒 0）→ `internal/app/payments/callback.go:175`（`event.Amount > 0 &&` 前置放行 0 值，Currency 同为空连币种防线一并失效）→ `internal/app/assets/fulfiller.go:89-91`（topup 履约 qty=order.Amount）；`internal/app/payments/receipt.go` 全程无 ProductID 与订单 purpose 绑定校验。状态机 `internal/domain/payments/order.go:74-80` 显式允许 `created→paid`。
   **平行入口（评审 A 新发现）**：ASN V2 回调 `tx.Price=0`（免费商品/100% 优惠）时 `milliunitsToMinor` 返回 0，同样跳过校验。
   修复方向：applyPaid 对「渠道未提供金额（Amount==0）且订单金额 >0」fail-closed 拒绝；根治需服务端 productID→价目映射（记后续）。

2. **E-P1-2 分片上传 complete 互斥锁用请求 ctx 释放，锁泄漏 1h；大文件永久 complete 失败**
   `internal/app/storage/uploads.go:189,317` defer UnlockComplete(ctx)；`internal/runtime/grpc_gateway.go:101-110` default 分支 60s TimeoutHandler；`internal/infra/storage/redis_upload_session.go:187-190` DEL 尊重 ctx；`completeLockTTL=1h`。156GB/10000 片的 Compose+逐片删除必然超 60s → ctx 取消 → DEL 失败被 `_ =` 吞 → 锁残留 1h；且超时点落在删分片中段时，重试 complete 因缺片被拒——**大文件在此架构下永远无法完成**。孤儿分片 48h cleanup（`internal/app/storage/cleanup.go`）本可兜底。
   修复方向：UnlockComplete 用 `WithoutCancel+2s`（样板 `pkg/semaphore/semaphore.go:89-96`）；主流程成功后的删分片/删会话改 best-effort（孤儿已有 cleanup 兜底）；DEL 改 Lua compare-and-del。

3. **D-P1-3 动态 collection 默认排序 `_created_at DESC, _id DESC` 无索引支撑**
   `internal/infra/documentdb/postgres_collection_ddl.go:393-440` 建表仅 `PRIMARY KEY (_tenant,_id)`，attrs 索引只按用户显式定义；`postgres_query_compile.go:202` 默认排序与 keyset 谓词（`postgres_document_query.go:148`）均无索引可用。BaaS 列表态命门：默认 List 每页全表扫描+排序。
   修复方向：新表 DDL 默认建 `(_tenant,_created_at,_id)`（PG b-tree 可反向扫描服务 DESC）；存量在 DDL reconcile 路径幂等补建。

4. **P-P1-4 CHANGELOG 对 sdk/go v0.1.0 的发布声明与 tag 事实矛盾**
   `CHANGELOG.md:14-17` 声称「require 已改写、replace 已移除、下游可正常解析编译」；实测 `git show sdk/go/v0.1.0:sdk/go/go.mod` 仍含 `replace … => ../../genproto` 与伪版本 `v0.0.0-00010101000000-000000000000`。tag 不可变，唯一修复是发 v0.1.1 并修正 CHANGELOG（发布动作留给维护者，本轮先加警示）。

### P2（修复批覆盖项）

- **E-P2-1 退款回调零金额校验**（原 P1 降级）：`callback.go:261-281` applyRefunded 无金额比对；Stripe/微信/支付宝部分退款 Amount=退款额 → 订单翻终态 + `fulfiller.Reverse` 按 order.Amount 全额回收（`fulfiller.go:49-56`）。修复：金额不匹配（含缺失）时仅记日志、事件行保留、不驱动状态机。相邻问题（同批修复）：Reverse 失败仅日志无对账留痕。
- **E-P2-2 订阅续费永久卡死**：`subscribe.go:277-279` 幂等命中不检查 existing 状态；CreatePayment 失败→订单被 CloseExpiredOrders 关单→同 cycle 键永久命中 closed 单→每轮空转，不进 past_due，仅人工 ForceCancel 可解。
- **E-P2-3 hosted 终态订阅 webhook**：`hosted.go:52-60` 无 IsTerminal 预检；`billing.go` markPastDue/applyTerminal 从终态 Transition 必报错→事务回滚（事件登记一并回滚）→渠道重推 3 天后事件丢失、已付款订阅卡 trialing。**附带（评审 A 新发现）**：`hosted.go:41-49` 在 switch 前无条件改写 `CurrentPeriodStart/End/CancelAtPeriodEnd`，终态订阅字段可被改写。
- **E-P2-4 topup purpose float 截断**：`orders.go:219-227` `int64(n)` 截断放行 10.5；paid 时 `fulfiller.go:110-122` `Amount int64` 反序列化失败→永久回滚→渠道重推失败→用户已付款订单终态 closed。修复：validateTopupAmount 拒绝非整数 float。
- **E-P2-5 并发 Subscribe 双开**：无 (project,user,plan) 活跃唯一约束（`000017_subscriptions.up.sql`），`ListNonTerminalByUserPlan` 无锁；hosted 模式每期 benefits 双发。修复：partial unique index + 冲突映射 ErrAlreadySubscribed。**附带**：`subscribe.go:106-110` 幂等重放不检查订阅状态，可能返回已 canceled 订阅且 IdempotentReplay=true。
- **D-P2-6 offset 模式每页全量 COUNT**（原 P1 降级）：`postgres_document_query.go:157-166`。修复：仅首页（offset==0）COUNT，续页满页探测（limit+1）。

### P3（顺手修/记录）

- docker 清理无超时（`docker.go:305,335`）→ WithTimeout(30s)。
- realtime dedup 上限失效（`hub.go:156-163`）→ 超限时拒绝登记（保留查重窗口）。
- 资产幂等键跨动词碰撞（`service.go:117-135`，评审 B 定性为 Stripe 兼容语义）→ replay 时校验 Kind，不匹配返回 InvalidArgument。
- versionAlterTx 只增不删（`postgres_collection_ddl.go:451-457`）→ 注释声明增长模型（量级小，本轮不改造）。
- **文档组**：`docs/roadmap.md:53` 迁移号错误（应 000013/000015/000016/000017）+ M2 验收 checkbox 未同步；`docs/developer/08-functions.md:59` 网络描述与 per-project 默认不符；`docs/review/round4/fix-report-j4-j7.md:46` golangci「全量 0 issues」措辞与 exclusions 现实有出入（历史报告不改，此处记录）；HMAC 派生修正的滚动混布窗口未在部署说明提及。

### 记录不修（后续工作）

- Bulk N+1 批量化（P2，改动面大需保原子回滚+per-doc 权限/outbox，专项处理）。
- iOS 服务端 productID→价目映射（P1 根治方案，本轮 fail-closed 止血）。
- complete 幂等化（重试返回已建文件）。
- ios REFUND 订阅 benefits 不回收、milliunitsToMinor 精度损失（999→99 mismatch）、认证热路径 4 次点查、audit/sessions/identities GC、ListFiles 内存分页、billing SUM 全表轮询、CREATE INDEX CONCURRENTLY 变体、preview 并发预算、Range/ETag、存储配额、Functions 网络 Internal opt-in、Node 18 EOL。

## 四、各维度总评（R5）

| 维度 | 评分 | 一句话 |
|---|---|---|
| 架构分层与组装 | 9/10 | R4 J4 收口后组合根干净（五 ProviderSet + 显式 Bind），关停语义有论证 |
| 并发正确性 | 7.5/10 | Lua 原子性/SKIP LOCKED/OCC/singleflight 纪律好；清理路径 ctx 选择与缓存失效协议是系统性弱点 |
| 数据库正确性 | 9/10 | 注入面/OCC/advisory lock/幂等键/down 迁移全干净 |
| 数据库性能 | 6/10 | 默认排序无索引 + 每页 COUNT 是列表态命门；三张只写不删的表无 GC |
| 经济系统 | 6.5/10 | 账本内核高于平均；渠道边界金额语义（回调金额未被当作钱）是全部资损风险来源 |
| Functions/Storage | 8.5/10 | 凭据零泄漏、构建无注入面、zip/preview/锁防护扎实；缺资源配额面 |
| 测试与 CI | 8/10 | 门禁齐备；渠道金额异常边界（部分退款/Amount=0/float）无测试族——本轮 P1 均属此类 |
| 文档与发布诚实度 | 7/10 | 当前态文档已修复；CHANGELOG 失信与 roadmap 滞后是剩余污点 |

**系统性结论**：这套代码的「正确性纪律」显著高于「资源边界纪律」。并发原语与幂等设计规范，但「清理路径的 ctx 选择」（锁泄漏、无超时 Background）和「外部边界的金额语义」（回调金额字段没有被当作钱来校验）是两类反复出现的结构性弱点。建议后续将「清理路径一律 `WithoutCancel + 固定超时`」与「缓存写入必须回答何时删除」固化进 AGENTS.md 编码规约。
