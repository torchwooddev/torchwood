package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/lynx-go/lynx"
	lynxhttp "github.com/lynx-go/lynx/server/http"
	"github.com/stretchr/testify/require"
)

// TestHealthz_Readiness 用真实监听验证 lynxhttp 的 /healthz/readiness：
// checkers 全 ok → 200；任一失败 → 503。gRPC gateway 与 lynx 的 HTTP
// readiness 走同一 WithHealthCheckers 机制。
func TestHealthz_Readiness(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := lis.Addr().String()
	require.NoError(t, lis.Close())

	ok := func() error { return nil }
	fail := func() error { return errors.New("db down") }

	start := func(t *testing.T, checkers lynx.HealthCheckersFunc) string {
		t.Helper()
		srv := lynxhttp.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
			lynxhttp.WithAddr(addr),
			lynxhttp.WithHealthCheckers(checkers),
		)
		ctx, cancel := context.WithCancel(context.Background())
		go func() { _ = srv.Start(ctx) }()
		t.Cleanup(func() {
			cancel()
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer stopCancel()
			_ = srv.Stop(stopCtx)
		})
		return addr
	}

	waitReady := func(t *testing.T, base string) *http.Response {
		t.Helper()
		var resp *http.Response
		var err error
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			resp, err = http.Get(base + "/healthz/liveness")
			if err == nil {
				_ = resp.Body.Close()
				return resp
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("server not ready: %v", err)
		return nil
	}

	t.Run("all healthy -> 200", func(t *testing.T) {
		start(t, func() []lynx.Checker { return []lynx.Checker{okChecker{ok}} })
		waitReady(t, "http://"+addr)

		resp, err := http.Get("http://" + addr + "/healthz/readiness")
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		resp, err = http.Get("http://" + addr + "/healthz/liveness")
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		require.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("one failed -> 503", func(t *testing.T) {
		start(t, func() []lynx.Checker { return []lynx.Checker{okChecker{fail}} })
		waitReady(t, "http://"+addr)

		resp, err := http.Get("http://" + addr + "/healthz/readiness")
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

		resp, err = http.Get("http://" + addr + "/healthz/liveness")
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		require.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

type okChecker struct {
	fn func() error
}

func (c okChecker) CheckHealth() error { return c.fn() }

var _ lynx.Checker = okChecker{}
