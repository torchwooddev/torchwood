package conn

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// capturedInvoker 记录 invoker 收到的 ctx deadline，并可注入耗时与错误。
type capturedInvoker struct {
	sawDeadline   time.Time // 调用时 ctx 的 deadline（零值表示无）
	hasDeadline   bool
	sleep         time.Duration
	err           error
	canceledEarly bool
}

func (c *capturedInvoker) invoke(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
	if dl, ok := ctx.Deadline(); ok {
		c.hasDeadline = true
		c.sawDeadline = dl
	}
	if c.sleep > 0 {
		select {
		case <-time.After(c.sleep):
		case <-ctx.Done():
			c.canceledEarly = true
			return status.FromContextError(ctx.Err()).Err()
		}
	}
	return c.err
}

func TestTimeoutUnaryInterceptor_InjectsDefaultWhenNoDeadline(t *testing.T) {
	inv := &capturedInvoker{}
	ic := TimeoutUnaryInterceptor(0) // <=0 → DefaultTimeout

	require.NoError(t, ic(context.Background(), "/svc/Method", nil, nil, nil, inv.invoke))

	require.True(t, inv.hasDeadline, "应向 invoker 注入 deadline")
	budget := time.Until(inv.sawDeadline)
	require.LessOrEqual(t, budget, DefaultTimeout)
	require.Greater(t, budget, DefaultTimeout-time.Second, "注入预算应约等于 DefaultTimeout")
}

func TestTimeoutUnaryInterceptor_CustomBudget(t *testing.T) {
	inv := &capturedInvoker{}
	ic := TimeoutUnaryInterceptor(5 * time.Second)

	require.NoError(t, ic(context.Background(), "/svc/Method", nil, nil, nil, inv.invoke))
	require.True(t, inv.hasDeadline)
	budget := time.Until(inv.sawDeadline)
	require.LessOrEqual(t, budget, 5*time.Second)
	require.Greater(t, budget, 4*time.Second)
}

func TestTimeoutUnaryInterceptor_RespectsExistingDeadline(t *testing.T) {
	inv := &capturedInvoker{}
	ic := TimeoutUnaryInterceptor(50 * time.Millisecond) // 远小于调用方 deadline

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Hour))
	defer cancel()

	require.NoError(t, ic(ctx, "/svc/Method", nil, nil, nil, inv.invoke))
	require.True(t, inv.hasDeadline)
	// 尊重调用方：不得被 50ms 兜底覆盖。
	require.Greater(t, time.Until(inv.sawDeadline), time.Hour-time.Minute)
}

func TestTimeoutUnaryInterceptor_DeadlineExceededOnSlowCall(t *testing.T) {
	inv := &capturedInvoker{sleep: time.Second}
	ic := TimeoutUnaryInterceptor(30 * time.Millisecond)

	start := time.Now()
	err := ic(context.Background(), "/svc/Method", nil, nil, nil, inv.invoke)
	require.Error(t, err)
	require.Equal(t, codes.DeadlineExceeded, status.Code(err))
	require.True(t, inv.canceledEarly)
	require.Less(t, time.Since(start), time.Second, "应在兜底预算内返回而非等满服务端耗时")
}

func TestRetryDialOption_ValidServiceConfig(t *testing.T) {
	// service config 由 gRPC 在建连时解析；这里直接断言 JSON 形状的关键字段，
	// 真正的重试行为由 server 包的 bufconn 集成测试覆盖（TestUnavailableRetried）。
	require.Contains(t, retryServiceConfigJSON, `"retryPolicy"`)
	require.Contains(t, retryServiceConfigJSON, `"UNAVAILABLE"`)
}
