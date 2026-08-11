package main

import (
	"strings"
	"testing"

	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// serverV1Methods 遍历 protoregistry.GlobalFiles 中 server/v1/ 的全部方法描述符
// （不含 APIKeysService——API Key 凭证被服务端拦截器禁止调用），返回
// 完整方法名（/service/Method）到输入消息类型的映射。
func serverV1Methods(t *testing.T) map[string]protoreflect.FullName {
	t.Helper()
	// 引用生成包确保其 init 完成文件描述符注册（同包所有文件的 init 都会执行）。
	_ = serverv1.File_server_v1_health_proto

	methods := map[string]protoreflect.FullName{}
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if !strings.HasPrefix(fd.Path(), "server/v1/") {
			return true
		}
		services := fd.Services()
		for i := 0; i < services.Len(); i++ {
			sd := services.Get(i)
			if strings.HasSuffix(string(sd.FullName()), "APIKeysService") {
				continue
			}
			methodsDesc := sd.Methods()
			for j := 0; j < methodsDesc.Len(); j++ {
				md := methodsDesc.Get(j)
				fullMethod := "/" + string(sd.FullName()) + "/" + string(md.Name())
				methods[fullMethod] = md.Input().FullName()
			}
		}
		return true
	})
	return methods
}

// TestRPCRegistryCoverage 校验 rpcRegistry 覆盖 proto/server/v1 全部方法
// （除 APIKeysService），请求类型与方法描述符的输入消息一致，且没有多余条目。
func TestRPCRegistryCoverage(t *testing.T) {
	methods := serverV1Methods(t)
	if len(methods) == 0 {
		t.Fatal("未发现 server/v1 方法描述符，protoregistry 未注册？")
	}

	var missing []string
	var mismatched []string
	for fullMethod, wantInput := range methods {
		e, ok := rpcRegistry[fullMethod]
		if !ok {
			missing = append(missing, fullMethod)
			continue
		}
		if got := proto.MessageName(e.newReq()); got != wantInput {
			mismatched = append(mismatched, fullMethod+": got "+string(got)+" want "+string(wantInput))
		}
	}
	if len(missing) > 0 || len(mismatched) > 0 {
		t.Fatalf("注册表不完整：missing=%v\nmismatched=%v", missing, mismatched)
	}

	// 反向校验：注册表不允许出现描述符之外的条目（防手工录入笔误）。
	for fullMethod := range rpcRegistry {
		if _, ok := methods[fullMethod]; !ok {
			t.Errorf("注册表多余条目（不存在的方法）：%s", fullMethod)
		}
	}
}

// TestRPCRegistryConstructors 校验每个条目的构造器每次返回全新实例且可复用
// （proto.Clone 语义），请求与响应类型不同名。
func TestRPCRegistryConstructors(t *testing.T) {
	for method, e := range rpcRegistry {
		req1, req2 := e.newReq(), e.newReq()
		if req1 == req2 {
			t.Errorf("%s: newReq 应返回新实例", method)
		}
		if proto.MessageName(e.newReq()) == proto.MessageName(e.newResp()) {
			t.Errorf("%s: 请求与响应类型相同，注册错误？", method)
		}
	}
}

// TestLookupRPCMethod 覆盖未知方法的错误提示。
func TestLookupRPCMethod(t *testing.T) {
	if _, err := lookupRPCMethod("/torchwood.server.v1.APIKeysService/ListAPIKeys"); err == nil {
		t.Fatal("APIKeysService 不应注册在表中")
	}
	if _, err := lookupRPCMethod("/torchwood.server.v1.UsersService/NotAMethod"); err == nil {
		t.Fatal("未知方法应报错")
	}
	if _, err := lookupRPCMethod("/torchwood.server.v1.UsersService/ListUsers"); err != nil {
		t.Fatalf("已知方法应可解析：%v", err)
	}
}
