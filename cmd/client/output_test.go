package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
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
			name: "Unauthenticated 显示 code+message",
			err:  status.Error(codes.Unauthenticated, "invalid or expired credential"),
			want: []string{"Unauthenticated", "invalid or expired credential"},
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

func TestNewConnTLSUnsupported(t *testing.T) {
	if _, err := newConn("127.0.0.1:9060", "k", true); err == nil || !strings.Contains(err.Error(), "尚未支持") {
		t.Fatalf("--tls 应返回未支持错误，got %v", err)
	}
}

func TestPrintJSON(t *testing.T) {
	var buf bytes.Buffer
	msg := &serverv1.GetVersionResponse{Version: "v1.2.3"}
	if err := printJSON(&buf, msg); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	// protojson Multiline+Indent：字段后跟 Indent 串作为分隔（"version":  "v1.2.3"）。
	if !strings.Contains(out, "\n  \"version\"") || !strings.Contains(out, "\"v1.2.3\"") {
		t.Errorf("printJSON 输出格式不符：%q", out)
	}
}
