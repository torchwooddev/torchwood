package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

func TestInvokeJSONRoundTrip(t *testing.T) {
	lis, rec := newBufconn(t)
	c := newTestClient(t, lis, WithAPIKey("k"))
	out, err := c.InvokeJSON(context.Background(), "/torchwood.server.v1.HealthService/Check", nil)
	require.NoError(t, err)
	// protojson Multiline 的 key: 后空格数由 detrand 随机（1 或 2 个），
	// 与 CLI 历史输出同一实现，格式一致；断言不依赖空格数。
	require.Regexp(t, `"status":\s+"ok"`, string(out))
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

// TestInvokeJSONCompleteness 遍历 protoregistry.GlobalFiles 中 torchwood.server.v1
// 包的全部方法（排除 APIKeysService），断言每个方法都能被解析并用空 JSON 构造
// 请求——防包名白名单/新增 proto 文件回归。
func TestInvokeJSONCompleteness(t *testing.T) {
	count := 0
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if fd.Package() != "torchwood.server.v1" {
			return true
		}
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
		return true
	})
	require.Greater(t, count, 60) // 当前 80 个（除 APIKeysService），留余量防误删
}
