package testutil

import (
	"context"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/infra/auth"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/pkg/grpc/interceptor"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const (
	MethodHealthCheck    = "/torchwood.server.v1.HealthService/Check"
	MethodListUsers      = "/torchwood.server.v1.UsersService/ListUsers"
	MethodAccountMe      = "/torchwood.client.v1.AccountService/Me"
	MethodAccountSignOut = "/torchwood.client.v1.AccountService/SignOut"
)

// InterceptorEnv wires auth + audit interceptors the same way production does.
type InterceptorEnv struct {
	DB        *clients.Database
	Validator *auth.Validator
	Auth      *interceptor.AuthInterceptor
	Audit     *interceptor.AuditInterceptor
}

func NewInterceptorEnv(db *clients.Database, cfg *config.AppConfig, docDB databases.DocumentDB) (*InterceptorEnv, error) {
	validator := auth.NewValidator(
		cfg,
		bunrepo.NewAPIKeyRepository(db),
		bunrepo.NewAdminRepository(db),
		bunrepo.NewAdminProjectRepository(db),
		nil,
		docDB,
		nil,
	)
	authIC, err := interceptor.NewAuthInterceptor(
		validator,
		[]string{MethodHealthCheck},
		[]string{MethodListUsers},
		map[string][]string{
			MethodAccountMe:      {"users"},
			MethodAccountSignOut: {"users"},
		},
	)
	if err != nil {
		return nil, err
	}
	return &InterceptorEnv{
		DB:        db,
		Validator: validator,
		Auth:      authIC,
		Audit:     interceptor.NewAuditInterceptor(bunrepo.NewAuditRepository(db)),
	}, nil
}

// InvokeUnary runs auth -> audit -> handler for the given gRPC method and metadata.
func (e *InterceptorEnv) InvokeUnary(ctx context.Context, method string, md metadata.MD) error {
	ctx = metadata.NewIncomingContext(ctx, md)
	info := &grpc.UnaryServerInfo{FullMethod: method}
	handler := func(ctx context.Context, req any) (any, error) { return nil, nil }
	auditHandler := func(ctx context.Context, req any) (any, error) {
		return e.Audit.UnaryAuditMiddleware(ctx, req, info, handler)
	}
	_, err := e.Auth.UnaryAuthMiddleware(ctx, nil, info, auditHandler)
	return err
}

func (e *InterceptorEnv) AuditLogCount(ctx context.Context) (int, error) {
	return e.DB.NewSelect().Model((*model.AuditLog)(nil)).Count(ctx)
}

func (e *InterceptorEnv) LatestAuditLog(ctx context.Context) (*model.AuditLog, error) {
	row := new(model.AuditLog)
	err := e.DB.NewSelect().Model(row).Order("created_at DESC").Limit(1).Scan(ctx)
	if err != nil {
		return nil, err
	}
	return row, nil
}
