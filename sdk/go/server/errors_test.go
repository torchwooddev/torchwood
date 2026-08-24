package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

// TestExtractRetryAfter：限流 ResourceExhausted 携带 RetryInfo detail 时
// 可读出建议退避秒数；无 detail / 非 status 错误返回 false（Round4 J3-6）。
func TestExtractRetryAfter(t *testing.T) {
	st, err := status.New(codes.ResourceExhausted, "rate limit exceeded").
		WithDetails(&errdetails.RetryInfo{RetryDelay: durationpb.New(17 * time.Second)})
	require.NoError(t, err)

	d, ok := ExtractRetryAfter(st.Err())
	require.True(t, ok)
	require.Equal(t, 17*time.Second, d)

	// 无 RetryInfo detail。
	_, ok = ExtractRetryAfter(status.Error(codes.ResourceExhausted, "bare"))
	require.False(t, ok)

	// 非 status 错误。
	_, ok = ExtractRetryAfter(nil)
	require.False(t, ok)

	// HTTPErrorClass 与限流类别联动自检。
	require.Equal(t, 4, HTTPErrorClass(st.Err()))
}
