package runtime

import "github.com/google/wire"

// ProviderSet 收纳组装根的运行时服务（Round4 J4-4）：
// 原寄生于 internal/infra 的三项 gRPC/HTTP 运行时已迁至 internal/runtime，
// 保持对 infra 的单向依赖（runtime → infra/auth 等适配器，infra 不再反向依赖 api）。
var ProviderSet = wire.NewSet(
	NewGRPCServer,
	NewGRPCGatewayServer,
	NewMetricsServer,
	NewConsoleHandler,
)
