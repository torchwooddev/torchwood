# Round 5 修复计划

> 依据：[audit-report.md](audit-report.md) 交叉验证后的最终分级。
> 原则：P1 全修；P2 修经济/订阅/分页语义批；P3 只修零风险顺手项；Bulk 批量化、iOS 价目表、complete 幂等化等大改动记后续专项。

## J1 经济安全批（P1 + P2 经济）

- **J1-1**（E-P1-1）`applyPaid` 金额 fail-closed：`internal/app/payments/callback.go` 对「`event.Amount == 0` 且 `order.Amount > 0`」返回 `FailedPrecondition`（覆盖 iOS legacy 与 ASN V2 Price=0 两个入口）；保留既有 `>0 && !=` 校验。同步检查/更新受影响测试，新增回归测试（Amount=0 拒绝翻单）。
- **J1-2**（E-P2-1）`applyRefunded` 金额校验：`event.Amount != order.Amount`（含 0/缺失）时记 Error 日志并 `return nil`（事件行保留在 payment_callback_events 供对账，不驱动状态机、不 Reverse）；仅金额一致才走现有路径。Reverse 失败日志补充 order/provider_event 定位字段。新增部分退款不翻单测试。
- **J1-3**（E-P2-4）`validateTopupAmount` 拒绝非整数：`internal/app/payments/orders.go:219-227` float 分支 `n != math.Trunc(n)` 返回 InvalidArgument；`json.Number` 非整同样拒绝。

## J2 存储可靠性批（P1）

- **J2-1**（E-P1-2）`UnlockComplete` 独立 ctx：`internal/app/storage/uploads.go:189,317` defer 改 `context.WithTimeout(context.WithoutCancel(ctx), 2s)`（样板 `pkg/semaphore/semaphore.go:89-96`）。
- **J2-2** complete 主流程（Compose + files Insert）成功后的删分片循环与删会话改 best-effort：失败仅 Warn 日志，孤儿分片由既有 48h cleanup（`internal/app/storage/cleanup.go`）、会话由 24h TTL 兜底；保证 ctx 取消不再导致锁滞留与「大文件永久无法 complete」。
- **J2-3** `UnlockComplete` 改 Lua compare-and-del（持锁 token 比对成功才 DEL），`internal/infra/storage/redis_upload_session.go`。
- 新增/更新单测与集成测试（锁在 ctx 取消后可释放）。

## J3 数据库性能批（P1 + P2）

- **J3-1**（D-P1-3）动态 collection 默认时间索引：新表 DDL（`postgres_collection_ddl.go` createCollectionTable 路径）默认 `CREATE INDEX IF NOT EXISTS <tbl>_tenant_created_idx ON <tbl>(_tenant, _created_at, _id)`（b-tree 反向扫描服务 DESC，无需 DESC 关键字）；存量集合在既有 reconcile/索引确保路径幂等补建。注意两段式 DDL 与系统表（isSystem）排除。
- **J3-2**（D-P2-6）offset 模式 COUNT 收敛：`postgres_document_query.go` 仅 `offset == 0` 时执行精确 COUNT；续页取 `limit+1` 行做满页探测决定 next token（proto 语义 `total_count ≤0 = unknown` 已合法）。检查并更新对续页 total 的测试断言；新增「续页不再 COUNT」的行为测试（可断言 SQL 计数或以 mock/stub 验证）。

## J4 订阅可靠性批（P2）

- **J4-1**（E-P2-2）`createBillingOrder` 幂等命中检查状态：existing 非 `created`（closed/failed）时换新键重建订单（`原cycle键#N`，N=同前缀已存订单数）或走 `markPastDue`，消灭「永久空转卡死」。
- **J4-2**（E-P2-3）`HandleHostedCallback` 入口对终态订阅直接 `return nil`（事件行保留）；`hosted.go:41-49` 的 period 字段改写移到非终态分支，终态订阅不被旁路改写。
- **J4-3**（E-P2-5）并发双开防护：新增迁移 `db/migrations/000023_subscriptions_live_unique.up.sql`（partial unique index `(project_id, user_id, plan_id) WHERE status IN ('trialing','active','past_due')`，含可逆 down——注意 testutil 会跑全量 down）；Insert 冲突（23505/唯一索引名）映射 `ErrAlreadySubscribed`。附带：`subscribe.go` 幂等重放命中终态订阅时不再返回 IdempotentReplay 成功，改返回 `ErrAlreadySubscribed` 语义（终态=不再持有）。
- 新增订阅批测试。

## J5 卫生批（P3 + 文档）

- **J5-1** `docker.go:305,335` ContainerRemove/ContainerStop 包 `context.WithTimeout(context.Background(), 30s)`。
- **J5-2** `hub.go` dedup 超限且清理后仍满时跳过登记（保留查重判断），补注释说明窗口语义。
- **J5-3** `internal/domain/assets/service.go` loadReplay 命中时校验 `first.Kind` 与当前动词，不匹配返回 InvalidArgument（若有测试依赖跨动词重放语义则降级为文档注释并说明）。
- **J5-4** 文档：`docs/roadmap.md:53` 迁移号改 000013/000015/000016/000017；M2 验收 checkbox 按现状勾选；§0「规划中」表移除已交付的事件脊柱/Outbox。`docs/developer/08-functions.md:59` 网络描述改 per-project 默认。`CHANGELOG.md` v0.1.0 小节加「该 tag 的 go.mod 仍含本地 replace，不可下游解析；待 v0.1.1 重发」警示。
- **J5-5** `versionAlterTx` 注释声明增长模型（每 DDL 事务一条、进程生命周期累积）。

## 验收

- `go build ./...`、`go vet ./...`、`gofmt -l` 干净。
- `go test ./... -count=1`（.env + PG/Redis/MinIO）全绿。
- 新增迁移通过 `migrations_cycle_test.go`（up→down→up）。
- 修复项与 audit-report 分级一一对应，fix-report.md 记录实施细节。
