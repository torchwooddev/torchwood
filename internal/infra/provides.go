package infra

import (
	"github.com/google/wire"
	domainauth "github.com/torchwooddev/torchwood/internal/domain/auth"
	domainidgen "github.com/torchwooddev/torchwood/internal/domain/idgen"
	"github.com/torchwooddev/torchwood/internal/infra/auth"
	"github.com/torchwooddev/torchwood/internal/infra/bun"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	infraevents "github.com/torchwooddev/torchwood/internal/infra/events"
	infrafunctions "github.com/torchwooddev/torchwood/internal/infra/functions"
	"github.com/torchwooddev/torchwood/internal/infra/health"
	infraidgen "github.com/torchwooddev/torchwood/internal/infra/idgen"
	inframessaging "github.com/torchwooddev/torchwood/internal/infra/messaging"
	infrapayments "github.com/torchwooddev/torchwood/internal/infra/payments"
	infraqueue "github.com/torchwooddev/torchwood/internal/infra/queue"
	infrarealtime "github.com/torchwooddev/torchwood/internal/infra/realtime"
	"github.com/torchwooddev/torchwood/internal/infra/server"
	infrastorage "github.com/torchwooddev/torchwood/internal/infra/storage"
)

var ProviderSet = wire.NewSet(
	clients.NewDataClients,
	clients.NewDatabase,
	clients.NewRedis,
	health.NewCheckers,

	auth.NewValidatorWithOneTimeTokens,
	auth.NewSessionService,
	auth.NewRedisOTPChallengeStore,
	auth.NewRedisOAuthStateStore,
	auth.NewRedisAccountTokenStore,
	auth.NewRedisAdminTokenRevokeStore,
	auth.NewRedisLoginThrottle,
	auth.NewRedisRefreshRotationStore,
	auth.NewRedisRateLimiter,
	auth.NewTOTPService,
	auth.NewRedisMFAChallengeStore,
	auth.NewRedisOneTimeTokenStore,
	wire.Bind(new(domainauth.SessionService), new(*auth.SessionService)),
	wire.Bind(new(domainauth.OTPChallengeStore), new(*auth.RedisOTPChallengeStore)),
	wire.Bind(new(domainauth.OAuthStateStore), new(*auth.RedisOAuthStateStore)),
	wire.Bind(new(domainauth.AccountTokenStore), new(*auth.RedisAccountTokenStore)),
	wire.Bind(new(domainauth.AdminTokenRevokeStore), new(*auth.RedisAdminTokenRevokeStore)),
	wire.Bind(new(domainauth.LoginThrottle), new(*auth.RedisLoginThrottle)),
	wire.Bind(new(domainauth.RefreshRotationStore), new(*auth.RedisRefreshRotationStore)),
	wire.Bind(new(domainauth.RateLimiter), new(*auth.RedisRateLimiter)),
	wire.Bind(new(domainauth.OneTimeTokenStore), new(*auth.RedisOneTimeTokenStore)),

	infraidgen.ProviderSet,
	wire.Bind(new(domainidgen.Generator), new(*infraidgen.Service)),

	inframessaging.ProviderSet,

	bun.ProviderSet,
	documentdb.ProviderSet,
	infraevents.ProviderSet,
	infrarealtime.ProviderSet,
	infrastorage.ProviderSet,
	infrafunctions.ProviderSet,
	infrapayments.ProviderSet,
	infraqueue.ProviderSet,

	server.NewGRPCServer,
	server.NewGRPCGatewayServer,
	server.NewMetricsServer,
)
