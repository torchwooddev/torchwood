package health

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDetails_AllOK(t *testing.T) {
	c := &Checkers{deps: []*DependencyChecker{
		{Name: "a", Check: func(ctx context.Context) error { return nil }},
		{Name: "b", Check: func(ctx context.Context) error { return nil }},
	}}
	st := c.Details(context.Background())
	require.Len(t, st, 2)
	for _, d := range st {
		require.Equal(t, "ok", d.GetStatus())
		require.Empty(t, d.GetError())
	}
}

func TestDetails_FailurePropagation(t *testing.T) {
	boom := errors.New("boom")
	c := &Checkers{deps: []*DependencyChecker{
		{Name: "a", Check: func(ctx context.Context) error { return nil }},
		{Name: "b", Check: func(ctx context.Context) error { return boom }},
	}}
	st := c.Details(context.Background())
	require.Equal(t, "ok", st[0].GetStatus())
	require.Empty(t, st[0].GetError())
	require.Equal(t, "unavailable", st[1].GetStatus())
	require.Equal(t, "boom", st[1].GetError())
}

func TestDetails_Timeout(t *testing.T) {
	c := &Checkers{deps: []*DependencyChecker{
		{Name: "slow", Timeout: 50 * time.Millisecond, Check: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		}},
	}}
	start := time.Now()
	st := c.Details(context.Background())
	require.Less(t, time.Since(start), 500*time.Millisecond)
	require.Equal(t, "unavailable", st[0].GetStatus())
	require.Equal(t, context.DeadlineExceeded.Error(), st[0].GetError())
}

func TestDetails_PanicRecover(t *testing.T) {
	c := &Checkers{deps: []*DependencyChecker{
		{Name: "panicky", Check: func(ctx context.Context) error { panic("kaboom") }},
	}}
	st := c.Details(context.Background())
	require.Equal(t, "unavailable", st[0].GetStatus())
	require.Contains(t, st[0].GetError(), "panic")
	require.Contains(t, st[0].GetError(), "kaboom")
}

func TestDependencyChecker_CheckHealthSelfTimeout(t *testing.T) {
	c := &DependencyChecker{Name: "x", Timeout: 50 * time.Millisecond, Check: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	start := time.Now()
	err := c.CheckHealth()
	require.Error(t, err)
	require.Less(t, time.Since(start), 500*time.Millisecond)
}

func TestDependencyChecker_CheckHealthOK(t *testing.T) {
	c := &DependencyChecker{Name: "x", Check: func(ctx context.Context) error { return nil }}
	require.NoError(t, c.CheckHealth())
}

func TestDependencyChecker_DefaultTimeout(t *testing.T) {
	c := &DependencyChecker{Name: "x", Check: func(ctx context.Context) error { return nil }}
	require.Equal(t, DefaultTimeout, c.timeout())
	require.Equal(t, DefaultTimeout, (&DependencyChecker{Name: "y", Timeout: -1}).timeout())
}

func TestCheckers_Deps(t *testing.T) {
	c := &Checkers{deps: []*DependencyChecker{
		{Name: "a", Check: func(ctx context.Context) error { return nil }},
	}}
	deps := c.Deps()
	require.Len(t, deps, 1)
	require.NoError(t, deps[0].CheckHealth())
}
