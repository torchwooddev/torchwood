# M-4 五动词进 Assets 领域服务

> 后波。对应 `docs/review/first-principles-design.md` §3 M-4、`docs/review/first-principles-plan.md` M-4。  
> 日期：2026-08-21。**已锁定。** Owner：五动词进 Assets 领域服务，不放 Holding。

## 锁定

1. **Grant / Consume / Transfer / Mutate / Expire** 是单一 `domain/assets.Service` 的接口。跨 Def + Holding + Ledger，不放上 `Holding` 实体（反向 Feature Envy）。
2. **仓储只持久化**：`DefRepo` / `HoldingRepo` / `LedgerRepo` 形状不变。`FOR UPDATE` / `FOR SHARE` / `SKIP LOCKED` 只在服务内部调用，不升成支付/订阅可调的公开锁端口。
3. **不变式不改**：ledger 幂等（`InsertIfAbsent` + 重放）、OCC `expectVersion`、FEFO、`unique_per_owner`、class 矩阵、`ValidateGrant` / `ValidateConsumeQuantity` / `ValidateMutateClass`。
4. **app 层只做**：`requireAssetWrite`（含 worker 的 System principal）、项目/操作者注入、领域 sentinel → gRPC status。公开 RPC 形状不变。
5. **事务缝**：不重写 S-4 `uow.Run`；写路径仍 `RunInTx` + ctx 传连接。
6. **Wire**：继续 `app/assets.NewAssets`；领域服务在其内部构造，不新增 Wire 类型。
7. **ExpireDue**：到期删行与 `Expire` 同一引擎（幂等键 `expire:{holdingID}[:expiresAt]`），挂在 `Service.ExpireDue`（单项目）。worker 的多项目轮转游标与预算仍在 app。

## 做

- `docs/review/wave4-m4-assets-service.md` + `docs/review/README.md` 登记。
- `internal/domain/assets.Service`：`Grant` / `Consume` / `Transfer` / `Mutate` / `Expire` / `ExpireDue`；`Command` / `OpResult` 无 grpc 类型，下沉领域。
- 把 `internal/app/assets/write.go` 与到期扫描的不变式体搬进领域；app 五动词变为鉴权 + `svc.Grant(...)`。
- 领域只返回既有（及字段校验）sentinel；app `mapWriteError` 映射。领域禁止 import grpc。
- 编译期/单测断言 `Holding` 无 Grant/Consume/Transfer 方法。

## 不做

- 不把五动词放到 `Holding`。
- 不改 ledger / OCC / 幂等 / FEFO / unique_per_owner / class 矩阵语义。
- 不改公开 RPC、不改履约 `GrantCommand` 调用方签名心智（app 对 Command/OpResult 做别名）。
- 不重写 `uow.Run`；不把 `ListForUpdate` 收成支付/订阅的领域端口。
- 不把 Def CRUD / 只读查询 / Reconcile / 多项目轮转做成领域服务（本条只收五动词）。
- 不向 `domain.ProviderSet` 加新 Wire 类型。

## 验收

- `gofmt`；`go vet ./internal/app/assets/... ./internal/domain/assets/...`
- `go test ./internal/app/assets/... ./internal/domain/assets/...` 绿（含既有幂等/FEFO/不足即整单失败/转让双边流水）。
- 领域包 import 不含 `google.golang.org/grpc`。
- `Holding` 无 Grant/Consume/Transfer/Mutate/Expire 方法。
- Wire 仍只暴露 `assets.NewAssets`。
