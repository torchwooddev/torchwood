package server

import (
	"fmt"
	"sort"
	"strings"
	"time"

	clientv1 "github.com/torchwooddev/torchwood/genproto/client/v1"
	consolev1 "github.com/torchwooddev/torchwood/genproto/console/v1"
	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
	sharedv1 "github.com/torchwooddev/torchwood/genproto/shared/v1"
	"github.com/torchwooddev/torchwood/internal/api/clientgrpc"
	"github.com/torchwooddev/torchwood/internal/api/consolegrpc"
	"github.com/torchwooddev/torchwood/internal/api/servergrpc"
	"github.com/torchwooddev/torchwood/internal/domain/audit"
	"github.com/torchwooddev/torchwood/internal/infra/auth"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/pkg/grpc/interceptor"
	"github.com/lynx-go/lynx"
	lynxgrpc "github.com/lynx-go/lynx/server/grpc"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

func NewGRPCServer(
	app lynx.App,
	cfg *config.AppConfig,
	validator *auth.Validator,
	auditRepo audit.Repository,
	account *clientgrpc.AccountService,
	clientDatabases *clientgrpc.DatabasesService,
	clientTeams *clientgrpc.TeamsService,
	health *servergrpc.HealthService,
	projects *servergrpc.ProjectsService,
	storage *servergrpc.StorageService,
	users *servergrpc.UsersService,
	apiKeys *servergrpc.APIKeysService,
	oauthProviders *servergrpc.OAuthProvidersService,
	teams *servergrpc.TeamsService,
	databases *servergrpc.DatabasesService,
	consoleAuth *consolegrpc.AuthService,
) (*lynxgrpc.Server, error) {
	grpcCfg := cfg.GetServer().GetGrpc()
	timeout := parseDuration(grpcCfg.GetTimeout(), 30*time.Second)

	publicMethods, apiKeyMethods, permissionMethods, err := collectMethodsByAccess(
		clientv1.File_client_v1_account_proto,
		clientv1.File_client_v1_databases_proto,
		clientv1.File_client_v1_teams_proto,
		serverv1.File_server_v1_projects_proto,
		serverv1.File_server_v1_health_proto,
		serverv1.File_server_v1_storage_proto,
		serverv1.File_server_v1_users_proto,
		serverv1.File_server_v1_apikeys_proto,
		serverv1.File_server_v1_oauth_providers_proto,
		serverv1.File_server_v1_teams_proto,
		serverv1.File_server_v1_databases_proto,
		consolev1.File_console_v1_auth_proto,
	)
	if err != nil {
		return nil, err
	}

	authInterceptor, err := interceptor.NewAuthInterceptor(validator, publicMethods, apiKeyMethods, permissionMethods)
	if err != nil {
		return nil, err
	}
	auditInterceptor := interceptor.NewAuditInterceptor(auditRepo)
	trustedProxies, err := interceptor.ParseTrustedProxies(cfg.GetSecurity().GetTrustedProxies())
	if err != nil {
		return nil, fmt.Errorf("parse security.trusted_proxies: %w", err)
	}
	clientInfoInterceptor := interceptor.NewClientInfoInterceptor(trustedProxies)

	srv := lynxgrpc.NewServer(
		lynxgrpc.WithAddr(grpcCfg.GetAddr()),
		lynxgrpc.WithTimeout(timeout),
		lynxgrpc.WithLogger(app.Logger()),
		lynxgrpc.WithInterceptors(
			clientInfoInterceptor.UnaryMiddleware,
			authInterceptor.UnaryAuthMiddleware,
			auditInterceptor.UnaryAuditMiddleware,
		),
	)
	grpcSrv := srv.GetServer()

	clientv1.RegisterAccountServiceServer(grpcSrv, account)
	clientv1.RegisterDatabasesServiceServer(grpcSrv, clientDatabases)
	clientv1.RegisterTeamsServiceServer(grpcSrv, clientTeams)
	serverv1.RegisterHealthServiceServer(grpcSrv, health)
	serverv1.RegisterProjectsServiceServer(grpcSrv, projects)
	serverv1.RegisterStorageServiceServer(grpcSrv, storage)
	serverv1.RegisterUsersServiceServer(grpcSrv, users)
	serverv1.RegisterAPIKeysServiceServer(grpcSrv, apiKeys)
	serverv1.RegisterOAuthProvidersServiceServer(grpcSrv, oauthProviders)
	serverv1.RegisterTeamsServiceServer(grpcSrv, teams)
	serverv1.RegisterDatabasesServiceServer(grpcSrv, databases)
	consolev1.RegisterConsoleAuthServiceServer(grpcSrv, consoleAuth)

	// fail-closed：所有已注册方法都必须带有 authz 注解，缺失的方法会在拦截器里被放行。
	if err := assertRegisteredMethodsHaveAuthz(grpcSrv, publicMethods, apiKeyMethods, permissionMethods); err != nil {
		return nil, err
	}

	return srv, nil
}

// authzExemptServicePrefixes 是不参与业务 authz 注解校验的 gRPC 框架内置服务白名单：
// grpc.health.v1.Health 由 lynx 注册用于健康检查，grpc.reflection.* 用于 server reflection，
// 它们不是业务 API、不携带业务 authz 注解，由部署层网络策略保护。
var authzExemptServicePrefixes = []string{
	"grpc.health.v1.",
	"grpc.reflection.",
}

// assertRegisteredMethodsHaveAuthz 断言每个已注册的 gRPC 方法都存在于某一类 access map
// （public/apiKey/permission 之一）。漏配的方法不在任何 map 中，会在拦截器
// "len(perms)==0 跳过"分支被任意有效凭证放行，因此启动期直接报错（fail-closed）。
func assertRegisteredMethodsHaveAuthz(grpcSrv *grpc.Server, publicMethods, apiKeyMethods []string, permissionMethods map[string][]string) error {
	covered := make(map[string]struct{}, len(publicMethods)+len(apiKeyMethods)+len(permissionMethods))
	for _, m := range publicMethods {
		covered[m] = struct{}{}
	}
	for _, m := range apiKeyMethods {
		covered[m] = struct{}{}
	}
	for m := range permissionMethods {
		covered[m] = struct{}{}
	}

	var missing []string
	for serviceName, info := range grpcSrv.GetServiceInfo() {
		exempt := false
		for _, prefix := range authzExemptServicePrefixes {
			if strings.HasPrefix(serviceName, prefix) {
				exempt = true
				break
			}
		}
		if exempt {
			continue
		}
		for _, m := range info.Methods {
			fullMethod := "/" + serviceName + "/" + m.Name
			if _, ok := covered[fullMethod]; !ok {
				missing = append(missing, fullMethod)
			}
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("registered grpc methods missing authz annotation: %s", strings.Join(missing, ", "))
	}
	return nil
}

func collectMethodsByAccess(fileDescs ...protoreflect.FileDescriptor) (publicMethods []string, apiKeyMethods []string, permissionMethods map[string][]string, err error) {
	permissionMethods = make(map[string][]string)
	for _, fileDesc := range fileDescs {
		services := fileDesc.Services()
		for i := 0; i < services.Len(); i++ {
			service := services.Get(i)
			serviceDefault := resolveServiceDefaultAccess(service)
			methods := service.Methods()
			for j := 0; j < methods.Len(); j++ {
				method := methods.Get(j)
				access, perms, ok := resolveMethodAccess(method, serviceDefault)
				if !ok || access == sharedv1.AccessLevel_ACCESS_LEVEL_UNSPECIFIED {
					return nil, nil, nil, fmt.Errorf("missing auth policy for method %s/%s", service.FullName(), method.Name())
				}
				fullMethod := fmt.Sprintf("/%s/%s", service.FullName(), method.Name())
				switch access {
				case sharedv1.AccessLevel_ACCESS_PUBLIC:
					publicMethods = append(publicMethods, fullMethod)
				case sharedv1.AccessLevel_ACCESS_API_KEY:
					apiKeyMethods = append(apiKeyMethods, fullMethod)
				case sharedv1.AccessLevel_ACCESS_AUTHENTICATED:
					if len(perms) == 0 {
						perms = []string{"users"}
					}
					permissionMethods[fullMethod] = perms
				case sharedv1.AccessLevel_ACCESS_PERMISSION:
					permissionMethods[fullMethod] = perms
				}
			}
		}
	}
	return publicMethods, apiKeyMethods, permissionMethods, nil
}

func resolveServiceDefaultAccess(service protoreflect.ServiceDescriptor) sharedv1.AccessLevel {
	options, ok := service.Options().(*descriptorpb.ServiceOptions)
	if !ok || options == nil || !proto.HasExtension(options, sharedv1.E_ServiceAuth) {
		return sharedv1.AccessLevel_ACCESS_LEVEL_UNSPECIFIED
	}
	ext := proto.GetExtension(options, sharedv1.E_ServiceAuth)
	policy, ok := ext.(*sharedv1.ServiceAuth)
	if !ok {
		return sharedv1.AccessLevel_ACCESS_LEVEL_UNSPECIFIED
	}
	return policy.GetDefaultAccess()
}

func resolveMethodAccess(method protoreflect.MethodDescriptor, serviceDefault sharedv1.AccessLevel) (sharedv1.AccessLevel, []string, bool) {
	options, ok := method.Options().(*descriptorpb.MethodOptions)
	if ok && options != nil && proto.HasExtension(options, sharedv1.E_MethodAuth) {
		ext := proto.GetExtension(options, sharedv1.E_MethodAuth)
		policy, ok := ext.(*sharedv1.MethodAuth)
		if ok && policy.GetAccess() != sharedv1.AccessLevel_ACCESS_LEVEL_UNSPECIFIED {
			return policy.GetAccess(), policy.GetPermissions(), true
		}
	}
	if serviceDefault != sharedv1.AccessLevel_ACCESS_LEVEL_UNSPECIFIED {
		return serviceDefault, nil, true
	}
	return sharedv1.AccessLevel_ACCESS_LEVEL_UNSPECIFIED, nil, false
}

func parseDuration(s string, fallback time.Duration) time.Duration {
	if s == "" {
		return fallback
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}
