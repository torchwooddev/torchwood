// Package conn 提供 client 与 server 两包共享的 gRPC 拨号逻辑。
package conn

import (
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Dial 建立到 target 的 gRPC 连接；默认明文（insecure），调用方可通过
// opts 传入 TLS 凭据或自定义 dialer。target 不能为空。
func Dial(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	if target == "" {
		return nil, fmt.Errorf("torchwood: target is required")
	}
	all := append([]grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}, opts...)
	return grpc.NewClient(target, all...)
}
