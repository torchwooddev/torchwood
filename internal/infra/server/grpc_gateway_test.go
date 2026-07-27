package server

import "testing"

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
