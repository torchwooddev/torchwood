package server

import (
	"testing"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
)

// 回归测试：X-Graviton-Project 必须经 gateway 透传为 gRPC metadata，
// 此前 case 写成混合大小写永不命中，导致 HTTP 入口丢失项目上下文。
func TestAuthIncomingHeaderMatcher_ProjectHeader(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"X-Graviton-Project", "x-graviton-project", "X-GRAVITON-PROJECT"} {
		mapped, ok := authIncomingHeaderMatcher(key)
		if !ok {
			t.Fatalf("header %q should be forwarded", key)
		}
		if mapped != "x-graviton-project" {
			t.Fatalf("header %q should map to lowercase metadata key, got %q", key, mapped)
		}
	}
}

// console session cookie 依赖 set-cookie metadata 透传为 Set-Cookie 响应头；
// grpc-gateway 默认 matcher 会给所有 metadata 加 Grpc-Metadata- 前缀，导致
// 浏览器收不到 cookie，这里锁定自定义 matcher 的行为。
func TestAuthOutgoingHeaderMatcher(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"set-cookie", "Set-Cookie", "SET-COOKIE"} {
		mapped, ok := authOutgoingHeaderMatcher(key)
		if !ok {
			t.Fatalf("metadata key %q should be forwarded", key)
		}
		if mapped != "Set-Cookie" {
			t.Fatalf("metadata key %q should map to Set-Cookie, got %q", key, mapped)
		}
	}

	// 其余 metadata key 保持 grpc-gateway 默认行为（Grpc-Metadata- 前缀）。
	mapped, ok := authOutgoingHeaderMatcher("x-custom-meta")
	if !ok {
		t.Fatal("other metadata keys should keep the default passthrough")
	}
	if mapped != runtime.MetadataHeaderPrefix+"x-custom-meta" {
		t.Fatalf("unexpected mapping %q, want %q", mapped, runtime.MetadataHeaderPrefix+"x-custom-meta")
	}
}
