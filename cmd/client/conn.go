package main

import (
	"context"
	"errors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// newConn 建立到服务端的 gRPC 连接。MVP 走明文（insecure），服务端当前仅监听
// 回环 127.0.0.1:9060；--tls 为占位参数，使用时返回未支持错误。
func newConn(endpoint, apiKey string, tls bool) (*grpc.ClientConn, error) {
	if tls {
		return nil, errors.New("--tls 尚未支持：服务端当前为明文 gRPC（默认仅回环 127.0.0.1:9060）")
	}
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	if apiKey != "" {
		opts = append(opts, grpc.WithUnaryInterceptor(apiKeyUnaryInterceptor(apiKey)))
	}
	return grpc.NewClient(endpoint, opts...)
}

// apiKeyUnaryInterceptor 向每次 unary 调用的 outgoing metadata 注入 x-api-key。
// 注意：不传 X-Torchwood-Project（该 header 仅对 admin console session 有效）。
func apiKeyUnaryInterceptor(apiKey string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx = metadata.AppendToOutgoingContext(ctx, "x-api-key", apiKey)
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}
