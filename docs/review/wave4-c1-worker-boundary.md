# C-1 worker 模块边界

> 后波。对应 `docs/review/first-principles-design.md` §8 C-1、`docs/review/first-principles-plan.md` C-1。  
> 日期：2026-08-21。**已锁定。**

## 锁定

1. **作业切分已对**，本条只收口装配图。worker 继续负责：functions 队列、outbox、支付关单、资产过期、订阅扣款、用量汇总、storage 孤儿分片。Lynx + Wire 留下。**不要**拆成多进程。
2. `cmd/worker` **禁止**引用 `app.ProviderSet` / `infra.ProviderSet` / `domain.ProviderSet` 桶包。只显式列出作业用例构造器及其真正需要的 infra 适配器。
3. 用例：`functions.NewFunctionsWithUsage`、`payments.NewPayments`、`assets.NewAssets`、`subscriptions.NewSubscriptions`、`subscriptions.NewOrderFulfiller`、`billing.NewBilling`、`storage.NewStorage`；`SubscriptionCallbackHandler` 仍绑定到 `*subscriptions.Subscriptions`。
4. 两边进程仍跑 `projectschema.EnsureAll` OnStart hook。
5. `cmd/worker/wire_gen.go` 实例化图与改前行为等价（同一组 `New*`，同一作业间隔与语义）。

## 做

- 重写 `cmd/worker/provides.go`：按包列出 provider（clients / bunrepo 作业表 / events / Docker 执行器 / 四支付渠道 / Redis 用量 / 队列 / MinIO / `realtime.NewStreamTransport`）。
- Bucket/File bun 适配器返回具体类型，组合根补 `wire.Bind` 到领域接口（与 server 侧 `infra.ProviderSet` 同类绑定，不改 repo 签名）。
- `task wire-all`，提交生成的 `wire_gen.go`。
- `cmd/worker/import_guard_test.go`：生产源码 import 与 `go list -deps .` 均不得出现禁止包。
- proto Query codec 从 `pkg/query` 挪到 `pkg/query/proto`：AST/`Parse` 不依赖 genproto，避免 worker 经 `domain/databases` / bunrepo 把 RPC 生成代码拉进来。

## 不做

- 不拆 worker 为多进程，不改作业间隔 / `CloseExpiredOrders` / `ExpireHoldings` / `ProcessDue` / `Rollup` / `CleanupOrphanChunks` / outbox。
- 不把 Account / Users / DocumentDB / gRPC server / Gateway / Metrics / auth / Hub+Subscriber 拉进 worker。
- 不改 `Order.Transition`、履约同事务、不砍 RPC、不删 document engine、不把 Agent 加成 `ActorKind`。
- 不重写 `uow.Run`、不改经济锁接口、禁止手改 `genproto/**`。

## 验收

- `cmd/worker` 生产 `.go` 与 `go list -deps ./cmd/worker` 不含：`internal/app`（桶）、`internal/infra`（桶）、`internal/app/client`、`internal/app/console`、`internal/app/server`、`internal/api`、`internal/infra/auth`、`internal/infra/documentdb`、`internal/infra/server`、`genproto`。
- `gofmt`、`go vet ./cmd/worker/...`、`go test ./cmd/worker/...` 绿。
- 现有 functions 消费 / requeue / chunk cleaner 测试保持绿。
