# Torchwood Round 5 修复实施报告

> 日期：2026-08-25 ｜ 基线：`main` @ `904b4e6` ｜ 依据：[fix-plan.md](fix-plan.md)
> 方式：5 个修复子代理并行（文件域互不重叠），主代理统一全量验证。

## J1 经济安全批 ✅

### J1-1 applyPaid 金额 fail-closed（E-P1-1，P1）
`internal/app/payments/callback.go`：在既有 `event.Amount > 0 &&` 校验后新增「Amount==0 且 order.Amount>0 → FailedPrecondition 拒绝结算」，覆盖 iOS legacy verifyReceipt 与 ASN V2 Price=0 两个金额缺失入口（iOS 攻击链：0.99 美元真实 receipt + 自建大额 topup 单任意入账）。
- 全量检查既有测试：无任何用例依赖 Amount=0 通过 applyPaid，零改既有断言。
- 新增回归：`TestHandleCallback_PaidZeroAmountRefusedToSettle`、`TestHandleCallback_PaidZeroAmountFreeOrderSettles`（免费单不受影响）、`TestVerifyReceipt_LegacyZeroAmountRefused`（直打 legacy 入口）。

### J1-2 applyRefunded 金额校验（E-P2-1，P2）
同文件：`event.Amount != order.Amount`（含缺失=0）→ Error 日志 + `return nil`——不翻单、不 Reverse，事件行保留在 payment_callback_events 供对账；仅金额一致走既有路径。部分退款（商户渠道后台操作）不再触发全额资产回收+终态锁死。Reverse 失败日志补 provider_event_id。新增全额翻转/部分不翻/金额缺失不翻表驱动测试。

### J1-3 validateTopupAmount 拒绝非整数（E-P2-4，P2）
`internal/app/payments/orders.go`：float 分支 `n != math.Trunc(n)` 拒绝；json.Number `Int64()` 出错拒绝（原 `i, _ :=` 吞错）。封死「10.5 截断为 10 绕过一致性校验 → paid 回调 int64 反序列化失败 → 永久回滚 → 用户已付款订单 closed」链路。新增拒绝/整数放行测试。

验证：`go test ./internal/app/payments/... -count=1` 38 PASS / 0 FAIL（含 PG 集成）。

## J2 存储可靠性批 ✅

### J2-1/J2-2/J2-3（E-P1-2，P1）
- `internal/app/storage/uploads.go`：Complete/Abort 两处 defer 的 `UnlockComplete` 改 `context.WithTimeout(context.WithoutCancel(ctx), 2s)`（样板 `pkg/semaphore`）；封死「60s TimeoutHandler → ctx 取消 → DEL 失败被吞 → 锁残留 1h → 上传卡死」。
- complete 主流程（Compose + files.Insert）成功后的删分片与删会话改 best-effort（Warn 日志），孤儿分片由 48h `CleanupOrphanChunks`、残留会话由 24h TTL 兜底——「超时落在删分片中段导致大文件永久无法 complete」消除；失败回滚路径与 Abort 主语义不变。
- `internal/infra/storage/redis_upload_session.go`：DEL 改 Lua compare-and-del（token 比对），接口签名加 token 参数（domain 接口同步）。
- 新增测试：ctx 取消后锁可释放（miniredis 真实 store 模拟网关超时）、清理双失败不影响成功返回、compare-and-del 三态。

验证：`go test ./internal/app/storage/... ./internal/infra/storage/... -count=1` 全绿。

## J3 数据库性能批 ✅

### J3-1 默认时间索引（D-P1-3，P1）
`internal/infra/documentdb/postgres_collection_ddl.go` 新增 `ensureTenantCreatedIndex`：`CREATE INDEX IF NOT EXISTS idx_<coll>_tenant_created ON <tbl>(_tenant, _created_at, _id)`（b-tree 反向扫描服务 DESC）。双挂点：createCollectionTable 末尾（新表即建）+ reconcileVersionColumn 入口（存量集合任意 DDL touch 幂等补建，置于缓存短路之前防「索引被人工删除后不再补」）。系统集合跳过。
新增 `TestCreateCollection_DefaultTimeIndex`：建表有索引、DROP 后 touch 补建、幂等、isSystem 跳过。

### J3-2 offset 模式 COUNT 收敛（D-P2-6，P2）
`postgres_document_query.go`：COUNT 收窄为 `cursor == "" && offset == 0`（首页精确 total 不变）；续页满页探测（fetchLimit=limit+1，截断后再做权限回填），total=0=unknown（proto 合法语义）。keyset 与首页路径完全不变。
新增 `TestListDocuments_OffsetContinuationSkipsCount`（bun AddQueryHook 计数 COUNT 语句）。`TestListDocuments_PaginationGuards` 续页 total 断言按新语义更新。

验证：`go test ./internal/infra/documentdb/... -count=1` 全绿（含集成）。

## J4 订阅可靠性批 ✅

**关键架构发现**：subscriptions 运行时表在项目 schema `tw_<project>`（public 副本已被 000022 删除），约束必须落 projectschema + 控制面回填双层。

### J4-1 续费死单重建（E-P2-2，P2）
`subscribe.go` createBillingOrder 改重试循环（≤5 键）：命中 `created` 原单继续下单；命中终态（closed/failed/refunded）换 `原键#N` 重建；命中 `paying/paid/refunding` 原样返回（防同周期重复扣款）；耗尽转哨兵错误 → `billOrPastDue` 走 `markPastDue`。「订阅永久空转卡死」消除。

### J4-2 hosted 终态预检（E-P2-3，P2）
`hosted.go`：provider_sub_id 回填与 period 改写**之前**对 `sub.Status.IsTerminal()` 直接 `return nil`（事件行由调用方事务保留）——迟到 webhook 不再因 Transition 报错回滚事件登记（渠道重推 3 天后事件丢失、已付款订阅卡 trialing）；终态订阅字段不再被旁路改写。

### J4-3 并发 Subscribe 双开防护（E-P2-5，P2）
- `internal/infra/projectschema/migrations/000010_subscriptions_live_unique.up.sql`：`UNIQUE (project_id, user_id, plan_id) WHERE status IN ('trialing','active','past_due')`（新建/存量项目全覆盖，版本化迁移器自动重放）。
- `db/migrations/000023_subscriptions_live_unique.{up,down}.sql`：DO 块遍历存量 `tw_%` schema 回填同索引（可逆，通过 up→down→up 循环测试）。
- repo Insert 捕获 23505（约束名+文本合并比对，修复 Field('N') 为空路径漏网）→ `ErrAlreadySubscribed`。
- 附带：幂等重放命中终态订阅改返回新 domain 错误 `ErrReplayTerminalSubscription`（FailedPrecondition，引导换新键），不再伪装 IdempotentReplay=true。

验证：订阅域/repo/testutil/projectschema 全绿；并发 `-race` 集成测试恰一成功。

## J5 卫生批 ✅

- **J5-1** docker.go ContainerRemove/ContainerStop 包 30s 独立超时（daemon 挂起不再无限阻塞清理链）。
- **J5-2** hub.go markSeen：清理后仍满 dedupMax 时跳过登记（保留查重语义，内存有界）。
- **J5-3** **降级为文档**（发现真实语义依赖）：订阅 benefits 的 entitlement 键 `sub:<id>:ent:<period>:<i>` 在同周期内首次走 Grant、重入走 Mutate（同 key），严格 Kind 校验会破坏 hosted 恢复流程——改为 loadReam 与 `proto/server/v1/assets.proto` 五个请求消息的 idempotency_key 注释写明「项目级全局键空间（Stripe 语义），不同动词不可复用」，已重生成 genproto。此发现同时修正了评审 B「内部键无跨动词复用」的论断。
- **J5-4** 文档：roadmap 迁移号改 000013/000015/000016/000017；M2 验收勾选 5 项（逐项代码核对）；「事件脊柱+Outbox」移入已具备；08-functions.md 两处网络描述改 per-project 默认；CHANGELOG v0.1.0 加「tag 不可下游解析，待 v0.1.1」警示。
- **J5-5** markVersionAlterTx 注释声明增长模型。

验证：functions/realtime/assets 域 + runtime（swagger 一致性）全绿。

## J6 全量验证中发现的既有失败（顺手修复）

全量 `-race` 测试暴露 3 个 HEAD 上即存在的失败（`git worktree` 干净基线复核确证，非本轮修复引入）：

1. **account token 锁定可被正确 secret 绕过**（`internal/infra/auth/account_token_redis.go`）：P3-2 把「超限锁定」从删除记录改为保留记录后，Lua 成功路径未检查锁定状态——超限后正确 secret 仍匹配 hash 并 DEL+成功，与 Round3 H6-3 契约（超限后正确 secret 也拒绝）不符。修复：成功路径前置 `(record.attempts or 0) >= max` 检查，命中返回 locked。`TestRedisAccountTokenStore_WrongSecretCountsAndLocks` 恢复通过。
2. **console cookie Max-Age 断言未随 R4 J7-1 同步**（`internal/api/consolegrpc/auth_test.go:151`）：实现已收紧 1h，测试仍期望 86400。断言改 3600。
3. **ListDocuments 续页 total 断言**（`internal/app/server/databases_integration_test.go:223`）：J3-2 语义变化（续页 total=0=unknown）的连带更新，按新语义改写并注明依据。

## J7 Bulk Update/Delete 批量化（遗留专项，后续落地）

原「记录不修」清单中改动面最大的一项，作为独立批次实施（语义清单先行、主代理逐项复核）：

- **实现**（`internal/infra/documentdb/postgres_permissions.go`）：校验与权限判断全前置（批量预取 `_perms` + 内存 `AllowsDocumentAccess` 同函数，语义零漂移）→ 单条 `UPDATE ... WHERE _id IN (...) RETURNING to_jsonb(d.*)`（写入+写后快照一步）/ delete 走批量 `SELECT ... FOR UPDATE`（保行锁）+ IN DELETE；`_perms` 替换改 1 条 IN DELETE + 1 条多值 INSERT；outbox 事件保持 per-doc（实时订阅按 doc 过滤），`publishDocumentEvent` 增加 coll 参数消除每文档 GetCollection（顺带收敛 audit P2-8）。
- **语句数**：N=100 实测 update 107 条 / delete 109 条（旧路径 ~8-14N），即 O(N)+常数；IN 列表按 900 分片、_perms INSERT 按 2000 行分片（PG 参数上限防御）。
- **语义保持**：all-or-nothing（不存在/权限拒绝/任何错误整体回滚）、SkipVersion 仍 `_version+1`、仅权限变更刷审计列、`ErrNoFieldsToUpdate`、update 事件 acl=写前、delete 事件 version=写前、系统集合不发事件、事务边界不变——10 项清单逐项自查 + 测试锁定。
- **可观察差异（有意）**：重复 docID 按唯一集合执行（旧路径重复执行 affected=2/version+2/2 事件 → 新 affected=1/+1/1 事件），`TestBulkDocuments_DuplicateIDsSingleEffect` 固化。
- **测试**：新增 all-or-nothing 双向、事件 ACL/快照断言、语句数验证（AddQueryHook 断言 ≥N 且 <2N，锁定 per-doc 事件不被合并）；`go test ./internal/infra/documentdb/... -count=1` 与关联层回归全绿。

## 整体验证

- `go build ./...` ✅ `go vet ./...` ✅ `gofmt -l` 干净 ✅
- `go test -race ./... -count=1`（.env + PG/Redis/MinIO）全绿（68 包）。
- 新增迁移通过 `TestMigrations_UpDownUpCycle`（000023 up→down→up）与 projectschema 版本断言（9→10）。

## 未修（记录于 audit-report「记录不修」）

Bulk N+1 批量化、iOS productID→价目映射（本轮 fail-closed 止血）、complete 幂等化、ios REFUND 订阅 benefits 回收、milliunitsToMinor 精度、认证热路径点查合并、audit/sessions/identities GC、CREATE INDEX CONCURRENTLY 变体、存储配额、Functions 网络 Internal opt-in、Node 18 EOL。
