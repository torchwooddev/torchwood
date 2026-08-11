package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// protoJSONMarshaler 是统一的成功响应渲染器：缩进 2 空格、不输出零值字段。
var protoJSONMarshaler = protojson.MarshalOptions{
	Multiline: true,
	Indent:    "  ",
}

// printJSON 把响应消息以缩进 protojson 渲染到 stdout。
func printJSON(w io.Writer, msg proto.Message) error {
	b, err := protoJSONMarshaler.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(b))
	return err
}

// invoke 建立连接并以全局超时发起一次 unary 调用；返回的错误已格式化为
// code + message（见 formatRPCError），直接交给 main 打印即可。
func invoke(g *globalFlags, method string, req, resp proto.Message) error {
	conn, err := newConn(g.endpoint, g.apiKey, g.tls)
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), g.timeoutDur)
	defer cancel()

	if err := conn.Invoke(ctx, method, req, resp); err != nil {
		return errors.New(formatRPCError(err))
	}
	return nil
}

// formatRPCError 把 gRPC 调用错误转成 CLI 可读文本：code + message；
// PermissionDenied 时附加 scope 提示（scope 格式见 pkg/grpc/interceptor/apikey_scope.go）。
func formatRPCError(err error) string {
	st, ok := status.FromError(err)
	if !ok {
		return err.Error()
	}
	msg := fmt.Sprintf("rpc failed: %s: %s", st.Code(), st.Message())
	if st.Code() == codes.PermissionDenied {
		msg += "\n提示：请检查 API Key 的 scope（如 users.read / users.write，或 * / all），或用 Console 重新生成 key"
	}
	return msg
}

// changedBoolPtr 返回 flag 显式设置时的指针（nil 表示未设置），
// 用于映射 proto3 optional bool 字段的 presence 语义。
func changedBoolPtr(cmd *cobra.Command, name string, v bool) *bool {
	if cmd.Flags().Changed(name) {
		return &v
	}
	return nil
}
