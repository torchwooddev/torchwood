package server

import (
	"context"
	"testing"

	clientv1 "github.com/torchwooddev/torchwood/genproto/client/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestCollectMethodsByAccess_AuthenticatedRequiresUsersRole(t *testing.T) {
	t.Parallel()

	_, _, permissionMethods, err := collectMethodsByAccess(clientv1.File_client_v1_teams_proto)
	require.NoError(t, err)

	fullMethod := "/torchwood.client.v1.TeamsService/CreateTeam"
	perms, ok := permissionMethods[fullMethod]
	require.True(t, ok, "TeamsService methods should require users permission")
	require.Equal(t, []string{"users"}, perms)
}

func TestCollectMethodsByAccess_AccountPublicMethods(t *testing.T) {
	t.Parallel()

	publicMethods, _, permissionMethods, err := collectMethodsByAccess(clientv1.File_client_v1_account_proto)
	require.NoError(t, err)

	require.Contains(t, publicMethods, "/torchwood.client.v1.AccountService/SignIn")
	require.Equal(t, []string{"users"}, permissionMethods["/torchwood.client.v1.AccountService/Me"])
}

// fakeUnaryHandler 满足 grpc.MethodDesc 的 Handler 签名，仅用于注册测试服务。
func fakeUnaryHandler(any, context.Context, func(any) error, grpc.UnaryServerInterceptor) (any, error) {
	return nil, nil
}

func newServerWithServices(serviceNames ...string) *grpc.Server {
	srv := grpc.NewServer()
	for _, name := range serviceNames {
		srv.RegisterService(&grpc.ServiceDesc{
			ServiceName: name,
			Methods:     []grpc.MethodDesc{{MethodName: "DoThing", Handler: fakeUnaryHandler}},
		}, nil)
	}
	return srv
}

func TestAssertRegisteredMethodsHaveAuthz(t *testing.T) {
	t.Parallel()

	t.Run("all methods covered", func(t *testing.T) {
		t.Parallel()
		srv := newServerWithServices("torchwood.test.v1.PublicService", "torchwood.test.v1.KeyService", "torchwood.test.v1.PermService")
		err := assertRegisteredMethodsHaveAuthz(srv,
			[]string{"/torchwood.test.v1.PublicService/DoThing"},
			[]string{"/torchwood.test.v1.KeyService/DoThing"},
			map[string][]string{"/torchwood.test.v1.PermService/DoThing": {"users"}})
		require.NoError(t, err)
	})

	t.Run("unannotated method fails closed", func(t *testing.T) {
		t.Parallel()
		srv := newServerWithServices("torchwood.test.v1.UnannotatedService")
		err := assertRegisteredMethodsHaveAuthz(srv, nil, nil, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "/torchwood.test.v1.UnannotatedService/DoThing")
	})

	t.Run("framework services are exempt", func(t *testing.T) {
		t.Parallel()
		srv := newServerWithServices("grpc.health.v1.Health", "grpc.reflection.v1.ServerReflection", "grpc.reflection.v1alpha.ServerReflection")
		require.NoError(t, assertRegisteredMethodsHaveAuthz(srv, nil, nil, nil))
	})
}
