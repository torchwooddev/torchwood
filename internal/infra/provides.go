package infra

import (
	"github.com/google/wire"
	domainauth "github.com/torchwooddev/torchwood/internal/domain/auth"
	domainidgen "github.com/torchwooddev/torchwood/internal/domain/idgen"
	"github.com/torchwooddev/torchwood/internal/infra/auth"
	"github.com/torchwooddev/torchwood/internal/infra/bun"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	infrafunctions "github.com/torchwooddev/torchwood/internal/infra/functions"
	infraidgen "github.com/torchwooddev/torchwood/internal/infra/idgen"
	inframessaging "github.com/torchwooddev/torchwood/internal/infra/messaging"
	"github.com/torchwooddev/torchwood/internal/infra/server"
	infrastorage "github.com/torchwooddev/torchwood/internal/infra/storage"
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
)
