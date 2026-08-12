package clients

import (
	"bytes"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizePoolSizes_ZeroValues(t *testing.T) {
	var buf bytes.Buffer
	maxOpen, maxIdle := normalizePoolSizes(0, 0, newTestLogger(&buf))
	require.Equal(t, 4*runtime.GOMAXPROCS(0), maxOpen)
	require.Equal(t, maxOpen, maxIdle)
	out := buf.String()
	require.Contains(t, out, "max_open_conns <= 0")
	require.Contains(t, out, "max_idle_conns <= 0")
}

func TestNormalizePoolSizes_NegativeValues(t *testing.T) {
	var buf bytes.Buffer
	maxOpen, maxIdle := normalizePoolSizes(-5, -8, newTestLogger(&buf))
	require.Equal(t, 4*runtime.GOMAXPROCS(0), maxOpen)
	require.Equal(t, maxOpen, maxIdle)
	out := buf.String()
	require.Contains(t, out, "max_open_conns <= 0")
	require.Contains(t, out, "max_idle_conns <= 0")
}

func TestNormalizePoolSizes_MaxIdleDefaultsToMaxOpen(t *testing.T) {
	var buf bytes.Buffer
	maxOpen, maxIdle := normalizePoolSizes(20, 0, newTestLogger(&buf))
	require.Equal(t, 20, maxOpen)
	require.Equal(t, 20, maxIdle)
	out := buf.String()
	require.NotContains(t, out, "max_open_conns <= 0")
	require.Contains(t, out, "max_idle_conns <= 0")
}

func TestNormalizePoolSizes_NormalValuesUntouched(t *testing.T) {
	var buf bytes.Buffer
	maxOpen, maxIdle := normalizePoolSizes(20, 5, newTestLogger(&buf))
	require.Equal(t, 20, maxOpen)
	require.Equal(t, 5, maxIdle)
	require.Empty(t, buf.String())
}

func TestNormalizePoolSizes_NilLogger(t *testing.T) {
	maxOpen, maxIdle := normalizePoolSizes(0, 0, nil)
	require.Equal(t, 4*runtime.GOMAXPROCS(0), maxOpen)
	require.Equal(t, maxOpen, maxIdle)
}
