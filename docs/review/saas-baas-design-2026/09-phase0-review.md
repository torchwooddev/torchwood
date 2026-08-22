# 阶段0 复核报告（对照代码）

> 范围：`07-existing-problems.md` + `08-fix-plan.md` 的 11 必核项（A1/C1/A2/A3/A4/A5/A6/A7/A8/A9/B1/B5/B8/B10/B11），以 `main` 分支代码为准。

## 总体结论

**全部属实，无需修改 08-fix-plan 方案。** 08 的六条工作流切分与验收口径仍正确，可直接进入阶段1并行实现。仅对 2 处实现细节做补充说明（不改 08 决策）。

---

## 逐项复核

### A1 DefaultCollectionPermissions 含 read:any；ParsePermissionStrings 空列表走默认
- **判定：属实**
- 证据：`internal/domain/databases/permissions.go:14-28` `DefaultCollectionPermissions()` 含 `{Type:"read", Role:"any"}`；`:149` `ParsePermissionStrings` 在 `len==0` 时返回该默认集；`ExpandPermissionRoles:48-49` 无条件注入 `any`。
- 方案评估：08 要求删除该条 `read:any`，保留 `create/update/delete:users/keys/admin`。与现有 `AllowsDocumentAccess` 的文档级覆盖语义一致，Delete后需同步改单测。

### C1 Server 空文档 ACE → 无 _perms 行 → 回落集合 ACL
- **判定：属实**
- 证据：`internal/api/servergrpc/databases.go:614-622` `parseOptionalPermissions` 空→`nil`；`internal/app/server` 空 perms 不写 owner ACE；`permissions.go:99-100` `!docHasPerms` 时返回 `collOK`，叠加 A1 的集合 `read:any` 则 guest 可读。`internal/infra/documentdb/postgres_permissions.go:108-120` 同理 `NOT EXISTS` 兜底。
- 方案评估：08 的“写零 ACE 且令 `docHasPerms=true`”或“Server 空时写仅 privileged 匹配的占位”均可达成私有。与 B1 文档级覆盖一致，系统集合 `IsSystem` 的 OR 豁免保持。

### A2 Client CreateOrder purpose.amount 与 Order.Amount 无关，fulfiller 用 purpose 数量 Grant
- **判定：属实**
- 证据：`internal/app/payments/orders.go:22-32,59-119` 接收自由 `purpose` map；`internal/app/assets/fulfiller.go:42-77,65-77` `parsePurpose` 直接取 `purpose.amount`/`quantity` 作为 Grant 数量；`callback.go:175-178` 仅校验回调金额==订单金额，未校验履约数量==订单金额。
- 方案评估：08 要求 `topup` 时 `purpose.amount == Order.Amount` 否则 InvalidArgument，且 Grant 用 `Order.Amount`；`item_purchase`/`subscription` 在 Client 拒掉。正确，能堵“1分买天量”。

### A3 CreateOrder 调 Stripe 时 SuccessURL 为空
- **判定：属实**
- 证据：`internal/app/payments/orders.go:111-119` `CreatePaymentInput` 未填 `SuccessURL/CancelURL`；`internal/infra/payments/stripe/stripe.go:96-101,503-511` 仅非空才写表单，`mode=payment` 下真实 Stripe 会 400；`internal/domain/payments/provider.go:82` 该字段存在但未被用例填充。
- 方案评估：08 要求请求可选 `success_url/cancel_url` + `public_url` fallback + 用例必传非空。合理；需给 `proto/client/v1/payments.proto` 加 `optional string success_url/cancel_url` 并 `task generate-proto`。

### A4 tryCharge 建一次性单后立刻 past_due
- **判定：属实**
- 证据：`internal/app/subscriptions/billing.go:132-139` 无 `BillingAssetCode` 时 `createBillingOrder` 后 `return false, nil`；`billOrPastDue:122-124` `!charged -> markPastDue`。即每期都新 Checkout 但合同立刻 past_due。
- 方案评估：08 的“账期等待支付，保持 active/trialing，靠回调/关单驱动”正确；需保留 `markPastDue` 仅走资产不足或回调 past_due 分支。

### A5 Refund 不 Reverse；ManualFulfill 不 Fulfill；部分退款与全额同态
- **判定：属实**
- 证据：`internal/app/payments/refund.go:22-99,104-173` `Refund` 仅翻 `refunded/refunding` 并发事件，无资产回滚；`ManualFulfill:102-173` 仅 `MarkDone`；`46-48` 校验允许 `amount==0 || 0<amount<=order.Amount` 的部分退款，回调 `applyRefunded:262-279` 也不做 Reverse。
- 方案评估：08 的 `Fulfiller.Reverse -> Consume`、仅全额退款、ManualFulfill 真调 `Fulfiller.Fulfill` 后才标 done 正确；`KindAdjust` 不单做，CHECK 留 `adjust` 可。

### A6 Realtime Subscriber 用 Redis consumer group + 进程内 Hub
- **判定：属实**
- 证据：`internal/infra/realtime/hub.go` 单进程 `map[channel]map[conn]`；`internal/infra/realtime/subscriber.go:20,43-189`  consumer group `torchwood-realtime` + `XREADGROUP/XAUTOCLAIM`；多副本时消息只给组内一个消费者，另一副本 Hub 静默。
- 方案评估：08 的“Outbox 领取仍队列，Hub 前一跳改 PUBLISH/SUBSCRIBE 广播，删 XREADGROUP 竞争”正确。接受宕机期间漏推不可补历史。

### A7 Queue 是 BRPOP，取出即丢
- **判定：属实**
- 证据：`internal/infra/queue/redis_queue.go:21-40` `LPUSH/BRPOP`；`cmd/worker/worker.go:17-143,59-67` 取出即离队，崩溃丢消息，仅启动 1h 扫描标 `failed` 不重放；`internal/domain/shared/ports.go:14-19` `Queue.Dequeue` 无 Ack。
- 方案评估：08 的“Stream/XREADGROUP/XACK 或 BRPOPLPUSH+Ack，失败/崩溃可重投”正确；需改 `Queue` 接口为 `(payload, ack Token)` + `Ack`，推荐 Stream 与 outbox 同哲学。

### A8 CreateFile 丢 Permissions；Get/List/Delete 的 principal 不参与判定
- **判定：属实**
- 证据：`internal/app/storage/storage.go:54-62,212-307` `CreateFileCommand.Permissions` 接收但未用，`CreateFile`仅写 `owner_user_id`；`GetFile/ListFiles/DeleteFile:260-307` 取 `principal` 但未校验，仅检查存在性；`file_handler.go:450-473` 对应。
- 方案评估：08 的“文件按 owner_user_id / public bucket / admin-key 鉴权，删撒谎的 Permissions 字段，List 仅返回自己或 public 桶内文件”正确。

### A9 HTTP /v1/storage 接受 EndUser JWT，gRPC Storage 是 API Key
- **判定：属实**
- 证据：`internal/api/serverhttp/file_handler.go:108-120,774-783` `authorize` 对 EndUser JWT 放行上传；`internal/app/storage/storage.go:64-71` `CreateBucket` 才有 `RequireServerWriteActor`，`CreateFile` 无；`pkg/grpc/interceptor` Server Storage 均为 `ACCESS_API_KEY`。正式表面 Server-only，实际 EndUser 能直传。
- 方案评估：08 的“保留 `/v1/storage` 作端用户直传，但受 A8 所有者/public 约束；写操作加审计；Server CRUD 仍 `/v1/server/storage`；Functions HTTP 仍拒 EndUser”正确。

### B1 admin_projects 几乎只在 Setup Grant；非超管 ListProjects 恒空
- **判定：属实**
- 证据：`internal/domain/projects/admin_project.go:4-7` 仅 `HasProjectAccess/GrantProjectAccess`；`internal/app/console/setup.go:185-187` 唯一生产 Grant；`internal/app/console/admins.go:74-113` `CreateAdmin` 不绑项目；`internal/app/server/projects.go:198-214` 非平台 admin 直接返回空列表。
- 方案评估：08 的 `ListProjectIDs` + 非平台 admin 走 `admin_projects` + `CreateAdmin` 时若带 ProjectID 则 Grant 当前项目，最小可用。正确。

### B5 public 000013–000017 经济表 vs 运行时 tw_<project>
- **判定：属实**
- 证据：`db/migrations/000013_payments.up.sql` 等在 public 建 `payment_orders/asset_holdings/subscriptions/usage_*`；运行时 `internal/infra/bun/bunrepo/*.go` 经 `Scoped(ctx, r.db, projectID, "payment_orders", ...)` 写 `tw_<project>.*`；`internal/infra/projectschema/migrations/000004_payments.up.sql` 等在 `{{schema}}` 建同名表；`internal/infra/bun/model/payments.go:10` 注释仍写 public，实际不读。
- 方案评估：08 的“新 migration DROP public 幽灵表，勿动 outbox/provider_resource_index”正确；需先 grep 确认无 public 读路径。

### B8 DeleteAttribute 不删依赖 indexes
- **判定：属实**
- 证据：`internal/infra/documentdb/postgres_permissions.go:244-271` `DeleteAttribute` 仅 `ALTER DROP COLUMN` + 删 `document_attributes`，未处理 `document_indexes`；`internal/infra/documentdb/postgres.go:269-280` `DeleteIndex` 另有逻辑。
- 方案评估：08 的“同一 RunInTx 先查依赖 indexes→DROP INDEX→删 catalog，再 DROP COLUMN”正确。

### B10 gRPC SignIn 丢 HMAC cookie；Realtime 拒 end-user cookie
- **判定：属实**
- 证据：`internal/api/clientgrpc/account.go:29-57` `SignUp/SignIn` 对第三返回值 `_, ` 丢弃；`internal/infra/auth/session_service.go:49-139` 每次产出 `TORCHWOOD_session_<project>`；`internal/api/serverhttp/oauth_handler.go:64-72` 仅 OAuth 回调 SetCookie；`internal/api/realtime/handler.go:401-403` 拒 end-user cookie。
- 方案评估：08 的“gRPC 成功路径经 gateway metadata Set-Cookie，Realtime 接受该 cookie（二选一 JWT）”正确。

### B11 swagger 写 API Key 必须带 X-Torchwood-Project
- **判定：属实**
- 证据：`proto/server/v1/*.proto` 等 10+ 文件 `MethodAuth apiKey` 描述含“需同时携带 X-Torchwood-Project”；运行时 `internal/infra/auth/validator.go:119-140` API Key 的 `ProjectID` 来自行，`pkg/grpc/interceptor` 仅 admin 分支用该头。
- 方案评估：08 的“改为：项目绑定在密钥行上，不需要该头；该头仅 Console admin 会话切换项目”正确，改后 `task generate-proto`。

## 对 08 的偏差与补充

- 无需推翻任何工作流；仅在实现时注意：
  - A3/B9 的 fallback URL 必须用 `server.http.public_url`，不得再用 `https://example.com` 静默藏错。
  - A4 的 `createBillingOrder` 成功后应视为等待支付，不标 past_due；`grace_days` 仍由回调 past_due 触发后才计。
  - A7 的新 `Queue` 若选 Stream，需与 `cmd/worker` 的 `processDue`/`requeue` 语义兼容（payload 内 `attempt` 仍有效）。
  - B5 的 DROP 迁移需排在 `000021` 之后，down 可不重建（内测）。

## 结论

08-fix-plan 可直接作为阶段1的六条并行工作流输入，按文件不重叠切分执行。建议每流独立 worktree，触及包 `go test -short`，`gofmt`，独立中文 commit。
