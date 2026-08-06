package infra

import (
	domainauth "github.com/torchwoodio/torchwood/internal/domain/auth"
	domainidgen "github.com/torchwoodio/torchwood/internal/domain/idgen"
	"github.com/torchwoodio/torchwood/internal/infra/auth"
	"github.com/torchwoodio/torchwood/internal/infra/bun"
	"github.com/torchwoodio/torchwood/internal/infra/clients"
	"github.com/torchwoodio/torchwood/internal/infra/documentdb"
	infrafunctions "github.com/torchwoodio/torchwood/internal/infra/functions"
	infraidgen "github.com/torchwoodio/torchwood/internal/infra/idgen"
	inframessaging "github.com/torchwoodio/torchwood/internal/infra/messaging"
	infrastorage "github.com/torchwoodio/torchwood/internal/infra/storage"
	"github.com/torchwoodio/torchwood/internal/infra/server"
	"github.com/google/wire"
)

var ProviderSet = wire.NewSet(
	clients.NewDataClients,
	clients.NewDatabase,
	clients.NewRedis,

	auth.NewValidator,
	auth.NewSessionService,
	auth.NewRedisOTPChallengeStore,
	auth.NewRedisOAuthStateStore,
	auth.NewRedisAccountTokenStore,
	auth.NewRedisAdminTokenRevokeStore,
	auth.NewRedisLoginThrottle,
	auth.NewRedisRefreshRotationStore,
	auth.NewRedisRateLimiter,
	wire.Bind(new(domainauth.SessionService), new(*auth.SessionService)),
	wire.Bind(new(domainauth.OTPChallengeStore), new(*auth.RedisOTPChallengeStore)),
	wire.Bind(new(domainauth.OAuthStateStore), new(*auth.RedisOAuthStateStore)),
	wire.Bind(new(domainauth.AccountTokenStore), new(*auth.RedisAccountTokenStore)),
	wire.Bind(new(domainauth.AdminTokenRevokeStore), new(*auth.RedisAdminTokenRevokeStore)),
	wire.Bind(new(domainauth.LoginThrottle), new(*auth.RedisLoginThrottle)),
	wire.Bind(new(domainauth.RefreshRotationStore), new(*auth.RedisRefreshRotationStore)),
	wire.Bind(new(domainauth.RateLimiter), new(*auth.RedisRateLimiter)),

	infraidgen.ProviderSet,
	wire.Bind(new(domainidgen.Generator), new(*infraidgen.Service)),

	inframessaging.ProviderSet,

	bun.ProviderSet,
	documentdb.ProviderSet,
	infrastorage.ProviderSet,
	infrafunctions.ProviderSet,

	server.NewGRPCServer,
	server.NewGRPCGatewayServer,
	server.NewMetricsServer,
	server.NewHealthCheckFunc,
)
