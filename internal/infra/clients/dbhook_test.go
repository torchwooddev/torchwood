package clients

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/schema"
)

type fakeQuery struct{ op string }

func (q fakeQuery) Operation() string                                   { return q.op }
func (q fakeQuery) GetModel() bun.Model                                 { return nil }
func (q fakeQuery) GetTableName() string                                { return "" }
func (q fakeQuery) AppendQuery(schema.QueryGen, []byte) ([]byte, error) { return nil, nil }

var _ bun.Query = fakeQuery{}

func newTestLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func slowEvent(back time.Duration) *bun.QueryEvent {
	return &bun.QueryEvent{
		IQuery:    fakeQuery{op: "SELECT"},
		Query:     "SELECT 1",
		StartTime: time.Now().Add(-back),
	}
}

func TestSlowQueryHook_ThresholdHit(t *testing.T) {
	var buf bytes.Buffer
	h := &SlowQueryHook{Threshold: 500 * time.Millisecond, Logger: newTestLogger(&buf)}
	h.AfterQuery(context.Background(), slowEvent(time.Second))
	out := buf.String()
	require.Contains(t, out, "slow query")
	require.Contains(t, out, "operation=SELECT")
	require.Contains(t, out, "SELECT 1")
	require.Contains(t, out, "duration")
}

func TestSlowQueryHook_ThresholdMiss(t *testing.T) {
	var buf bytes.Buffer
	h := &SlowQueryHook{Threshold: 500 * time.Millisecond, Logger: newTestLogger(&buf)}
	h.AfterQuery(context.Background(), slowEvent(time.Millisecond))
	require.Empty(t, buf.String())
}

func TestSlowQueryHook_Disabled(t *testing.T) {
	var buf bytes.Buffer
	h := &SlowQueryHook{Threshold: 0, Logger: newTestLogger(&buf)}
	h.AfterQuery(context.Background(), slowEvent(time.Second))
	require.Empty(t, buf.String())
}

func TestSlowQueryHook_LogAll(t *testing.T) {
	var buf bytes.Buffer
	h := &SlowQueryHook{LogAll: true, Logger: newTestLogger(&buf)}
	h.AfterQuery(context.Background(), slowEvent(time.Millisecond))
	out := buf.String()
	require.Contains(t, out, "msg=sql")
	require.Contains(t, out, "operation=SELECT")
}

func TestSlowQueryHook_ErrField(t *testing.T) {
	var buf bytes.Buffer
	h := &SlowQueryHook{Threshold: 500 * time.Millisecond, Logger: newTestLogger(&buf)}
	e := slowEvent(time.Second)
	e.Err = errors.New("db down")
	h.AfterQuery(context.Background(), e)
	require.Contains(t, buf.String(), "db down")
}

func TestSlowQueryHook_OperationFromQueryString(t *testing.T) {
	var buf bytes.Buffer
	h := &SlowQueryHook{Threshold: 500 * time.Millisecond, Logger: newTestLogger(&buf)}
	e := &bun.QueryEvent{Query: "INSERT INTO t (a) VALUES (1)", StartTime: time.Now().Add(-time.Second)}
	h.AfterQuery(context.Background(), e)
	require.Contains(t, buf.String(), "operation=INSERT")
}

func TestNewSlowQueryHook_DefaultThreshold(t *testing.T) {
	h := NewSlowQueryHook("", false, newTestLogger(&bytes.Buffer{}))
	require.NotNil(t, h)
	require.Equal(t, DefaultSlowQueryThreshold, h.Threshold)
	require.False(t, h.LogAll)
}

func TestNewSlowQueryHook_Disabled(t *testing.T) {
	require.Nil(t, NewSlowQueryHook("0", false, newTestLogger(&bytes.Buffer{})))
}

func TestNewSlowQueryHook_Explicit(t *testing.T) {
	h := NewSlowQueryHook("200ms", false, newTestLogger(&bytes.Buffer{}))
	require.NotNil(t, h)
	require.Equal(t, 200*time.Millisecond, h.Threshold)
}

func TestNewSlowQueryHook_InvalidDisables(t *testing.T) {
	var buf bytes.Buffer
	require.Nil(t, NewSlowQueryHook("not-a-duration", false, newTestLogger(&buf)))
	require.Contains(t, buf.String(), "invalid slow_query_threshold")
}

func TestNewSlowQueryHook_DebugLogAll(t *testing.T) {
	h := NewSlowQueryHook("0", true, newTestLogger(&bytes.Buffer{}))
	require.NotNil(t, h)
	require.True(t, h.LogAll)
}
