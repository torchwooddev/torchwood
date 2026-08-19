package server

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/lynx-go/lynx"
	lynxhttp "github.com/lynx-go/lynx/server/http"
	clientv1 "github.com/torchwooddev/torchwood/genproto/client/v1"
	consolev1 "github.com/torchwooddev/torchwood/genproto/console/v1"
	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
	apirealtime "github.com/torchwooddev/torchwood/internal/api/realtime"
	"github.com/torchwooddev/torchwood/internal/api/serverhttp"
	"github.com/torchwooddev/torchwood/internal/infra/health"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type GRPCGatewayServer struct {
	*lynxhttp.Server
}

func NewGRPCGatewayServer(
	app lynx.App,
	cfg *config.AppConfig,
	checkers *health.Checkers,
	fileHandler *serverhttp.FileHandler,
	oauthHandler *serverhttp.OAuthHandler,
	functionsHandler *serverhttp.FunctionsHandler,
	paymentsHandler *serverhttp.PaymentsHandler,
	realtimeHandler *apirealtime.Handler,
) (*GRPCGatewayServer, error) {
	httpCfg := cfg.GetServer().GetHttp()
	timeout := parseDuration(httpCfg.GetTimeout(), 60*time.Second)

	grpcAddr := cfg.GetServer().GetGrpc().GetAddr()
	grpcEndpoint := grpcEndpointFromAddr(grpcAddr)

	mux := runtime.NewServeMux(
		runtime.WithErrorHandler(HTTPErrorHandler),
		runtime.WithIncomingHeaderMatcher(authIncomingHeaderMatcher),
		runtime.WithOutgoingHeaderMatcher(authOutgoingHeaderMatcher),
		runtime.WithMarshalerOption("*", NewCustomMarshaler()),
		runtime.WithMarshalerOption("*/*", NewCustomMarshaler()),
		runtime.WithMarshalerOption("application/json", NewCustomMarshaler()),
	)

	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	ctx := app.Context()

	register := []func(context.Context, *runtime.ServeMux, string, []grpc.DialOption) error{
		clientv1.RegisterAccountServiceHandlerFromEndpoint,
		clientv1.RegisterDatabasesServiceHandlerFromEndpoint,
		clientv1.RegisterTeamsServiceHandlerFromEndpoint,
		serverv1.RegisterHealthServiceHandlerFromEndpoint,
		serverv1.RegisterProjectsServiceHandlerFromEndpoint,
		serverv1.RegisterStorageServiceHandlerFromEndpoint,
		serverv1.RegisterUsersServiceHandlerFromEndpoint,
		serverv1.RegisterAPIKeysServiceHandlerFromEndpoint,
		serverv1.RegisterOAuthProvidersServiceHandlerFromEndpoint,
		serverv1.RegisterTeamsServiceHandlerFromEndpoint,
		serverv1.RegisterDatabasesServiceHandlerFromEndpoint,
		serverv1.RegisterFunctionsServiceHandlerFromEndpoint,
		serverv1.RegisterPaymentsServiceHandlerFromEndpoint,
		serverv1.RegisterBillingServiceHandlerFromEndpoint,
		clientv1.RegisterPaymentsServiceHandlerFromEndpoint,
		serverv1.RegisterAssetsServiceHandlerFromEndpoint,
		serverv1.RegisterSubscriptionsServiceHandlerFromEndpoint,
		clientv1.RegisterAssetsServiceHandlerFromEndpoint,
		clientv1.RegisterSubscriptionsServiceHandlerFromEndpoint,
		consolev1.RegisterConsoleAuthServiceHandlerFromEndpoint,
		consolev1.RegisterAdminsServiceHandlerFromEndpoint,
	}
	for _, fn := range register {
		if err := fn(ctx, mux, grpcEndpoint, opts); err != nil {
			return nil, err
		}
	}

	// Custom HTTP handlers for file upload/download and OAuth callbacks.
	fileHandler.Register(mux)
	oauthHandler.Register(mux)
	functionsHandler.Register(mux)
	paymentsHandler.Register(mux)

	handler := http.Handler(mux)

	consoleHandler, err := NewConsoleHandler()
	if err != nil {
		return nil, err
	}

	// /v1/realtime 是长连接 WebSocket：不套 TimeoutHandler（下放
	// 握手超时与 ping 滑窗自行管理）；其余路径统一 60s TimeoutHandler
	// 兜底慢 handler。
	var routed http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/realtime":
			realtimeHandler.ServeHTTP(w, r)
		case strings.HasPrefix(r.URL.Path, "/console/") || r.URL.Path == "/console":
			http.TimeoutHandler(consoleHandler, timeout, "timeout").ServeHTTP(w, r)
		default:
			http.TimeoutHandler(handler, timeout, "timeout").ServeHTTP(w, r)
		}
	})

	if cors := httpCfg.GetCors(); cors != nil {
		routed = CORSMiddleware(cors, app.Logger())(routed)
	}

	// http.Server 超时（v2 设计 §4.1 锁定）：
	// - WithTimeout(0) 只清 lynx 默认 60s（否则读写超时打在 net.Conn 上，
	//   Hijack 之后 WS 连接约 60s 被杀，ping 无法重置一次性 deadline）；
	// - WithServerOptions 在内部赋值之后执行，覆盖为
	//   ReadTimeout=0 / WriteTimeout=0 / ReadHeaderTimeout=10s（保留
	//   慢握手 / Slowloris 上限）。
	return &GRPCGatewayServer{lynxhttp.NewServer(routed,
		lynxhttp.WithAddr(httpCfg.GetAddr()),
		lynxhttp.WithTimeout(0),
		lynxhttp.WithServerOptions(func(s *http.Server) {
			s.ReadTimeout = 0
			s.WriteTimeout = 0
			s.ReadHeaderTimeout = 10 * time.Second
		}),
		// Recovery 声明在最外层：gateway 转发/自定义 handler/Console SPA
		// 任一环节 panic 都被恢复为 500 + 统一 JSON 错误体，不拖垮进程。
		lynxhttp.WithMiddleware(lynxhttp.Recovery()),
		// /healthz/readiness 依赖 checkers（任一失败 503）；请求日志为
		// Debug 级（lynx requestlog），需 --log-level debug 可见。
		lynxhttp.WithHealthCheckers(func() []lynx.Checker { return checkers.Deps() }),
		lynxhttp.WithLogger(app.Logger()),
		lynxhttp.WithRequestLog(true),
	)}, nil
}

// grpcEndpointFromAddr 从 server.grpc.addr 推导 gateway 转发目标：
// 保留原主机（默认回环），仅补充缺失的端口默认值。
func grpcEndpointFromAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		if addr != "" && !strings.HasPrefix(addr, ":") {
			return addr
		}
		return "127.0.0.1:9060"
	}
	if host == "" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

func portFromAddr(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return "8088"
	}
	return port
}

func authIncomingHeaderMatcher(key string) (string, bool) {
	switch strings.ToLower(key) {
	case "authorization", "cookie", "x-api-key", "x-torchwood-project", "x-request-id":
		return strings.ToLower(key), true
	default:
		return runtime.DefaultHeaderMatcher(key)
	}
}

// authOutgoingHeaderMatcher 把 console auth handler 下发的 set-cookie metadata
// 透传为 Set-Cookie 响应头（用于 HttpOnly 会话 cookie）。grpc-gateway v2.27.1
// 的默认 defaultOutgoingHeaderMatcher 会给所有 metadata key 加
// "Grpc-Metadata-" 前缀，不自定义 matcher 则 cookie 永远到不了浏览器；其余
// key 保持默认行为不变。
func authOutgoingHeaderMatcher(key string) (string, bool) {
	if strings.EqualFold(key, "set-cookie") {
		return "Set-Cookie", true
	}
	return runtime.MetadataHeaderPrefix + key, true
}
