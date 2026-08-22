package realtime

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	infraevents "github.com/torchwooddev/torchwood/internal/infra/events"
	"github.com/torchwooddev/torchwood/internal/testutil"
)

// newSubscriberEnv 组装 subscriber 集成测试环境：真实 Postgres（迁移含 outbox）+ miniredis Pub/Sub。
func newSubscriberEnv(t *testing.T) (*Subscriber, *Hub, *redis.Client, *clients.Database) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	db := testutil.SetupTestDB(t)
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	hub := NewHub(nil)
	sub := NewRealtimeSubscriber(client, db, hub, nil)
	return sub, hub, client, db
}

// waitFrame 等待 Hub 扇出的帧（带超时）。
func waitFrame(t *testing.T, conn *Conn) map[string]any {
	t.Helper()
	select {
	case frame := <-conn.Send:
		return frame
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for hub frame")
		return nil
	}
}

// TestSubscriber_BroadcastToMultipleHubs：一次 PUBLISH，两个独立 Hub（含各自 Subscriber）都能 Dispatch。
func TestSubscriber_BroadcastToMultipleHubs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	db := testutil.SetupTestDB(t)
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	hub1 := NewHub(nil)
	hub2 := NewHub(nil)
	sub1 := NewRealtimeSubscriber(client, db, hub1, nil)
	sub2 := NewRealtimeSubscriber(client, db, hub2, nil)

	ctx := context.Background()
	ev := testEnvelope()
	require.NoError(t, infraevents.NewEventOutbox(db).Publish(ctx, ev))

	// 两个 Hub 各有一个订阅者，订阅同一集合频道。
	r1 := newTestConn("u1", databases.Principal{Roles: []string{"users", "user:u1"}})
	r2 := newTestConn("u1", databases.Principal{Roles: []string{"users", "user:u1"}})
	hub1.Subscribe(ev.CollectionChannel(), r1)
	hub2.Subscribe(ev.CollectionChannel(), r2)

	runCtx1, cancel1 := context.WithCancel(ctx)
	runCtx2, cancel2 := context.WithCancel(ctx)
	done1 := make(chan error, 1)
	done2 := make(chan error, 1)
	go func() { done1 <- sub1.Run(runCtx1) }()
	go func() { done2 <- sub2.Run(runCtx2) }()
	t.Cleanup(func() {
		cancel1()
		cancel2()
		select {
		case <-done1:
		case <-time.After(2 * time.Second):
		}
		select {
		case <-done2:
		case <-time.After(2 * time.Second):
		}
	})
	// 等订阅建立（PUBLISH 前已 SUBSCRIBE 的竞态由 Run 内的 Receive 保证；此处短睡确保两个 SUBSCRIBE 已完成）。
	time.Sleep(200 * time.Millisecond)

	require.NoError(t, NewStreamTransport(client).Enqueue(ctx, ev))

	frame1 := waitFrame(t, r1)
	frame2 := waitFrame(t, r2)
	require.Equal(t, ev.EventID, frame1["payload"].(map[string]any)["event_id"])
	require.Equal(t, ev.EventID, frame2["payload"].(map[string]any)["event_id"])
	require.NotContains(t, frame1["payload"].(map[string]any), "acl")
	require.NotContains(t, frame2["payload"].(map[string]any), "acl")

	require.Eventually(t, func() bool {
		row := outboxRow(t, db, ctx, ev.EventID)
		return row != nil && row.PublishedAt != nil
	}, 5*time.Second, 50*time.Millisecond)
}

// TestSubscriber_NoSubscribersStillMarksPublished：无订阅者仍标 published_at（合法事件、此刻无人听；重连不补历史）。
func TestSubscriber_NoSubscribersStillMarksPublished(t *testing.T) {
	sub, _, client, db := newSubscriberEnv(t)
	ctx := context.Background()

	ev := testEnvelope()
	require.NoError(t, infraevents.NewEventOutbox(db).Publish(ctx, ev))

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- sub.Run(runCtx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	})
	time.Sleep(200 * time.Millisecond)
	require.NoError(t, NewStreamTransport(client).Enqueue(ctx, ev))

	require.Eventually(t, func() bool {
		row := outboxRow(t, db, ctx, ev.EventID)
		return row != nil && row.PublishedAt != nil
	}, 5*time.Second, 50*time.Millisecond)
}

// TestSubscriber_DispatchUsesFullEnvelopeACL：Hub.Dispatch 用完整信封的 acl 过滤：无 read 权限的订阅者收不到，平台 admin 旁路全收。
func TestSubscriber_DispatchUsesFullEnvelopeACL(t *testing.T) {
	sub, hub, client, db := newSubscriberEnv(t)
	ctx := context.Background()

	ev := testEnvelope()
	require.NoError(t, infraevents.NewEventOutbox(db).Publish(ctx, ev))

	stranger := newTestConn("u2", databases.Principal{Roles: []string{"users", "user:u2"}})
	admin := newTestConn("adm", databases.Principal{Roles: []string{"admin"}})
	admin.PlatformAdmin = true
	hub.Subscribe(ev.CollectionChannel(), stranger)
	hub.Subscribe(ev.CollectionChannel(), admin)

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- sub.Run(runCtx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	})
	time.Sleep(200 * time.Millisecond)
	require.NoError(t, NewStreamTransport(client).Enqueue(ctx, ev))

	frame := waitFrame(t, admin)
	require.Equal(t, ev.EventID, frame["payload"].(map[string]any)["event_id"])
	require.Empty(t, drain(t, stranger.Send), "无 read 权不得收到事件")
}

func outboxRow(t *testing.T, db *clients.Database, ctx context.Context, eventID string) *model.DocumentEventsOutbox {
	t.Helper()
	var row model.DocumentEventsOutbox
	err := db.Conn(ctx).NewSelect().Model(&row).Where("event_id = ?", eventID).Scan(ctx)
	if err != nil {
		return nil
	}
	return &row
}
