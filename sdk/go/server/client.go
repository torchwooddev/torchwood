// Package server 是 Torchwood Server API 的 Go 客户端（x-api-key 认证）。
package server

import (
	"context"

	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
	"github.com/torchwooddev/torchwood/sdk/go/internal/conn"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// Config 汇总 Server API 客户端配置，由 Option 修改。
type Config struct {
	// APIKey 用于 x-api-key 头认证。
	APIKey string
	// ProjectID 非空时以 x-torchwood-project 头发送（admin session 项目覆盖）。
	ProjectID string
	// DatabaseID 是文档 API 使用的默认 database_id。
	DatabaseID string
	// dialOptions 透传给底层拨号。
	dialOptions []grpc.DialOption
}

// Option 以函数选项模式修改 Config。
type Option func(*Config)

// WithAPIKey 设置 Server API 认证用的 API Key。
func WithAPIKey(key string) Option { return func(c *Config) { c.APIKey = key } }

// WithProjectID 设置 x-torchwood-project 头。
func WithProjectID(projectID string) Option { return func(c *Config) { c.ProjectID = projectID } }

// WithDatabaseID 设置文档 API 的默认 database_id。
func WithDatabaseID(databaseID string) Option {
	return func(c *Config) { c.DatabaseID = databaseID }
}

// WithDialOptions 附加底层 gRPC 拨号选项。
func WithDialOptions(opts ...grpc.DialOption) Option {
	return func(c *Config) { c.dialOptions = append(c.dialOptions, opts...) }
}

// Client 封装 Torchwood Server API（API Key 认证）。
type Client struct {
	cfg  Config
	conn *grpc.ClientConn

	health    serverv1.HealthServiceClient
	users     serverv1.UsersServiceClient
	teams     serverv1.TeamsServiceClient
	databases serverv1.DatabasesServiceClient

	// Health 提供服务健康检查（ACCESS_PUBLIC，可不配置 API Key）。
	Health *HealthService
}

// New 建立 Server API 连接。target 为 gRPC 目标地址，不能为空。
func New(target string, opts ...Option) (*Client, error) {
	var cfg Config
	for _, o := range opts {
		o(&cfg)
	}
	c := &Client{cfg: cfg}
	dialOpts := append([]grpc.DialOption{grpc.WithChainUnaryInterceptor(c.authInterceptor())}, cfg.dialOptions...)
	gc, err := conn.Dial(target, dialOpts...)
	if err != nil {
		return nil, err
	}
	c.conn = gc
	c.health = serverv1.NewHealthServiceClient(gc)
	c.Health = &HealthService{c: c, api: c.health}
	return c, nil
}

// authInterceptor 注入 x-api-key 与 x-torchwood-project 头。
func (c *Client) authInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		return invoker(c.authContext(ctx), method, req, reply, cc, opts...)
	}
}

func (c *Client) authContext(ctx context.Context) context.Context {
	md := metadata.MD{}
	if c.cfg.APIKey != "" {
		md.Set("x-api-key", c.cfg.APIKey)
	}
	if c.cfg.ProjectID != "" {
		md.Set("x-torchwood-project", c.cfg.ProjectID)
	}
	if len(md) == 0 {
		return ctx
	}
	return metadata.NewOutgoingContext(ctx, md)
}

// Close 关闭底层连接。
func (c *Client) Close() error { return c.conn.Close() }
