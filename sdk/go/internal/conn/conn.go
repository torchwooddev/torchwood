// Package conn 提供 client 与 server 两包共享的 gRPC 拨号逻辑。
package conn

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// DefaultTimeout 是未配置 WithTimeout 时单次调用的兜底超时（调用方忘传
// deadline 时防止无限等待，audit R4 P1 #10）。
const DefaultTimeout = 30 * time.Second

// retryServiceConfigJSON 是默认 gRPC service config：对 UNAVAILABLE 做最多
// 4 次指数退避重试。UNAVAILABLE 表示连接/暂不可用类瞬态故障，幂等读安全；
// 非幂等写在极端情况（请求已达服务端但响应丢失）可能重复执行，可通过
// WithRetryDisabled 关闭（gRPC 内置按 token 的重试节流限制放大效应）。
const retryServiceConfigJSON = `{
  "methodConfig": [
    {
      "name": [{}],
      "retryPolicy": {
        "maxAttempts": 4,
        "initialBackoff": "0.2s",
        "maxBackoff": "5s",
        "backoffMultiplier": 1.6,
        "retryableStatusCodes": ["UNAVAILABLE"]
      }
    }
  ]
}`

// Dial 建立到 target 的 gRPC 连接；默认明文（insecure），调用方可通过
// opts 传入 TLS 凭据或自定义 dialer。target 不能为空。
func Dial(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	if target == "" {
		return nil, fmt.Errorf("torchwood: target is required")
	}
	all := append([]grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		// 与服务端 MaxRecvMsgSize(8<<20) 对齐（internal/infra/server/grpc.go）。
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(8 << 20)),
	}, opts...)
	return grpc.NewClient(target, all...)
}

// TimeoutUnaryInterceptor 返回 per-call deadline 兜底拦截器：ctx 已有
// deadline 时原样尊重（不覆盖），否则注入 timeout 兜底；timeout<=0 视为
// DefaultTimeout。
func TimeoutUnaryInterceptor(timeout time.Duration) grpc.UnaryClientInterceptor {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if _, ok := ctx.Deadline(); !ok {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// RetryDialOption 返回默认重试 service config 拨号选项（见
// retryServiceConfigJSON）。
func RetryDialOption() grpc.DialOption {
	return grpc.WithDefaultServiceConfig(retryServiceConfigJSON)
}
