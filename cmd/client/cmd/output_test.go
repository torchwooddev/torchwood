package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestValidateMissingAPIKey(t *testing.T) {
	g := &globalFlags{output: "json", timeout: "30s"}
	leaf := &cobra.Command{Use: "list"}
	root := &cobra.Command{Use: "users"}
	root.AddCommand(leaf)

	// 非豁免命令缺 key 报错
	if err := g.validate(leaf, nil); err == nil || !strings.Contains(err.Error(), "缺少 API key") {
		t.Fatalf("非豁免命令缺 key 应报错，got %v", err)
	}

	// health 命令（含子命令）豁免
	health := &cobra.Command{Use: "health", Annotations: map[string]string{annotationNoKey: "true"}}
	get := &cobra.Command{Use: "get"}
	health.AddCommand(get)
	if err := g.validate(get, nil); err != nil {
		t.Fatalf("health 子命令应豁免 api-key 校验：%v", err)
	}

	// 带 key 通过
	g.apiKey = "k"
	if err := g.validate(leaf, nil); err != nil {
		t.Fatalf("带 key 应通过：%v", err)
	}
}

func TestValidateOutputAndTimeout(t *testing.T) {
	g := &globalFlags{output: "yaml", timeout: "30s", apiKey: "k"}
	if err := g.validate(&cobra.Command{}, nil); err == nil || !strings.Contains(err.Error(), "不支持的输出格式") {
		t.Fatalf("非法 output 应报错，got %v", err)
	}
	g.output = "json"
	g.timeout = "abc"
	if err := g.validate(&cobra.Command{}, nil); err == nil || !strings.Contains(err.Error(), "无效的 --timeout") {
		t.Fatalf("非法 timeout 应报错，got %v", err)
	}
}

func TestFormatRPCError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want []string
	}{
		{
			name: "PermissionDenied 附加 scope 提示",
			err:  status.Error(codes.PermissionDenied, "api key missing required scope"),
			want: []string{"PermissionDenied", "api key missing required scope", "scope"},
		},
		{
			name: "Unauthenticated 附加 API Key 自诊断提示",
			err:  status.Error(codes.Unauthenticated, "invalid or expired credential"),
			want: []string{"Unauthenticated", "invalid or expired credential", "TORCHWOOD_CLI_API_KEY", "--api-key", "过期", "torchwood health"},
		},
		{
			name: "非 status 错误原样输出",
			err:  errors.New("dial 失败"),
			want: []string{"dial 失败"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatRPCError(tt.err)
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("want %q in %q", w, got)
				}
			}
		})
	}
}

// TestExitCode 固化退出码映射：OK=0 / 参数错=1 / 40x=2 / 5xx=3 / 限流=4。
func TestExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "nil 为成功", err: nil, want: 0},
		{name: "非 RPC 错误（参数校验等）为 1", err: errors.New("无效的 --timeout"), want: 1},
		{name: "Unauthenticated(401) 为 2", err: &rpcError{cause: status.Error(codes.Unauthenticated, "401")}, want: 2},
		{name: "PermissionDenied(403) 为 2", err: &rpcError{cause: status.Error(codes.PermissionDenied, "403")}, want: 2},
		{name: "NotFound(404) 为 2", err: &rpcError{cause: status.Error(codes.NotFound, "404")}, want: 2},
		{name: "InvalidArgument(400) 为 2", err: &rpcError{cause: status.Error(codes.InvalidArgument, "400")}, want: 2},
		{name: "AlreadyExists(409) 为 2", err: &rpcError{cause: status.Error(codes.AlreadyExists, "409")}, want: 2},
		{name: "Canceled(408/499) 为 2", err: &rpcError{cause: status.Error(codes.Canceled, "canceled")}, want: 2},
		{name: "Internal(500) 为 3", err: &rpcError{cause: status.Error(codes.Internal, "500")}, want: 3},
		{name: "Unavailable(503) 为 3", err: &rpcError{cause: status.Error(codes.Unavailable, "503")}, want: 3},
		{name: "DeadlineExceeded(504) 为 3", err: &rpcError{cause: status.Error(codes.DeadlineExceeded, "504")}, want: 3},
		{name: "Unknown(500) 为 3", err: &rpcError{cause: status.Error(codes.Unknown, "unknown")}, want: 3},
		{name: "ResourceExhausted(429) 为 4", err: &rpcError{cause: status.Error(codes.ResourceExhausted, "429")}, want: 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExitCode(tt.err); got != tt.want {
				t.Errorf("ExitCode(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

// TestInvokeWrapsRPCError 验证 invoke 对 gRPC 错误保留 code（供退出码映射）。
// 这里走 --tls 短路路径之外的真实错误：非法 endpoint 的拨号失败按 Unknown
// 归入 5xx 类（3）。
func TestInvokeMapsGRPCErrors(t *testing.T) {
	g := &globalFlags{endpoint: "127.0.0.1:1", apiKey: "k", timeoutDur: 300 * time.Millisecond}
	_, err := invoke(g, "/torchwood.server.v1.HealthService/Check", nil)
	re, ok := err.(*rpcError)
	if !ok {
		t.Fatalf("invoke 应返回 *rpcError，got %T", err)
	}
	if ExitCode(re) != 3 {
		t.Errorf("连接失败应归入 5xx 类（退出码 3），got %d", ExitCode(re))
	}
}

func TestInvokeTLSNotSupported(t *testing.T) {
	g := &globalFlags{tls: true}
	_, err := invoke(g, "/torchwood.server.v1.HealthService/Check", nil)
	if err == nil || !strings.Contains(err.Error(), "--tls 尚未支持") {
		t.Fatalf("--tls 应返回未支持错误，got %v", err)
	}
}

func TestPrintJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := printJSON(&buf, []byte("{\"version\":\"v1.2.3\"}")); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if out != "{\"version\":\"v1.2.3\"}\n" {
		t.Errorf("printJSON 应原样写字节 + 换行：%q", out)
	}
}
