package events

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	domainevents "github.com/torchwooddev/torchwood/internal/domain/events"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/torchwooddev/torchwood/internal/testutil"
)

// recordingTransport 记录 Enqueue 收到的信封（测试 transport 桩）。
type recordingTransport struct {
	enqueued []domainevents.Envelope
	fail     bool
	trims    int
}

func (t *recordingTransport) Enqueue(_ context.Context, ev domainevents.Envelope) error {
	if t.fail {
		return errors.New("xadd failed")
	}
	t.enqueued = append(t.enqueued, ev)
	return nil
}

func (t *recordingTransport) Trim(_ context.Context) error {
	t.trims++
	return nil
}

var _ shared.RealtimeTransport = (*recordingTransport)(nil)

func setupOutboxWorker(t *testing.T, transport shared.RealtimeTransport) *OutboxWorker {
	t.Helper()
	db := testutil.SetupTestDB(t)
	w := NewOutboxWorker(db, transport, nil)
	t.Cleanup(func() { _ = db.Close() })
	return w
}

// TestOutboxWorker_DispatchesAndMarksDispatchedAt：领取 → XADD（完整
// 信封）→ 只标 dispatched_at，**不**标 published_at。
func TestOutboxWorker_DispatchesAndMarksDispatchedAt(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	rec := &recordingTransport{}
	w := setupOutboxWorker(t, rec)
	db := w.db
	ctx := context.Background()

	ev := testEnvelope()
	ev.EventID = "test-event-1"
	require.NoError(t, NewEventOutbox(db).Publish(ctx, ev))

	require.NoError(t, w.pollOnce(ctx))

	require.Len(t, rec.enqueued, 1)
	require.Equal(t, ev.EventID, rec.enqueued[0].EventID)
	require.Equal(t, ev.ACL.DocumentPermissions, rec.enqueued[0].ACL.DocumentPermissions)

	row := fetchOutboxRow(t, db, ctx, ev.EventID)
	require.NotNil(t, row.DispatchedAt, "XADD 成功后必须标 dispatched_at")
	require.Nil(t, row.PublishedAt, "worker 不得标 published_at（由 subscriber 在 XACK 后标记）")

	// 再次 pollOnce：行已 dispatched，不再重复 XADD。
	require.NoError(t, w.pollOnce(ctx))
	require.Len(t, rec.enqueued, 1)
}

// TestOutboxWorker_RetriesWithBackoff：XADD 失败 attempts+1、available_at
// 退避到未来，行不被死信。
func TestOutboxWorker_RetriesWithBackoff(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	rec := &recordingTransport{fail: true}
	w := setupOutboxWorker(t, rec)
	db := w.db
	ctx := context.Background()

	ev := testEnvelope()
	ev.EventID = "test-event-1"
	require.NoError(t, NewEventOutbox(db).Publish(ctx, ev))

	before := time.Now()
	require.NoError(t, w.pollOnce(ctx))

	row := fetchOutboxRow(t, db, ctx, ev.EventID)
	require.Equal(t, 1, row.Attempts)
	require.NotNil(t, row.AvailableAt)
	require.True(t, row.AvailableAt.After(before), "available_at 必须退避到未来")
	require.Nil(t, row.DispatchedAt)
}

// TestOutboxWorker_DeadLettersAfterMaxAttempts：attempts 达到上限迁入
// 死信表，outbox 行删除。
func TestOutboxWorker_DeadLettersAfterMaxAttempts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	rec := &recordingTransport{fail: true}
	w := setupOutboxWorker(t, rec)
	db := w.db
	ctx := context.Background()

	ev := testEnvelope()
	ev.EventID = "test-event-1"
	require.NoError(t, NewEventOutbox(db).Publish(ctx, ev))
	_, err := db.Conn(ctx).NewUpdate().Model((*model.DocumentEventsOutbox)(nil)).
		Set("attempts = ?", maxOutboxAttempts-1).Where("event_id = ?", ev.EventID).Exec(ctx)
	require.NoError(t, err)

	require.NoError(t, w.pollOnce(ctx))

	require.Nil(t, fetchOutboxRow(t, db, ctx, ev.EventID), "死信后 outbox 行必须删除")
	var deadEventID, lastError string
	var deadAttempts int
	err = db.Conn(ctx).NewRaw(
		`SELECT event_id, last_error, attempts FROM document_events_outbox_dead WHERE event_id = ?`,
		ev.EventID).Scan(ctx, &deadEventID, &lastError, &deadAttempts)
	require.NoError(t, err)
	require.Equal(t, ev.EventID, deadEventID)
	require.Equal(t, maxOutboxAttempts, deadAttempts)
	require.Contains(t, lastError, "xadd failed")
}

// TestOutboxWorker_RedispatchAfterTwoMinutes：dispatched_at 超过 2min
// 仍未 published（整进程挂死兜底）→ 重新领取再 XADD。
func TestOutboxWorker_RedispatchAfterTwoMinutes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	rec := &recordingTransport{}
	w := setupOutboxWorker(t, rec)
	db := w.db
	ctx := context.Background()

	ev := testEnvelope()
	ev.EventID = "test-event-1"
	require.NoError(t, NewEventOutbox(db).Publish(ctx, ev))
	stale := time.Now().Add(-3 * time.Minute)
	_, err := db.Conn(ctx).NewUpdate().Model((*model.DocumentEventsOutbox)(nil)).
		Set("dispatched_at = ?", stale).Where("event_id = ?", ev.EventID).Exec(ctx)
	require.NoError(t, err)

	require.NoError(t, w.pollOnce(ctx))
	require.Len(t, rec.enqueued, 1, "2min 兜底必须重新领取再 XADD")

	row := fetchOutboxRow(t, db, ctx, ev.EventID)
	require.NotNil(t, row.DispatchedAt)
	require.True(t, row.DispatchedAt.After(stale), "回收分支同样刷新 dispatched_at")
}

func fetchOutboxRow(t *testing.T, db *clients.Database, ctx context.Context, eventID string) *model.DocumentEventsOutbox {
	t.Helper()
	var row model.DocumentEventsOutbox
	err := db.Conn(ctx).NewSelect().Model(&row).Where("event_id = ?", eventID).Scan(ctx)
	if err != nil {
		return nil
	}
	return &row
}
