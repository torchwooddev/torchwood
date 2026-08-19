package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	clientv1 "github.com/torchwooddev/torchwood/genproto/client/v1"
	consolev1 "github.com/torchwooddev/torchwood/genproto/console/v1"
	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
	sharedv1 "github.com/torchwooddev/torchwood/genproto/shared/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// businessFileDescriptors 与 NewGRPCServer 中 collectMethodsByAccess 的入参保持一致
// （新增服务文件后两处都要登记）。
func businessFileDescriptors() []protoreflect.FileDescriptor {
	return []protoreflect.FileDescriptor{
		clientv1.File_client_v1_account_proto,
		clientv1.File_client_v1_databases_proto,
		clientv1.File_client_v1_teams_proto,
		clientv1.File_client_v1_payments_proto,
		serverv1.File_server_v1_projects_proto,
		serverv1.File_server_v1_health_proto,
		serverv1.File_server_v1_storage_proto,
		serverv1.File_server_v1_users_proto,
		serverv1.File_server_v1_apikeys_proto,
		serverv1.File_server_v1_oauth_providers_proto,
		serverv1.File_server_v1_teams_proto,
		serverv1.File_server_v1_databases_proto,
		serverv1.File_server_v1_functions_proto,
		serverv1.File_server_v1_payments_proto,
		consolev1.File_console_v1_auth_proto,
		consolev1.File_console_v1_admins_proto,
	}
}

func accessLevelString(l sharedv1.AccessLevel) (string, bool) {
	switch l {
	case sharedv1.AccessLevel_ACCESS_PUBLIC:
		return "public", true
	case sharedv1.AccessLevel_ACCESS_AUTHENTICATED:
		return "authenticated", true
	case sharedv1.AccessLevel_ACCESS_PERMISSION:
		return "permission", true
	case sharedv1.AccessLevel_ACCESS_API_KEY:
		return "api_key", true
	}
	return "", false
}

// swaggerOp 是 swagger.json 中单个 HTTP operation 的最小结构。
type swaggerOp struct {
	OperationID string `json:"operationId"`
	XAccess     string `json:"x-torchwood-access"`
}

// TestSwaggerAccessExtensionMatchesCollectMethodsByAccess 断言每个
// genproto/**/*.swagger.json 中 operation 的有效 x-torchwood-access
// （operation 级扩展，未声明时继承 swagger 顶层值）与 collectMethodsByAccess
// 从 proto method_auth/service_auth 推导出的 access 完全一致（R10-P1-4）：
// 顶层扩展必须等于服务默认 access；operation 扩展与 method_auth 必须一致。
// 前提：仓库根执行过 task generate-proto（genproto 已入库，本测试直接可用）。
func TestSwaggerAccessExtensionMatchesCollectMethodsByAccess(t *testing.T) {
	descs := businessFileDescriptors()
	_, _, _, err := collectMethodsByAccess(descs...)
	require.NoError(t, err)

	// 按 collectMethodsByAccess 的解析语义（方法级 method_auth 优先，否则服务级
	// 默认）推导每个方法的 access；ACCESS_AUTHENTICATED 也落在 permissionMethods，
	// 因此不能直接用三个集合反推，这里逐方法解析。
	accessOf := make(map[string]string)
	defaultOf := make(map[string]string) // service full name → 服务默认 access
	byProtoPath := make(map[string]protoreflect.FileDescriptor, len(descs))
	for _, fd := range descs {
		byProtoPath[fd.Path()] = fd
		for i := 0; i < fd.Services().Len(); i++ {
			s := fd.Services().Get(i)
			serviceName := string(s.FullName())
			serviceDefault := resolveServiceDefaultAccess(s)
			def, ok := accessLevelString(serviceDefault)
			require.True(t, ok, "service %s 无法解析默认 access", serviceName)
			defaultOf[serviceName] = def
			methods := s.Methods()
			for j := 0; j < methods.Len(); j++ {
				m := methods.Get(j)
				access, _, ok := resolveMethodAccess(m, serviceDefault)
				require.True(t, ok && access != sharedv1.AccessLevel_ACCESS_LEVEL_UNSPECIFIED,
					"method %s/%s 无法解析 access", serviceName, m.Name())
				as, ok := accessLevelString(access)
				require.True(t, ok, "method %s/%s access 非法", serviceName, m.Name())
				accessOf["/"+serviceName+"/"+string(m.Name())] = as
			}
		}
	}

	genprotoDir := filepath.Join("..", "..", "..", "genproto")
	checkedOps := 0
	checkedFiles := 0
	for _, sub := range []string{"server", "client", "console"} {
		dir := filepath.Join(genprotoDir, sub, "v1")
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		for _, e := range entries {
			name := e.Name()
			if !strings.HasSuffix(name, ".swagger.json") {
				continue
			}
			protoPath := sub + "/v1/" + strings.TrimSuffix(name, ".swagger.json") + ".proto"
			fd, ok := byProtoPath[protoPath]
			if !ok {
				// shared/ 纯消息文件与框架产物无业务服务，跳过。
				continue
			}
			services := fd.Services()
			require.Equal(t, 1, services.Len(), "%s 应恰好包含一个业务服务", protoPath)
			service := services.Get(0)
			serviceName := string(service.FullName())

			var doc struct {
				XAccess string                          `json:"x-torchwood-access"`
				Paths   map[string]map[string]swaggerOp `json:"paths"`
			}
			raw, err := os.ReadFile(filepath.Join(dir, name))
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(raw, &doc))
			checkedFiles++

			wantDefault := defaultOf[serviceName]
			require.Equal(t, wantDefault, doc.XAccess,
				"%s 顶层 x-torchwood-access 应与服务默认 access 一致（%s）", name, serviceName)

			for _, ops := range doc.Paths {
				for _, op := range ops {
					if op.OperationID == "" {
						continue
					}
					// operationId 格式为 "{Service}_{RPC}"；additional_bindings
					// 生成 "{Service}_{RPC}{N}" 后缀（如 HealthService_Check2）。
					sep := strings.LastIndex(op.OperationID, "_")
					require.Greater(t, sep, 0, "%s operationId %q 格式非法", name, op.OperationID)
					rpc := op.OperationID[sep+1:]
					method := findMethodByOperationID(service, rpc)
					require.NotNil(t, method, "%s operationId %q 找不到对应 RPC", name, op.OperationID)

					fullMethod := "/" + serviceName + "/" + string(method.Name())
					want, ok := accessOf[fullMethod]
					require.True(t, ok, "%s %q 不在 collectMethodsByAccess 结果中", name, fullMethod)

					effective := op.XAccess
					if effective == "" {
						effective = doc.XAccess
					}
					require.Equal(t, want, effective,
						"%s %s 有效 x-torchwood-access=%q 与 method_auth %q 不一致",
						name, op.OperationID, effective, want)
					checkedOps++
				}
			}
		}
	}
	require.GreaterOrEqual(t, checkedFiles, 14, "genproto 业务 swagger 文件数量异常")
	require.GreaterOrEqual(t, checkedOps, 140, "swagger operation 数量异常（当前 %d）", checkedOps)
}

// findMethodByOperationID 先按原名查找；additional_bindings 的 "{N}" 后缀
// （如 Check2）按逐位截断回退查找。
func findMethodByOperationID(service protoreflect.ServiceDescriptor, rpc string) protoreflect.MethodDescriptor {
	methods := service.Methods()
	for i := 0; i < methods.Len(); i++ {
		if string(methods.Get(i).Name()) == rpc {
			return methods.Get(i)
		}
	}
	trimmed := strings.TrimRight(rpc, "0123456789")
	if trimmed == rpc || trimmed == "" {
		return nil
	}
	for i := 0; i < methods.Len(); i++ {
		if string(methods.Get(i).Name()) == trimmed {
			return methods.Get(i)
		}
	}
	return nil
}
