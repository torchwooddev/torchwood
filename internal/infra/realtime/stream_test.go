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

// TestStreamTransport_XAddsFullEnvelope：XADD 载荷是完整信封 JSON
// （含 acl），与 outbox.payload 同形；往返解码不丢字段。
func TestStreamTransport_XAddsFullEnvelope(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	ev := testEnvelope()
	transport := NewStreamTransport(client)
	require.NoError(t, transport.Enqueue(context.Background(), ev))

	entries, err := client.XRange(context.Background(), streamKey, "-", "+").Result()
	require.NoError(t, err)
	require.Len(t, entries, 1)
	raw, ok := entries[0].Values["payload"].(string)
	require.True(t, ok)

	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &m))
	require.Equal(t, ev.EventID, m["event_id"])
	require.Contains(t, m, "acl")

	decoded, err := infraevents.UnmarshalEnvelope([]byte(raw))
	require.NoError(t, err)
	require.Equal(t, ev.EventID, decoded.EventID)
	require.Equal(t, ev.Event, decoded.Event)
	require.Equal(t, ev.Version, decoded.Version)
	require.Equal(t, ev.DocumentID, decoded.DocumentID)
	require.Equal(t, ev.ACL.DocumentSecurity, decoded.ACL.DocumentSecurity)
	require.Equal(t, ev.ACL.DocHasPerms, decoded.ACL.DocHasPerms)
	require.Equal(t, ev.ACL.DocumentPermissions, decoded.ACL.DocumentPermissions)
	require.NotNil(t, decoded.Data)
	require.Equal(t, ev.Data.Data, decoded.Data.Data)
}

// TestMarshalEnvelope_RoundTrip：Envelope → JSON → Envelope 无损往返
// （worker 从 outbox.payload 重建信封再 XADD，序列化必须稳定）。
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
