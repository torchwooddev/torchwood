// Package torchwood 是 Torchwood BaaS 的官方 Go SDK。
//
// SDK 提供两类客户端：
//
//   - [Client]：Client API（end-user 认证，Authorization: Bearer JWT），
//     封装 Account / Teams / Databases（文档 CRUD）服务。
//   - [ServerClient]：Server API（x-api-key 认证），
//     封装 Health / Users / Teams / Databases（库、集合、属性、索引、文档）服务。
//
// 所有调用均返回 gRPC status 错误，可用 google.golang.org/grpc/status.Code
// 判别错误类别（如 codes.NotFound、codes.PermissionDenied）。
package torchwood

import (
	"context"
	"fmt"

	clientv1 "github.com/torchwooddev/torchwood/genproto/client/v1"
	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// Config 汇总 SDK 客户端配置，由 [Option] 修改。
type Config struct {
	// ServerAPIKey 用于 Server API 认证（x-api-key 头）。
	ServerAPIKey string
	// AccessToken 用于 Client API 认证（Authorization: Bearer 头）。
	AccessToken string
	// ProjectID 是默认项目：填入 Account 注册/登录请求的 project_id，
	// 并以 x-torchwood-project 头随 Server API 调用发送（admin session 项目覆盖）。
	ProjectID string
	// DatabaseID 是文档 API 使用的默认 database_id。
	DatabaseID string
	// dialOptions 是传给 grpc.NewClient 的额外拨号选项。
	dialOptions []grpc.DialOption
}

// Option 以函数选项模式修改 [Config]。
type Option func(*Config)

// WithServerAPIKey 设置 Server API 认证用的 API Key。
func WithServerAPIKey(key string) Option {
	return func(c *Config) { c.ServerAPIKey = key }
}

// WithAccessToken 设置 Client API 认证用的访问令牌（end-user JWT）。
func WithAccessToken(token string) Option {
	return func(c *Config) { c.AccessToken = token }
}

// WithProjectID 设置默认项目 ID。
func WithProjectID(projectID string) Option {
	return func(c *Config) { c.ProjectID = projectID }
}

// WithDatabaseID 设置文档 API 的默认 database_id。
func WithDatabaseID(databaseID string) Option {
	return func(c *Config) { c.DatabaseID = databaseID }
}

// WithDialOptions 附加底层 gRPC 拨号选项（如 TLS 凭据、自定义 dialer）。
func WithDialOptions(opts ...grpc.DialOption) Option {
	return func(c *Config) { c.dialOptions = append(c.dialOptions, opts...) }
}

func applyOptions(opts []Option) Config {
	var cfg Config
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

func dialTarget(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	if target == "" {
		return nil, fmt.Errorf("torchwood target is required")
	}
	all := append([]grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}, opts...)
	return grpc.NewClient(target, all...)
}

// Client 封装 Torchwood Client API（end-user 认证）。
type Client struct {
	cfg  Config
	conn *grpc.ClientConn

	account   clientv1.AccountServiceClient
	teams     clientv1.TeamsServiceClient
	databases clientv1.DatabasesServiceClient

	// Account 提供注册/登录/账户管理。
	Account *AccountService
	// Teams 提供团队与成员管理。
	Teams *TeamsService
	// Databases 提供文档 CRUD，绑定默认 DatabaseID。
	Databases *DatabasesService
}

// NewClient 建立 Client API 连接。target 为 gRPC 目标地址，不能为空。
func NewClient(target string, opts ...Option) (*Client, error) {
	cfg := applyOptions(opts)
	conn, err := dialTarget(target, cfg.dialOptions...)
	if err != nil {
		return nil, err
	}
	return newClient(conn, cfg), nil
}

func newClient(conn *grpc.ClientConn, cfg Config) *Client {
	c := &Client{
		cfg:       cfg,
		conn:      conn,
		account:   clientv1.NewAccountServiceClient(conn),
		teams:     clientv1.NewTeamsServiceClient(conn),
		databases: clientv1.NewDatabasesServiceClient(conn),
	}
	c.Account = &AccountService{c: c}
	c.Teams = &TeamsService{c: c}
	c.Databases = c.UseDatabase(cfg.DatabaseID)
	return c
}

// AuthContext 返回附加了 AccessToken 认证元数据的上下文；
// 未配置 AccessToken 时原样返回。
func (c *Client) AuthContext(ctx context.Context) context.Context {
	if c.cfg.AccessToken == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+c.cfg.AccessToken)
}

// SetAccessToken 运行时更新 AccessToken（如登录后回填）。
func (c *Client) SetAccessToken(token string) {
	c.cfg.AccessToken = token
}

// UseDatabase 返回绑定指定 databaseID 的文档服务副本；
// 默认见 [WithDatabaseID]。
func (c *Client) UseDatabase(databaseID string) *DatabasesService {
	return &DatabasesService{c: c, db: databaseID}
}

// Close 关闭底层连接。
func (c *Client) Close() error {
	return c.conn.Close()
}

// ServerClient 封装 Torchwood Server API（API Key 认证）。
type ServerClient struct {
	cfg  Config
	conn *grpc.ClientConn

	health    serverv1.HealthServiceClient
	users     serverv1.UsersServiceClient
	teams     serverv1.TeamsServiceClient
	databases serverv1.DatabasesServiceClient

	// Health 提供服务健康检查。
	Health *HealthService
	// Users 提供用户管理（含 CreateUserToken 签发 Agent 凭证）。
	Users *UsersService
	// Teams 提供服务端团队管理。
	Teams *ServerTeamsService
	// Databases 提供库/集合/属性/索引/文档管理，绑定默认 DatabaseID。
	Databases *ServerDatabasesService
}

// NewServerClient 建立 Server API 连接。target 为 gRPC 目标地址，不能为空。
func NewServerClient(target string, opts ...Option) (*ServerClient, error) {
	cfg := applyOptions(opts)
	conn, err := dialTarget(target, cfg.dialOptions...)
	if err != nil {
		return nil, err
	}
	return newServerClient(conn, cfg), nil
}

func newServerClient(conn *grpc.ClientConn, cfg Config) *ServerClient {
	c := &ServerClient{
		cfg:       cfg,
		conn:      conn,
		health:    serverv1.NewHealthServiceClient(conn),
		users:     serverv1.NewUsersServiceClient(conn),
		teams:     serverv1.NewTeamsServiceClient(conn),
		databases: serverv1.NewDatabasesServiceClient(conn),
	}
	c.Health = &HealthService{c: c}
	c.Users = &UsersService{c: c}
	c.Teams = &ServerTeamsService{c: c}
	c.Databases = c.UseDatabase(cfg.DatabaseID)
	return c
}

// AuthContext 返回附加了 API Key（及可选项目）认证元数据的上下文。
func (c *ServerClient) AuthContext(ctx context.Context) context.Context {
	md := metadata.Pairs("x-api-key", c.cfg.ServerAPIKey)
	if c.cfg.ProjectID != "" {
		md.Set("x-torchwood-project", c.cfg.ProjectID)
	}
	return metadata.NewOutgoingContext(ctx, md)
}

// SetAPIKey 运行时更新 API Key。
func (c *ServerClient) SetAPIKey(key string) {
	c.cfg.ServerAPIKey = key
}

// UseDatabase 返回绑定指定 databaseID 的文档服务副本；
// 默认见 [WithDatabaseID]。
func (c *ServerClient) UseDatabase(databaseID string) *ServerDatabasesService {
	return &ServerDatabasesService{c: c, db: databaseID}
}

// Close 关闭底层连接。
func (c *ServerClient) Close() error {
	return c.conn.Close()
}
