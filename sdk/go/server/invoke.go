package server

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

// invokeMarshaler 与 CLI 历史输出格式逐字节一致：缩进 2 空格、不输出零值字段。
var invokeMarshaler = protojson.MarshalOptions{Multiline: true, Indent: "  "}

// InvokeJSON 以「方法名 + protojson 请求」调用任意 Server API unary 方法
// （APIKeysService 除外），返回 protojson 响应。method 形如
// "/torchwood.server.v1.UsersService/CreateUser"。reqJSON 可为空（相当于 {}）。
func (c *Client) InvokeJSON(ctx context.Context, method string, reqJSON []byte) ([]byte, error) {
	md, err := findServerMethod(method)
	if err != nil {
		return nil, err
	}
	req := dynamicpb.NewMessage(md.Input())
	if len(reqJSON) > 0 {
		if err := protojson.Unmarshal(reqJSON, req); err != nil {
			return nil, err
		}
	}
	resp := dynamicpb.NewMessage(md.Output())
	if err := c.conn.Invoke(c.authContext(ctx), method, req, resp); err != nil {
		return nil, err
	}
	return invokeMarshaler.Marshal(resp)
}

// findServerMethod 按 gRPC 路径查找 MethodDescriptor，限定 torchwood.server.v1
// 且排除 APIKeysService（API Key 凭证被服务端禁止调用该服务）。
func findServerMethod(method string) (protoreflect.MethodDescriptor, error) {
	unknown := fmt.Errorf("torchwood: unknown method %q", method)
	name := strings.TrimPrefix(method, "/")
	// gRPC 路径 "pkg.Service/Method" -> protoreflect 全名 "pkg.Service.Method"
	idx := strings.LastIndex(name, "/")
	if idx < 0 {
		return nil, unknown
	}
	full := name[:idx] + "." + name[idx+1:]
	d, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(full))
	if err != nil {
		return nil, unknown
	}
	md, ok := d.(protoreflect.MethodDescriptor)
	if !ok {
		return nil, unknown
	}
	svc, ok := md.Parent().(protoreflect.ServiceDescriptor)
	if !ok {
		return nil, unknown
	}
	if svc.Name() == "APIKeysService" || !strings.HasPrefix(string(svc.FullName()), "torchwood.server.v1.") {
		return nil, unknown
	}
	return md, nil
}
