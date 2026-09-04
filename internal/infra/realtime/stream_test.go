package realtime

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	infraevents "github.com/torchwooddev/torchwood/internal/infra/events"
)

// newStreamEnv 组装 miniredis Stream 测试环境。
func newStreamEnv(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return client, mr
}

// TestStreamTransport_XAddFullEnvelope：XADD 载荷是完整信封 JSON（含 acl
// 与 seq），与 outbox.payload 同形；XRANGE 读回往返解码不丢字段。
func TestStreamTransport_XAddFullEnvelope(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	client, _ := newStreamEnv(t)
	transport := NewStreamTransport(client)

	ev := testEnvelope()
	ev.Seq = 42
	require.NoError(t, transport.Enqueue(context.Background(), ev))

	entries, err := client.XRange(context.Background(), eventsStream, "-", "+").Result()
	require.NoError(t, err)
	require.Len(t, entries, 1)
	raw, _ := entries[0].Values["payload"].(string)
	require.NotEmpty(t, raw)

	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &m))
	require.Equal(t, ev.EventID, m["event_id"])
	require.Contains(t, m, "acl")
	require.Equal(t, float64(42), m["seq"])

	decoded, err := infraevents.UnmarshalEnvelope([]byte(raw))
	require.NoError(t, err)
	require.Equal(t, ev.EventID, decoded.EventID)
	require.Equal(t, ev.Event, decoded.Event)
	require.Equal(t, ev.Version, decoded.Version)
	require.Equal(t, ev.DocumentID, decoded.DocumentID)
	require.Equal(t, int64(42), decoded.Seq)
	require.Equal(t, ev.ACL.DocumentSecurity, decoded.ACL.DocumentSecurity)
	require.Equal(t, ev.ACL.DocHasPerms, decoded.ACL.DocHasPerms)
	require.Equal(t, ev.ACL.DocumentPermissions, decoded.ACL.DocumentPermissions)
	require.NotNil(t, decoded.Data)
	require.Equal(t, ev.Data.Data, decoded.Data.Data)
}

// TestStreamTransport_Trim：周期裁剪把 Stream 收敛到水位内（近似裁剪）。
func TestStreamTransport_Trim(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	client, _ := newStreamEnv(t)
	transport := NewStreamTransport(client)

	// var 覆写水位（miniredis 的 XTRIM 近似语义按精确处理，直接用小值验证）。
	saved := eventsStreamMaxLen
	eventsStreamMaxLen = 3
	t.Cleanup(func() { eventsStreamMaxLen = saved })

	for i := 0; i < 10; i++ {
		ev := testEnvelope()
		ev.EventID = testEnvelope().EventID
		ev.DocumentID = "d"
		require.NoError(t, transport.Enqueue(context.Background(), ev))
	}
	require.NoError(t, transport.Trim(context.Background()))

	n, err := client.XLen(context.Background(), eventsStream).Result()
	require.NoError(t, err)
	require.LessOrEqual(t, n, int64(3), "Trim 后 Stream 不得超过水位")
}

// TestMarshalEnvelope_RoundTrip：Envelope → JSON → Envelope 无损往返
//（worker 从 outbox.payload 重建信封再 XADD，序列化必须稳定）。
func TestMarshalEnvelope_RoundTrip(t *testing.T) {
	ev := testEnvelope()
	first, err := infraevents.MarshalEnvelope(ev)
	require.NoError(t, err)
	decoded, err := infraevents.UnmarshalEnvelope(first)
	require.NoError(t, err)
	second, err := infraevents.MarshalEnvelope(decoded)
	require.NoError(t, err)
	require.Equal(t, string(first), string(second), "信封往返序列化必须字节一致")
}
