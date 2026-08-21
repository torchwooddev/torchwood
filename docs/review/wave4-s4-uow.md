# S-4 对外 `uow.Run`（第一刀）

> 后波。对应 `docs/review/first-principles-design.md` §4 S-4、`docs/review/first-principles-plan.md` S-4。  
> 日期：2026-08-21。**已锁定。** Owner：对外 `uow.Run`；实现可暂用 ctx。

## 锁定

1. **对外缝**是 `uow.Run(ctx, fn)`。`fn` 仍接收 `ctx`；适配器仍可从 ctx 读取连接。不是一夜换成 `func(tx Tx)`。
2. **实现可暂用 ctx**：`clients.Database.Run` 委托已有 `RunInTx`（已在事务内则加入，不嵌套）。`RunInTx` 保留，既有调用方不断。
3. **领域端口**不得再写驱动类型（尤其 `bun.Tx`）。契约写成：加入调用方的 `uow.Run`；实现可从 ctx 读取连接。
4. **本波只改**端口注释 + 经济/outbox 热路径上已有的 `TxRunner` / `txRunner` / `RunInTx` 用例缝。不改事务边界、不改 `Order.Transition`、不改 outbox 同事务、不改经济锁策略（S-2）。

## 做

- `docs/review/wave4-s4-uow.md` + `docs/review/README.md` 登记。
- `pkg/uow.Runner`：`Run(ctx, fn func(ctx) error) error`。放 `pkg/`，避免领域包长出 infra 词。
- `*clients.Database` 实现 `uow.Runner`：`Run` → `RunInTx`。
- 重写 `shared.EventPublisher`、`payments.OrderRepo`、`assets.DefRepo` / `HoldingRepo` 及同类「感知事务连接」注释。
- `domain/assets.Service` 的 `TxRunner` 换成 `uow.Runner` / `Run`。
- 支付 / 订阅 use-case 同形 `txRunner` 换成 `uow.Runner`（订阅另保留 `RunInNewTx`，两段式建单行为不变）。
- outbox 适配器注释与端口同一套措辞；内部仍 `Conn(ctx)`。

## 不做

- 不重写全部 `Conn(ctx)` / `RunInTx` 调用点（documentdb、bunrepo、staged Commit、建项目……一律不动）。
- 不引入嵌套事务；不改 join-if-already-in-tx。
- 不改经济锁策略（S-2），不把 `ListForUpdate` 收成支付/订阅公开端口。
- 不把 gRPC 引进领域；不改公开 RPC。

## 验收

- `gofmt`；`go vet` 触及包。
- `go test ./pkg/uow/... ./internal/infra/clients/... ./internal/domain/assets/... ./internal/app/assets/... ./internal/app/payments/...`
- `Database.Run` 在已有事务的 ctx 上加入外层，不新开。
- `internal/domain/**` 不含 `bun.Tx`。
