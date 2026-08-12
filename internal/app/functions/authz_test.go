package functions

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func strPtr(s string) *string { return &s }

// platformAdminCtx 返回携带平台 admin principal 的上下文（G2-1 后 functions
// 写方法 use-case 层要求平台 admin，测试需显式注入）。
func platformAdminCtx() context.Context {
	return contexts.WithPrincipal(context.Background(), &shared.Principal{
		ActorID:         "admin-1",
		ActorKind:       shared.ActorKindAdmin,
		IsPlatformAdmin: true,
		UserID:          "admin-1",
		Roles:           []string{"owner"},
	})
}

// G2-1 纵深防御：functions 全部写方法对受限主体（viewer/member admin、
// API key、端用户、匿名）必须 PermissionDenied/Unauthenticated，
// 平台 admin 放行进入业务校验。
func TestFunctionsWriteMethods_RequirePlatformAdmin(t *testing.T) {
	repo := newMockRepo()
	seedReadyFunction(repo, "p1", "fn_1", true, 15)
	uc := newTestUC(newMockExecutor(nil, nil), repo, newMockQueue())

	restricted := []*shared.Principal{
		{ActorID: "admin-2", ActorKind: shared.ActorKindAdmin, UserID: "admin-2", Roles: []string{"viewer"}},
		{ActorID: "admin-3", ActorKind: shared.ActorKindAdmin, UserID: "admin-3", Roles: []string{"member"}},
		{ActorID: "key-1", ActorKind: shared.ActorKindService, Roles: []string{"keys"}, Permissions: []string{"functions.write"}},
		{ActorID: "user-1", ActorKind: shared.ActorKindEndUser, UserID: "user-1"},
	}
	base := CreateFunctionCommand{ID: "fn_new", ProjectID: "p1", Name: "f", Runtime: "node-18.0", TimeoutSeconds: timeoutPtr(15)}

	for _, p := range restricted {
		ctx := contexts.WithPrincipal(context.Background(), p)

		_, err := uc.CreateFunction(ctx, base)
		require.Equal(t, codes.PermissionDenied, status.Code(err), "%+v CreateFunction 应被拒", p)

		_, err = uc.UpdateFunction(ctx, UpdateFunctionCommand{ProjectID: "p1", FunctionID: "fn_1", Name: strPtr("x")})
		require.Equal(t, codes.PermissionDenied, status.Code(err), "%+v UpdateFunction 应被拒", p)

		err = uc.DeleteFunction(ctx, "p1", "fn_1")
		require.Equal(t, codes.PermissionDenied, status.Code(err), "%+v DeleteFunction 应被拒", p)

		_, err = uc.CreateDeployment(ctx, CreateDeploymentCommand{ProjectID: "p1", FunctionID: "fn_1", Code: []byte("PK\x03\x04code")})
		require.Equal(t, codes.PermissionDenied, status.Code(err), "%+v CreateDeployment 应被拒", p)

		err = uc.DeleteDeployment(ctx, "p1", "fn_1", "dep_ready")
		require.Equal(t, codes.PermissionDenied, status.Code(err), "%+v DeleteDeployment 应被拒", p)

		_, err = uc.SetVariables(ctx, "p1", "fn_1", map[string]string{"A": "1"})
		require.Equal(t, codes.PermissionDenied, status.Code(err), "%+v SetVariables 应被拒", p)

		_, err = uc.CreateExecution(ctx, CreateExecutionCommand{ProjectID: "p1", FunctionID: "fn_1"})
		require.Equal(t, codes.PermissionDenied, status.Code(err), "%+v CreateExecution 应被拒", p)
	}

	// 匿名（无 principal）→ Unauthenticated。
	_, err := uc.CreateFunction(context.Background(), base)
	require.Equal(t, codes.Unauthenticated, status.Code(err))

	// 平台 admin 放行：进入业务校验（无效 ID 返回 InvalidArgument 证明守卫已过）。
	_, err = uc.CreateFunction(platformAdminCtx(), CreateFunctionCommand{ID: "bad id!", ProjectID: "p1", Name: "f", Runtime: "node-18.0"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

// G2-1：读方法（GetVariables/GetDeployment/List）不受平台 admin 守卫影响，
// 受限主体仍可读。
func TestFunctionsReadMethods_NotGatedByPlatformAdmin(t *testing.T) {
	repo := newMockRepo()
	seedReadyFunction(repo, "p1", "fn_1", true, 15)
	uc := newTestUC(newMockExecutor(nil, nil), repo, newMockQueue())
	ctx := contexts.WithPrincipal(context.Background(), &shared.Principal{
		ActorID: "admin-2", ActorKind: shared.ActorKindAdmin, UserID: "admin-2", Roles: []string{"viewer"},
	})

	_, err := uc.GetVariables(ctx, "p1", "fn_1")
	require.NoError(t, err, "GetVariables 返回掩码值，viewer 可读")

	_, err = uc.GetDeployment(ctx, "p1", "fn_1", "dep_ready")
	require.NoError(t, err, "GetDeployment viewer 可读")

	fns, err := uc.ListFunctions(ctx, "p1")
	require.NoError(t, err)
	require.Len(t, fns, 1)
}
