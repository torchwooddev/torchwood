package interceptor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// G2-1/G2-2：adminRoleMethodRules 方法覆盖断言（与 proto RPC 清单逐一核对，
// 防止新增写方法漏登记——漏登的方法对 viewer/member 放行，构成提权）。
func TestAdminRoleMethodRules_FunctionsWriteMethodsRequireOwnerAdmin(t *testing.T) {
	t.Parallel()
	writeMethods := []string{
		"/torchwood.server.v1.FunctionsService/CreateFunction",
		"/torchwood.server.v1.FunctionsService/UpdateFunction",
		"/torchwood.server.v1.FunctionsService/DeleteFunction",
		"/torchwood.server.v1.FunctionsService/CreateDeployment",
		"/torchwood.server.v1.FunctionsService/DeleteDeployment",
		"/torchwood.server.v1.FunctionsService/SetVariables",
		"/torchwood.server.v1.FunctionsService/CreateExecution",
	}
	for _, m := range writeMethods {
		roles := adminRoleMethodRules[m]
		require.NotNil(t, roles, "functions 写方法 %s 必须登记 adminRoleMethodRules", m)
		require.Contains(t, roles, "owner", "%s 必须允许 owner", m)
		require.Contains(t, roles, "admin", "%s 必须允许 admin", m)
		require.NotContains(t, roles, "member", "%s 不得允许 member", m)
		require.NotContains(t, roles, "viewer", "%s 不得允许 viewer", m)
	}

	// 读方法（含返回掩码值的 GetVariables）viewer 可读：不得登记受限规则。
	for _, m := range []string{
		"/torchwood.server.v1.FunctionsService/ListRuntimes",
		"/torchwood.server.v1.FunctionsService/ListSpecifications",
		"/torchwood.server.v1.FunctionsService/ListFunctions",
		"/torchwood.server.v1.FunctionsService/GetFunction",
		"/torchwood.server.v1.FunctionsService/ListDeployments",
		"/torchwood.server.v1.FunctionsService/GetDeployment",
		"/torchwood.server.v1.FunctionsService/GetVariables",
		"/torchwood.server.v1.FunctionsService/ListExecutions",
		"/torchwood.server.v1.FunctionsService/GetExecution",
	} {
		require.NotContains(t, adminRoleMethodRules, m, "functions 读方法 %s 不得登记受限规则", m)
	}
}

func TestAdminRoleMethodRules_BusinessWriteMethodsAllowMember(t *testing.T) {
	t.Parallel()
	// CreateUser/CreateBucket 是业务写（member 可做）；UpdateProject 保持
	// 现有语义仅收 viewer；三者均拒绝 viewer。
	for _, m := range []string{
		"/torchwood.server.v1.UsersService/CreateUser",
		"/torchwood.server.v1.StorageService/CreateBucket",
		"/torchwood.server.v1.ProjectsService/UpdateProject",
	} {
		roles := adminRoleMethodRules[m]
		require.NotNil(t, roles, "%s 必须登记 adminRoleMethodRules", m)
		for _, allowed := range []string{"member", "owner", "admin"} {
			require.Contains(t, roles, allowed, "%s 必须允许 %s", m, allowed)
		}
		require.NotContains(t, roles, "viewer", "%s 不得允许 viewer", m)
	}

	// DeleteUserSession 是管理员操作，仅 owner/admin。
	roles := adminRoleMethodRules["/torchwood.server.v1.UsersService/DeleteUserSession"]
	require.NotNil(t, roles)
	require.Contains(t, roles, "owner")
	require.Contains(t, roles, "admin")
	require.NotContains(t, roles, "member")
	require.NotContains(t, roles, "viewer")
}

// G2-1：viewer/member admin 调 Functions 全部写方法必须 PermissionDenied。
func TestAuthInterceptor_RejectsViewerOrMemberOnFunctionsWriteMethods(t *testing.T) {
	t.Parallel()
	methods := []string{
		"/torchwood.server.v1.FunctionsService/CreateFunction",
		"/torchwood.server.v1.FunctionsService/UpdateFunction",
		"/torchwood.server.v1.FunctionsService/DeleteFunction",
		"/torchwood.server.v1.FunctionsService/CreateDeployment",
		"/torchwood.server.v1.FunctionsService/DeleteDeployment",
		"/torchwood.server.v1.FunctionsService/SetVariables",
		"/torchwood.server.v1.FunctionsService/CreateExecution",
	}
	for _, role := range []string{"viewer", "member"} {
		for _, method := range methods {
			ic, err := NewAuthInterceptor(stubValidator{principal: &shared.Principal{
				ActorKind:      shared.ActorKindAdmin,
				CredentialType: shared.CredentialTypeSession,
				Roles:          []string{role},
			}}, nil, []string{method}, nil)
			requireNoError(t, err)

			ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Session admin-token"))
			_, err = ic.UnaryAuthMiddleware(ctx, nil, &grpc.UnaryServerInfo{
				FullMethod: method,
			}, func(context.Context, any) (any, error) {
				t.Fatalf("handler should not run for role %s on %s", role, method)
				return nil, nil
			})
			requirePermissionDenied(t, err)
		}
	}
}

// G2-1：owner/admin 调 Functions 写方法放行（含 GetVariables 读方法）。
func TestAuthInterceptor_AllowsOwnerAdminOnFunctionsWriteMethods(t *testing.T) {
	t.Parallel()
	for _, role := range []string{"owner", "admin"} {
		for _, method := range []string{
			"/torchwood.server.v1.FunctionsService/CreateFunction",
			"/torchwood.server.v1.FunctionsService/UpdateFunction",
			"/torchwood.server.v1.FunctionsService/DeleteFunction",
			"/torchwood.server.v1.FunctionsService/CreateDeployment",
			"/torchwood.server.v1.FunctionsService/DeleteDeployment",
			"/torchwood.server.v1.FunctionsService/SetVariables",
			"/torchwood.server.v1.FunctionsService/CreateExecution",
			"/torchwood.server.v1.FunctionsService/GetVariables",
		} {
			ic, err := NewAuthInterceptor(stubValidator{principal: &shared.Principal{
				ActorKind:      shared.ActorKindAdmin,
				CredentialType: shared.CredentialTypeSession,
				Roles:          []string{role},
			}}, nil, []string{method}, nil)
			requireNoError(t, err)

			ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Session admin-token"))
			called := false
			_, err = ic.UnaryAuthMiddleware(ctx, nil, &grpc.UnaryServerInfo{
				FullMethod: method,
			}, func(context.Context, any) (any, error) {
				called = true
				return "ok", nil
			})
			requireNoError(t, err)
			if !called {
				t.Fatalf("expected handler to run for role %s on %s", role, method)
			}
		}
	}
}

// G2-2：viewer 调业务写方法（CreateUser/CreateBucket/UpdateProject）与
// DeleteUserSession 必须 PermissionDenied。
func TestAuthInterceptor_RejectsViewerOnGatedWriteMethods(t *testing.T) {
	t.Parallel()
	methods := []string{
		"/torchwood.server.v1.UsersService/CreateUser",
		"/torchwood.server.v1.StorageService/CreateBucket",
		"/torchwood.server.v1.ProjectsService/UpdateProject",
		"/torchwood.server.v1.UsersService/DeleteUserSession",
	}
	for _, method := range methods {
		ic, err := NewAuthInterceptor(stubValidator{principal: &shared.Principal{
			ActorKind:      shared.ActorKindAdmin,
			CredentialType: shared.CredentialTypeSession,
			Roles:          []string{"viewer"},
		}}, nil, []string{method}, nil)
		requireNoError(t, err)

		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Session admin-token"))
		_, err = ic.UnaryAuthMiddleware(ctx, nil, &grpc.UnaryServerInfo{
			FullMethod: method,
		}, func(context.Context, any) (any, error) {
			t.Fatalf("handler should not run on %s", method)
			return nil, nil
		})
		requirePermissionDenied(t, err)
	}
}

// G2-2：member 可写业务资源（CreateUser/CreateBucket/UpdateProject 放行），
// 但 DeleteUserSession（管理员操作）拒绝。
func TestAuthInterceptor_MemberAllowedOnBusinessWritesDeniedOnAdminOp(t *testing.T) {
	t.Parallel()
	for _, method := range []string{
		"/torchwood.server.v1.UsersService/CreateUser",
		"/torchwood.server.v1.StorageService/CreateBucket",
		"/torchwood.server.v1.ProjectsService/UpdateProject",
	} {
		ic, err := NewAuthInterceptor(stubValidator{principal: &shared.Principal{
			ActorKind:      shared.ActorKindAdmin,
			CredentialType: shared.CredentialTypeSession,
			Roles:          []string{"member"},
		}}, nil, []string{method}, nil)
		requireNoError(t, err)

		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Session admin-token"))
		called := false
		_, err = ic.UnaryAuthMiddleware(ctx, nil, &grpc.UnaryServerInfo{
			FullMethod: method,
		}, func(context.Context, any) (any, error) {
			called = true
			return "ok", nil
		})
		if err != nil {
			t.Fatalf("member 应可调 %s: %v", method, err)
		}
		if !called {
			t.Fatalf("expected handler to run on %s", method)
		}
	}

	ic, err := NewAuthInterceptor(stubValidator{principal: &shared.Principal{
		ActorKind:      shared.ActorKindAdmin,
		CredentialType: shared.CredentialTypeSession,
		Roles:          []string{"member"},
	}}, nil, []string{"/torchwood.server.v1.UsersService/DeleteUserSession"}, nil)
	requireNoError(t, err)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Session admin-token"))
	_, err = ic.UnaryAuthMiddleware(ctx, nil, &grpc.UnaryServerInfo{
		FullMethod: "/torchwood.server.v1.UsersService/DeleteUserSession",
	}, func(context.Context, any) (any, error) {
		t.Fatal("handler should not run")
		return nil, nil
	})
	requirePermissionDenied(t, err)
}
