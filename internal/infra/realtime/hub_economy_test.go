package realtime

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/events"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
)

// economyFrame 从连接缓冲收一帧并断言频道。
func economyFrame(t *testing.T, conn *Conn, wantChannel string) map[string]any {
	t.Helper()
	select {
	case frame := <-conn.Send:
		require.Equal(t, "event", frame["type"])
		require.Equal(t, wantChannel, frame["channel"])
		return frame["payload"].(map[string]any)
	case <-time.After(time.Second):
		t.Fatal("expected economy frame on accounts channel")
		return nil
	}
}

// TestHubDispatchEconomyChannel 验证经济事件（D17）只扇出显式 Channel
// （accounts.{userId}），无 acl 过滤、帧含 domain 字段；未订阅该频道的
// 连接不收任何帧。
func TestHubDispatchEconomyChannel(t *testing.T) {
	hub := NewHub(nil)
	owner := &Conn{ID: "c1", Send: make(chan map[string]any, 4)}
	other := &Conn{ID: "c2", Send: make(chan map[string]any, 4)}
	// DocPrincipal 留空：经济事件不得走文档 ACL（空 ACL 会全拒）。
	hub.Subscribe("accounts.u1", owner)
	hub.Subscribe("accounts.u2", other)

	hub.Dispatch(events.Envelope{
		EventID:   "evt_e1",
		Event:     "payments.orders.paid",
		ProjectID: "p1",
		Domain:    "payments",
		Channel:   "accounts.u1",
		CreatedAt: time.Now(),
		Attrs: map[string]any{
			"order_id": "o1",
			"amount":   int64(1999),
			"currency": "USD",
		},
	})

	payload := economyFrame(t, owner, "accounts.u1")
	require.Equal(t, "payments", payload["domain"])
	require.Equal(t, "payments.orders.paid", payload["event"])
	require.Equal(t, "o1", payload["order_id"])
	require.Equal(t, int64(1999), payload["amount"])
	select {
	case frame := <-other.Send:
		t.Fatalf("other user channel must not receive event, got %v", frame)
	default:
	}

	// 重复 event_id 不二次扇出（去重窗）。
	hub.Dispatch(events.Envelope{
		EventID: "evt_e1", Event: "payments.orders.paid", ProjectID: "p1",
		Domain: "payments", Channel: "accounts.u1", CreatedAt: time.Now(),
	})
	select {
	case frame := <-owner.Send:
		t.Fatalf("duplicate event_id must be deduped, got %v", frame)
	default:
	}
	hub.Remove(owner.ID)
	hub.Remove(other.ID)
}

// TestHubDispatchDocumentUnchangedWithEconomyFields 验证文档事件扇出
// 行为不因经济扩展改变：仍按集合 + 文档双频道、走 ACL 过滤。
func TestHubDispatchDocumentUnchangedWithEconomyFields(t *testing.T) {
	hub := NewHub(nil)
	conn := &Conn{
		ID:           "c1",
		DocPrincipal: shared.RealtimeConn{}.DocPrincipal, // zero value ok
		Send:         make(chan map[string]any, 4),
	}
	hub.Subscribe("databases.db1.collections.coll1", conn)

	// PlatformAdmin 旁路：空 ACL 也可见（v2 语义）。
	conn.PlatformAdmin = true
	hub.Dispatch(events.Envelope{
		EventID:      "evt_d1",
		Event:        events.EventDocumentsCreate,
		ProjectID:    "p1",
		DatabaseID:   "db1",
		CollectionID: "coll1",
		DocumentID:   "doc1",
		Version:      1,
		CreatedAt:    time.Now(),
	})
	payload := economyFrame(t, conn, "databases.db1.collections.coll1")
	require.NotContains(t, payload, "domain")
	require.NotContains(t, payload, "channel")
	hub.Remove(conn.ID)
}
