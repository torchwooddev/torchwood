package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/torchwooddev/torchwood/sdk/go/server"
)

// invoke 建立连接并以全局超时发起一次 InvokeJSON 调用。
// req 为 nil / string（原始 JSON，如 --data）/ map[string]any。
func invoke(g *globalFlags, method string, req any) ([]byte, error) {
	if g.tls {
		return nil, errors.New("--tls 尚未支持：服务端当前为明文 gRPC")
	}
	var opts []server.Option
	if g.apiKey != "" {
		opts = append(opts, server.WithAPIKey(g.apiKey))
	}
	c, err := server.New(g.endpoint, opts...)
	if err != nil {
		return nil, err
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), g.timeoutDur)
	defer cancel()

	var reqJSON []byte
	switch v := req.(type) {
	case nil:
	case string:
		if v != "" {
			reqJSON = []byte(v)
		}
	case []byte:
		reqJSON = v
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("请求编码失败：%v", err)
		}
		reqJSON = b
	}
	resp, err := c.InvokeJSON(ctx, method, reqJSON)
	if err != nil {
		return nil, errors.New(formatRPCError(err))
	}
	return resp, nil
}

// printJSON 把响应 JSON 字节原样渲染到 stdout（SDK 已按缩进格式编码）。
func printJSON(w io.Writer, b []byte) error {
	_, err := fmt.Fprintln(w, string(b))
	return err
}

// formatRPCError 把 gRPC 调用错误转成 CLI 可读文本；
// PermissionDenied 时附加 scope 提示（scope 格式见 pkg/grpc/interceptor/apikey_scope.go）。
func formatRPCError(err error) string {
	if server.IsPermissionDenied(err) {
		return fmt.Sprintf("rpc failed: %v\n提示：请检查 API Key 的 scope（如 users.read / users.write，或 * / all），或用 Console 重新生成 key", err)
	}
	return fmt.Sprintf("rpc failed: %v", err)
}

// changedBoolPtr 返回 flag 显式设置时的指针（nil 表示未设置），
// 用于映射 proto3 optional bool 字段的 presence 语义。
// 注意：Task 11 起被 setChanged（JSON map 版本）取代，本函数随命令迁移后删除。
func changedBoolPtr(cmd *cobra.Command, name string, v bool) *bool {
	if cmd.Flags().Changed(name) {
		return &v
	}
	return nil
}
