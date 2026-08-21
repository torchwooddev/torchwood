# v2 实施派发稿（给第三方 agent / opencode）

> **已过期。** D-6 已删除 staged transaction API（内测无兼容）。

把下面 **「总则」+ 当前要做的那一张 PR** 整段复制到新 session。一次只做一张 PR，合入（或至少自测绿）后再派下一张。

顺序：**PR1 → PR2 →（PR3 与 PR4 可并行）→ PR5**。

规格唯一来源：`docs/design/v2-events-realtime-transactions.md`  
执行切片：`docs/design/v2-execution-plan.md`  
仓库约定：`AGENTS.md`、`docs/developer/09-api-guide.md`

不要使用 `docs/prompts/databases-transactions.md`（已过期）。不要重开产品决策。

---

## 总则（每张 PR 都贴）

你在仓库 `D:\Codes\qiulin\torchwood`（或当前 clone）实施 Torchwood v2 的**一张** PR。

1. 先读 `AGENTS.md`，再读批准设计里与本 PR 对应的章节（PR 切片见设计文末 `## PR Plan`）。
2. Clean Architecture：handler 薄、use-case 编排、端口在 domain、适配器在 infra。
3. 改 proto 后执行 `task generate-proto`，禁止手改 `genproto/**`。
4. 改 Wire provider 后执行 `task wire-all`。
5. 改 Console 后先 `task console-build` 再 `task build`。
6. 对话、commit message、PR 说明用简体中文。
7. 本 PR 范围以外的文件不要改。不要顺手重构。
8. 做完后在回复里给出：变更文件列表、自测命令与退出码、**与设计的偏差**（无则写「无偏差」）。不要声称「已审查通过」。

完成后停下来，等仓库 owner 审查。不要自行开始下一张 PR，除非派发内容明确包含多张。

---

## Prompt：PR1 `_version` + 强制 OCC

```
实施 Torchwood v2 PR1：用户 collection `_version` + Update/Delete/Increment 强制 OCC。

规格：docs/design/v2-events-realtime-transactions.md
切片：该文档 ## PR Plan → ### PR1
约束：docs/prompts/implement-v2.md 总则 + AGENTS.md

范围（必须做完）：
- 用户表加 _version；仅写路径懒 ALTER + 类型检查（非 bigint fail-closed）；读路径缺列视为 1，不要在读路径 ALTER。
- createCollectionTable 增加 isSystem；系统集合不加列。
- proto：Document.version=6；UpdateDocumentRequest.version=7 optional；独立 DeleteDocumentRequest（REST ?version=）。task generate-proto。
- 端口用 DeleteOptions{ExpectedVersion, SkipVersion}；所有 DocumentDB 假实现一并改。仓库没有 groups_test.go，要改 groups_pagination_test.go。
- Bulk/Upsert SkipVersion；Increment 必带 version。
- 拒绝用户属性名 _version。未 ALTER 表上 $version 查询返回 version_column_unavailable，不要落到 PG 未定义列。
- Console 详情保存和列表删除都传 version；CLI 增加 --version；TS/Go SDK + contract 测试同步。

禁止：EventPublisher、Websocket、事务 RPC、outbox。

验收：
- go vet ./... 与 go test -short ./... 绿
- 未带 version 的 Update/Delete/Increment → version_required
- version 错 → version_mismatch，行不变
- Bulk 不传 version 仍成功
- CLI 无 --version 拒
```

---

## Prompt：PR2 事务性 outbox

```
实施 Torchwood v2 PR2：用户 collection 文档写的事务性 outbox。PR1 必须已经在当前分支上。

规格：docs/design/v2-events-realtime-transactions.md §事件 / outbox / WithTransactionID
切片：## PR Plan → ### PR2
约束：docs/prompts/implement-v2.md 总则 + AGENTS.md

范围：
- internal/domain/events 信封（含服务端 acl 快照）+ ClientPayload() 去掉 acl
- EventPublisher 在 documentdb 写成功读回之后、仍在同一 RunInTx 内 INSERT public.document_events_outbox
- 必须走同一个 *clients.Database.Conn(ctx)，禁止另开连接
- Create/Update/Increment/Delete/Bulk/Upsert 挂钩；系统集合跳过
- 256KiB 截断事件、不回滚业务写
- WithTransactionID(ctx) 就位，本 PR 普通写路径不注入该键
- migration 000011；task wire-all
- 本 PR 不启 Redis Stream，不标 published_at

禁止：/v1/realtime、Hub、事务 API。

验收：
- 成功写有 outbox 行；失败写无行
- 系统集合无 outbox 行
- ClientPayload 不含 acl
- go test 覆盖 outbox 挂钩与截断
```

---

## Prompt：PR3 Realtime WS + Stream 最后一跳

```
实施 Torchwood v2 PR3：WebSocket Hub + Redis Streams 最后一跳。PR2 必须已经在当前分支上。

规格：docs/design/v2-events-realtime-transactions.md §Realtime、§3.4、§4
切片：## PR Plan → ### PR3
约束：docs/prompts/implement-v2.md 总则 + AGENTS.md

范围：
- GET /v1/realtime 挂 combined mux
- HTTP：WithTimeout(0) 只清 lynx 默认，必须再用 WithServerOptions 设 ReadTimeout=0、WriteTimeout=0、ReadHeaderTimeout=10s；非 WS 路径自套 TimeoutHandler；websocket.Accept 后立刻清 conn deadline；ping 滑窗
- 握手：SDK 首帧 JWT；Console cookie；admin 先 principal.ProjectID=hello.project_id 再 ValidateAdminProjectAccess；无 API Key、无 guest；JWT 过期断开
- 频道名按设计；订集合不查 collection read；订文档失败统一 NOT_FOUND；无通配
- Hub.Dispatch 收完整信封（含 acl）做 VisibleTo；连接 chan 只放 ClientPayload()
- XADD 存完整 outbox JSON（含 acl），MAXLEN ~ 50000
- Worker 只 XADD + dispatched_at，不标 published_at
- Server RealtimeSubscriber：XGROUP CREATE 0-0 MKSTREAM（BUSYGROUP 忽略）；每轮 XAUTOCLAIM idle 30s 再 XREADGROUP >；成功扇出后 XACK 再标 published_at
- 领取 SQL 含 dispatched_at IS NULL 与 2min 整进程挂死兜底
- 配额 4 连 / 32 订

禁止：SDK 封装、Console 试听 tab（PR5）。

验收：
- 连接保持 >60s
- 非 admin 按 _perms 收到事件且出站无 acl
- 无订阅者仍 XACK
- XACK 前杀 subscriber，重启后远小于 2min 再投同一 event_id
- worker 先于 server 启动时建组前的 XADD 仍被消费
```

---

## Prompt：PR4 单库事务

```
实施 Torchwood v2 PR4：Client + Server 单库 staged 事务。依赖 PR1+PR2，不依赖 PR3。

规格：docs/design/v2-events-realtime-transactions.md §事务
切片：## PR Plan → ### PR4
约束：docs/prompts/implement-v2.md 总则 + AGENTS.md

范围：
- Client 与 Server 对称事务 RPC（设计文 API 节）
- TTL 60s、最多 100 op、同一 principal+project+database 同时 1 个 pending（部分唯一索引）
- 仅创建者追加/Commit/Rollback；Console admin 与 databases 写权限 API Key 可干预任意 pending
- 用户 collection only；系统集合拒
- 追加 / seq / 100 上限 / Commit 都要对 document_transactions FOR UPDATE
- Commit：RunInTx 内 WithTransactionID 再调现有 CRUD（不要二次 INSERT outbox）
- apply 因权限/version 失败：整段回滚后另开短事务、行仍 pending 时标 rolled_back
- 过期：锁行后就地 SET expired，不 apply
- 同文档多 op 允许；upsert-insert 记 version=1
- 权限与单条 CRUD 一致（update 只查 update）
- apiKeyScopeRules + adminRoleMethodRules 同步登记全部事务写方法（member/owner/admin）；GetTransaction 不进写表
- task generate-proto && task wire-all

验收：
- 设计文档测试最低集 PR4 各条
- 失败后 GetTransaction=rolled_back，再 Commit → transaction_not_pending
- 进程能启动（AssertAdminRoleWriteCoverage 不 panic）
- grpc_swagger / method_auth 测试绿
```

---

## Prompt：PR5 SDK + Console 试听

```
实施 Torchwood v2 PR5：Client SDK Realtime + Console 集合试听。依赖 PR3。

规格：docs/design/v2-events-realtime-transactions.md §SDK / Console
切片：## PR Plan → ### PR5
约束：docs/prompts/implement-v2.md 总则 + AGENTS.md

范围：
- sdk/typescript 与 sdk/go/client：connect({ projectId, getAccessToken })、subscribe(channel, handler)
- JWT 过期断开后 refresh 再握手，不补历史
- Console 集合详情「试听」tab：仅 isPlatformAdmin；cookie 握手必须带 project_id
- 系统/停用集合不显示试听
- 15m 断线走 /v1/console/auth refresh，再 hello + 重订；失败显示「已断开，点击重连」
- task console-build && task build（Go embed 必须吃到新前端）

验收：
- SDK 单测
- Console 组件测试含重连
- task console-build && task build 绿
```

---

## 全部完成后给审查方的回执模板

```
分支：
PR 范围（1/2/3/4/5）：
偏差（对照 docs/design/v2-events-realtime-transactions.md）：
自测：
- go vet ./...
- go test -short ./...
- （如有）go test ./sdk/go/...
- （如有）sdk/typescript 测试
- （如有）task console-build && task build
未跑的测试与原因：
```
