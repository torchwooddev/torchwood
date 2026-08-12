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

func TestRedactQuery_InsertValues(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "single sensitive column",
			in:   "INSERT INTO users (password_hash) VALUES ('s3cret')",
			want: "INSERT INTO users (password_hash) VALUES ([REDACTED])",
		},
		{
			name: "setup_token in multi-column values",
			in:   "INSERT INTO api_keys (name, setup_token) VALUES ('default-key', 't0k3n123')",
			want: "INSERT INTO api_keys (name, setup_token) VALUES ([REDACTED])",
		},
		{
			name: "sensitive column among others",
			in:   "INSERT INTO users (id, email, password) VALUES (1, 'a@b.c', 'pw!')",
			want: "INSERT INTO users (id, email, password) VALUES ([REDACTED])",
		},
		{
			name: "paren inside quoted value stays fully redacted",
			in:   "INSERT INTO t (secret) VALUES ('abc)def')",
			want: "INSERT INTO t (secret) VALUES ([REDACTED])",
		},
		{
			name: "value list with default keyword",
			in:   "INSERT INTO t (token) VALUES (DEFAULT)",
			want: "INSERT INTO t (token) VALUES ([REDACTED])",
		},
		{
			name: "multi-row batch insert all tuples redacted",
			in:   "INSERT INTO users (password_hash) VALUES ('a'), ('b'), ('c')",
			want: "INSERT INTO users (password_hash) VALUES ([REDACTED])",
		},
		{
			name: "multi-line batch insert spans lines",
			in:   "INSERT INTO users (id, setup_token) VALUES (1, 't1'),\n(2, 't2'),\n(3, 't3')",
			want: "INSERT INTO users (id, setup_token) VALUES ([REDACTED])",
		},
		{
			name: "multi-row non-sensitive insert untouched",
			in:   "INSERT INTO t (name) VALUES ('bob'), ('alice'), ('carol')",
			want: "INSERT INTO t (name) VALUES ('bob'), ('alice'), ('carol')",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, redactQuery(&bun.QueryEvent{Query: tc.in}))
		})
	}
}

func TestRedactQuery_AssignmentFormsStillRedacted(t *testing.T) {
	got := redactQuery(&bun.QueryEvent{Query: "UPDATE projects SET setup_token = 'x1' WHERE id = 'p' AND password = 'y'"})
	require.Equal(t, "UPDATE projects SET setup_token = '[REDACTED]' WHERE id = 'p' AND password = '[REDACTED]'", got)
}
