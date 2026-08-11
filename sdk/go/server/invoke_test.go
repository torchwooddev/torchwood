package server

import (
	"context"
	"testing"

	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestInvokeJSONRoundTrip(t *testing.T) {
	lis, rec := newBufconn(t)
	c := newTestClient(t, lis, WithAPIKey("k"))
	out, err := c.InvokeJSON(context.Background(), "/torchwood.server.v1.HealthService/Check", nil)
	require.NoError(t, err)
	require.Contains(t, string(out), `"status": "ok"`)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	require.Equal(t, []string{"k"}, rec.md.Get("x-api-key"))
}

func TestInvokeJSONUnknownMethod(t *testing.T) {
	lis, _ := newBufconn(t)
	c := newTestClient(t, lis)
	_, err := c.InvokeJSON(context.Background(), "/torchwood.server.v1.Nope/Check", nil)
	require.ErrorContains(t, err, `torchwood: unknown method "/torchwood.server.v1.Nope/Check"`)
	// APIKeysService 被排除
	_, err = c.InvokeJSON(context.Background(), "/torchwood.server.v1.APIKeysService/ListAPIKeys", nil)
	require.ErrorContains(t, err, "unknown method")
	// 非 server v1 包被拒绝
	_, err = c.InvokeJSON(context.Background(), "/torchwood.client.v1.AccountService/SignIn", nil)
	require.ErrorContains(t, err, "unknown method")
}

func TestInvokeJSONBadJSON(t *testing.T) {
	lis, _ := newBufconn(t)
	c := newTestClient(t, lis)
	_, err := c.InvokeJSON(context.Background(), "/torchwood.server.v1.HealthService/Check", []byte(`{"nope": 1}`))
	require.Error(t, err) // 未知字段报错（DiscardUnknown=false）
}

// TestInvokeJSONCompleteness 遍历 serverv1 全部方法（排除 APIKeysService），
// 断言每个方法都能被解析并用空 JSON 构造请求——防包名白名单回归。
func TestInvokeJSONCompleteness(t *testing.T) {
	count := 0
	for _, fd := range []protoreflect.FileDescriptor{
		serverv1.File_server_v1_health_proto,
		serverv1.File_server_v1_users_proto,
		serverv1.File_server_v1_teams_proto,
		serverv1.File_server_v1_databases_proto,
		serverv1.File_server_v1_projects_proto,
		serverv1.File_server_v1_storage_proto,
		serverv1.File_server_v1_functions_proto,
		serverv1.File_server_v1_oauth_providers_proto,
		serverv1.File_server_v1_apikeys_proto,
	} {
		svcs := fd.Services()
		for i := 0; i < svcs.Len(); i++ {
			svc := svcs.Get(i)
			ms := svc.Methods()
			for j := 0; j < ms.Len(); j++ {
				m := ms.Get(j)
				// gRPC 路径：/pkg.Service/Method（svc.FullName() 为点分隔全名）
				method := "/" + string(svc.FullName()) + "/" + string(m.Name())
				_, err := findServerMethod(method)
				if svc.Name() == "APIKeysService" {
					require.Error(t, err)
					continue
				}
				require.NoError(t, err, "method %s", m.FullName())
				count++
			}
		}
	}
	require.Greater(t, count, 60) // 当前 80 个（除 APIKeysService），留余量防误删
}
