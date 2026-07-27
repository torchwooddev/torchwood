package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	clientv1 "github.com/deeploop-ai/graviton/genproto/client/v1"
	consolev1 "github.com/deeploop-ai/graviton/genproto/console/v1"
	serverv1 "github.com/deeploop-ai/graviton/genproto/server/v1"
	"github.com/deeploop-ai/graviton/internal/pkg/config"
	"github.com/deeploop-ai/graviton/internal/api/serverhttp"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/lynx-go/lynx"
	lynxhttp "github.com/lynx-go/lynx/server/http"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type GRPCGatewayServer struct {
	*lynxhttp.Server
}

func NewGRPCGatewayServer(
	app lynx.Lynx,
	cfg *config.AppConfig,
	fileHandler *serverhttp.FileHandler,
	oauthHandler *serverhttp.OAuthHandler,
) (*GRPCGatewayServer, error) {
	httpCfg := cfg.GetServer().GetHttp()
	timeout := parseDuration(httpCfg.GetTimeout(), 60*time.Second)

	grpcAddr := cfg.GetServer().GetGrpc().GetAddr()
	grpcEndpoint := fmt.Sprintf("127.0.0.1:%s", portFromAddr(grpcAddr))

	mux := runtime.NewServeMux(
		runtime.WithErrorHandler(HTTPErrorHandler),
		runtime.WithIncomingHeaderMatcher(authIncomingHeaderMatcher),
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
		consolev1.RegisterConsoleAuthServiceHandlerFromEndpoint,
	}
	for _, fn := range register {
		if err := fn(ctx, mux, grpcEndpoint, opts); err != nil {
			return nil, err
		}
	}

	// Custom HTTP handlers for file upload/download and OAuth callbacks.
	fileHandler.Register(mux)
	oauthHandler.Register(mux)

	handler := http.Handler(mux)

	consoleHandler, err := NewConsoleHandler()
	if err != nil {
		return nil, err
	}

	var combined http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/console/") || r.URL.Path == "/console" {
			consoleHandler.ServeHTTP(w, r)
			return
		}
		handler.ServeHTTP(w, r)
	})

	if cors := httpCfg.GetCors(); cors != nil {
		combined = CORSMiddleware(cors)(combined)
	}

	return &GRPCGatewayServer{lynxhttp.NewServer(combined, lynxhttp.WithAddr(httpCfg.GetAddr()), lynxhttp.WithTimeout(timeout))}, nil
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
	case "authorization", "cookie", "x-api-key", "x-graviton-project", "x-request-id":
		return strings.ToLower(key), true
	default:
		return runtime.DefaultHeaderMatcher(key)
	}
}
