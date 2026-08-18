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

// newSubscriberEnv 组装 subscriber 集成测试环境：真实 Postgres（迁移含
// outbox 表）+ miniredis Stream。claimIdle 缩短到 100ms 加速重投测试。
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
	sub.claimIdle = 100 * time.Millisecond
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

// TestSubscriber_ConsumesPreGroupXAdd：worker 先于 server 启动时，建组前
// XADD 的条目仍被 XGROUP CREATE 0-0 + XREADGROUP > 消费（组 ID 不能用 $）。
func TestSubscriber_ConsumesPreGroupXAdd(t *testing.T) {
	sub, hub, client, db := newSubscriberEnv(t)
	ctx := context.Background()

	// 建组前先 XADD（模拟 worker 先启动）。
	ev := testEnvelope()
	require.NoError(t, infraevents.NewEventOutbox(db).Publish(ctx, ev))
	transport := NewStreamTransport(client)
	require.NoError(t, transport.Enqueue(ctx, ev))

	reader := newTestConn("u1", databases.Principal{Roles: []string{"users", "user:u1"}})
	hub.Subscribe(ev.CollectionChannel(), reader)

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

	// 事件被扇出到订阅者；帧为 ClientPayload（无 acl）。
	frame := waitFrame(t, reader)
	payload := frame["payload"].(map[string]any)
	require.Equal(t, ev.EventID, payload["event_id"])
	require.NotContains(t, payload, "acl")

	// XACK 后 outbox 标 published_at；PEL 为空。
	require.Eventually(t, func() bool {
		row := outboxRow(t, db, ctx, ev.EventID)
		return row != nil && row.PublishedAt != nil
	}, 5*time.Second, 50*time.Millisecond)
	require.Eventually(t, func() bool {
		pel, err := client.XPending(ctx, streamKey, realtimeGroup).Result()
		return err == nil && pel.Count == 0
	}, 5*time.Second, 50*time.Millisecond)
}

// TestSubscriber_NoSubscribersStillAcks：无订阅者仍 XACK + 标 published_at
// （合法事件、此刻无人听；重连不补历史）。
func TestSubscriber_NoSubscribersStillAcks(t *testing.T) {
	sub, _, client, db := newSubscriberEnv(t)
	ctx := context.Background()

	ev := testEnvelope()
	require.NoError(t, infraevents.NewEventOutbox(db).Publish(ctx, ev))
	require.NoError(t, NewStreamTransport(client).Enqueue(ctx, ev))

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

	require.Eventually(t, func() bool {
		row := outboxRow(t, db, ctx, ev.EventID)
		return row != nil && row.PublishedAt != nil
	}, 5*time.Second, 50*time.Millisecond)
	require.Eventually(t, func() bool {
		pel, err := client.XPending(ctx, streamKey, realtimeGroup).Result()
		return err == nil && pel.Count == 0
	}, 5*time.Second, 50*time.Millisecond)
}

// TestSubscriber_RedeliversUnackedBeforeTwoMinutes：读出后、XACK 前杀掉
// subscriber，重启后在远小于 2min（约 claimIdle）内再投同一 event_id。
func TestSubscriber_RedeliversUnackedBeforeTwoMinutes(t *testing.T) {
	sub, hub, client, db := newSubscriberEnv(t)
	ctx := context.Background()

	ev := testEnvelope()
	require.NoError(t, infraevents.NewEventOutbox(db).Publish(ctx, ev))
	require.NoError(t, NewStreamTransport(client).Enqueue(ctx, ev))

	// 模拟"旧 subscriber"读出但不 XACK：条目进入 PEL。
	require.NoError(t, sub.ensureGroup(ctx))
	_, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    realtimeGroup,
		Consumer: "crashed-subscriber",
		Streams:  []string{streamKey, ">"},
		Count:    1,
	}).Result()
	require.NoError(t, err)

	reader := newTestConn("u1", databases.Principal{Roles: []string{"users", "user:u1"}})
	hub.Subscribe(ev.CollectionChannel(), reader)

	start := time.Now()
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

	// 重启后 XAUTOCLAIM 认领同一 event_id 并重投（远小于 2min）。
	frame := waitFrame(t, reader)
	require.Equal(t, ev.EventID, frame["payload"].(map[string]any)["event_id"])
	require.Less(t, time.Since(start), 2*time.Minute)

	require.Eventually(t, func() bool {
		row := outboxRow(t, db, ctx, ev.EventID)
		return row != nil && row.PublishedAt != nil
	}, 5*time.Second, 50*time.Millisecond)
	require.Eventually(t, func() bool {
		pel, err := client.XPending(ctx, streamKey, realtimeGroup).Result()
		return err == nil && pel.Count == 0
	}, 5*time.Second, 50*time.Millisecond)
}

// TestSubscriber_DispatchUsesFullEnvelopeACL：Hub.Dispatch 用完整信封的
// acl 过滤：无 read 权限的订阅者收不到，平台 admin 旁路全收。
func TestSubscriber_DispatchUsesFullEnvelopeACL(t *testing.T) {
	sub, hub, client, db := newSubscriberEnv(t)
	ctx := context.Background()

	ev := testEnvelope()
	require.NoError(t, infraevents.NewEventOutbox(db).Publish(ctx, ev))
	require.NoError(t, NewStreamTransport(client).Enqueue(ctx, ev))

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
