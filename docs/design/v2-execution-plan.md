# v2 执行计划

> 对应批准设计：`docs/design/v2-events-realtime-transactions.md`  
> 派发稿：`docs/prompts/implement-v2.md`  
> 日期：2026-08-15

第三方 agent 按 PR 顺序实施。PR3 与 PR4 在 PR2 合入后可并行；其余必须串行。完成后由仓库 owner 做严格审查，不以实施方自报为准。

```
PR1 OCC  →  PR2 Outbox  →  PR3 WS+Stream
                      └→  PR4 Transactions
PR3 ───────────────→  PR5 SDK + Console 试听
```

## 共同约束（每张 PR）

- 读 `AGENTS.md` 与批准设计对应章节；产品决策不重开。
- 禁止手改 `genproto/**`。改 proto 后 `task generate-proto`；改 Wire 后 `task wire-all`。
- 改 Console 后 `task console-build` 再 `task build`。
- 对话与 commit 用简体中文。
- 每张 PR 结束必须：`go vet ./...`、`go test -short ./...` 绿；触及 SDK 时跑 `go test ./sdk/go/...` 与 `sdk/typescript` 测试。
- 不把未完成的 PR 混进同一提交。

## PR1 — `_version` + 强制 OCC

**目标**：用户 collection 有 `_version`；Update / Delete / Increment 强制带 version；Bulk / Upsert / Create 不带。

**不做**：EventPublisher、Realtime、事务 API。

**关键验收**

- 未带 version 的 Update/Delete/Increment → `version_required`。
- version 对不上 → `version_mismatch`，行不变。
- Bulk 不传 version 仍成功；系统集合无 `_version` 列。
- Console 详情保存与列表删除、CLI `--version`、TS/Go SDK 全部回传 version。
- 全部 `DocumentDB` 假实现改完能编译（含 `groups_pagination_test.go`，仓库没有 `groups_test.go`）。

**命令**：`task generate-proto`；`go test -short ./internal/infra/documentdb/... ./internal/app/client/... ./internal/app/server/... ./sdk/go/...`

## PR2 — 事务性 outbox

**目标**：用户 collection 文档写与 `public.document_events_outbox` 同一 `RunInTx`。

**不做**：WS、Redis Stream、标 `published_at`（避免 PR3 前把行标死）。

**关键验收**

- Create/Update/Increment/Delete/Bulk/Upsert 成功则有对应 outbox 行；失败写无行。
- 系统集合不写 outbox。
- 信封含 `acl`（仅服务端）；`ClientPayload()` 不含 `acl`。
- 超 256KiB 截断事件、不回滚业务写。
- `WithTransactionID` 已就位（PR4 使用），本 PR 的写路径 ctx 不带该键。

**命令**：`task wire-all`；集成测试断言 outbox 行。

## PR3 — Realtime WS + Hub + Stream

**目标**：`GET /v1/realtime`；worker `XADD`；server `XAUTOCLAIM` + `XREADGROUP >` → Hub → `XACK` → `published_at`。

**不做**：SDK 封装、Console 试听（PR5）。

**关键验收**

- 连接保持 > 60s（`ReadTimeout=0` / `WriteTimeout=0` / `ReadHeaderTimeout=10s` + 清 hijack deadline + ping）。
- Client JWT 按 `_perms` 收事件；出站帧无 `acl`。
- platform admin cookie 旁路；非 platform admin 不旁路。
- 无订阅者仍 XACK。
- XACK 前杀 subscriber，重启后远小于 2min 再投同一 `event_id`。
- worker 先于 server 启动时，建组前的 XADD 仍被消费（`XGROUP CREATE 0-0`）。
- 配额 4 连 / 32 订。

## PR4 — 单库事务

**目标**：Client + Server 事务 API；Commit 经 `WithTransactionID` 写 outbox，不二次 INSERT。

**不依赖** PR3。

**关键验收**

- 系统集合 / 跨库拒。
- 同文档多 op 版本接力（含 upsert-insert → 1）。
- apply 失败后 `GetTransaction` 为 `rolled_back`，再 Commit → `transaction_not_pending`。
- 追加与 Commit `FOR UPDATE`；第二笔 pending 拒。
- 非创建者 Client 拒；admin / databases 写 Key 可干预。
- 空 Commit 成功、无事件。
- `adminRoleMethodRules` 登记全部事务写方法，进程能启动。
- swagger / `method_auth` 测试绿。

## PR5 — SDK + Console 试听

**目标**：TS/Go Client `connect` + `subscribe`；集合详情「试听」tab。

**关键验收**

- JWT 过期：SDK refresh 后重握手，不补历史。
- Console：仅 `isPlatformAdmin` 见 tab；cookie 握手先绑 `project_id`；15m 后走 console auth refresh 再订；失败显示断开。
- `task console-build && task build`。

## 完成后交给审查方

实施方提交：分支 / diff、每张 PR 的自测命令与输出、与设计的偏差清单（无则写「无偏差」）。

审查方对照批准设计逐项读源码并亲自跑命令，不以自报为准。
