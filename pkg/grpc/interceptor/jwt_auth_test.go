package interceptor

import (
	"context"
	"testing"

	"github.com/torchwoodio/torchwood/internal/domain/shared"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type stubValidator struct {
	principal *shared.Principal
}

func (s stubValidator) ValidateToken(_ context.Context, _ string) (*shared.Principal, error) {
	return s.principal, nil
}

func (s stubValidator) ValidateCredential(_ context.Context, _ string, _ shared.CredentialType) (*shared.Principal, error) {
	return s.principal, nil
}

func (s stubValidator) ValidateAdminProjectAccess(context.Context, *shared.Principal) error {
	return nil
}

func TestAuthInterceptor_RejectsAPIKeyOnUsersPermissionMethod(t *testing.T) {
	t.Parallel()

	ic, err := NewAuthInterceptor(stubValidator{principal: &shared.Principal{
		ActorKind:      shared.ActorKindService,
		CredentialType: shared.CredentialTypeAPIKey,
		Roles:          []string{"keys"},
		Permissions:    []string{"projects.read"},
	}}, nil, nil, map[string][]string{
		"/torchwood.client.v1.TeamsService/CreateTeam": {"users"},
	})
	requireNoError(t, err)

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-api-key", "test-key"))
	_, err = ic.UnaryAuthMiddleware(ctx, nil, &grpc.UnaryServerInfo{
		FullMethod: "/torchwood.client.v1.TeamsService/CreateTeam",
	}, func(context.Context, any) (any, error) {
		t.Fatal("handler should not run")
		return nil, nil
	})
	requirePermissionDenied(t, err)
}

func TestAuthInterceptor_AllowsEndUserOnUsersPermissionMethod(t *testing.T) {
	t.Parallel()

	ic, err := NewAuthInterceptor(stubValidator{principal: &shared.Principal{
		ActorKind: shared.ActorKindEndUser,
		UserID:    "user-1",
		Roles:     []string{"users", "user:user-1"},
	}}, nil, nil, map[string][]string{
		"/torchwood.client.v1.TeamsService/CreateTeam": {"users"},
	})
	requireNoError(t, err)

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer token"))
	called := false
	_, err = ic.UnaryAuthMiddleware(ctx, nil, &grpc.UnaryServerInfo{
		FullMethod: "/torchwood.client.v1.TeamsService/CreateTeam",
	}, func(context.Context, any) (any, error) {
		called = true
		return "ok", nil
	})
	requireNoError(t, err)
	if !called {
		t.Fatal("expected handler to run")
	}
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func requirePermissionDenied(t *testing.T, err error) {
	t.Helper()
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.PermissionDenied {
		t.Fatalf("expected permission denied, got %v", err)
	}
}

func TestAuthInterceptor_DeniesAPIKeyOnAPIKeysService(t *testing.T) {
	t.Parallel()

	ic, err := NewAuthInterceptor(stubValidator{principal: &shared.Principal{
		ActorKind:      shared.ActorKindService,
		CredentialType: shared.CredentialTypeAPIKey,
		Roles:          []string{"keys"},
		Permissions:    []string{"*"},
	}}, nil, []string{"/torchwood.server.v1.APIKeysService/CreateAPIKey"}, nil)
	requireNoError(t, err)

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-api-key", "test-key"))
	_, err = ic.UnaryAuthMiddleware(ctx, nil, &grpc.UnaryServerInfo{
		FullMethod: "/torchwood.server.v1.APIKeysService/CreateAPIKey",
	}, func(context.Context, any) (any, error) {
		t.Fatal("handler should not run")
		return nil, nil
	})
	requirePermissionDenied(t, err)
}

func TestAuthInterceptor_AllowsAdminSessionOnAPIKeysService(t *testing.T) {
	t.Parallel()

	ic, err := NewAuthInterceptor(stubValidator{principal: &shared.Principal{
		ActorKind:      shared.ActorKindAdmin,
		CredentialType: shared.CredentialTypeSession,
		Roles:          []string{"admin"},
	}}, nil, []string{"/torchwood.server.v1.APIKeysService/CreateAPIKey"}, nil)
	requireNoError(t, err)

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Session admin-token"))
	called := false
	_, err = ic.UnaryAuthMiddleware(ctx, nil, &grpc.UnaryServerInfo{
		FullMethod: "/torchwood.server.v1.APIKeysService/CreateAPIKey",
	}, func(context.Context, any) (any, error) {
		called = true
		return "ok", nil
	})
	requireNoError(t, err)
	if !called {
		t.Fatal("expected handler to run")
	}
}
