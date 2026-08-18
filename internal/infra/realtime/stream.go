package realtime

import (
	"context"

	"github.com/redis/go-redis/v9"
	"github.com/torchwooddev/torchwood/internal/domain/events"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	infraevents "github.com/torchwooddev/torchwood/internal/infra/events"
)

const (
	// streamKey 是实时事件的 Redis Stream 键（outbox → Hub 最后一跳）。
	streamKey = "torchwood:realtime"
	// streamMaxLen 是 XADD 近似裁剪上限（MAXLEN ~ 50000）。
	streamMaxLen = 50000
)

// streamTransport 是 shared.RealtimeTransport 的 Redis Streams 实现
// （v2 设计 §3.4）：worker XADD 完整信封 JSON（含 acl，与
// outbox.payload 同形），XADD 必须带近似裁剪避免 Stream 无限涨。
type streamTransport struct {
	client *redis.Client
}

// NewStreamTransport 构造 Stream 写入端。
func NewStreamTransport(client *redis.Client) shared.RealtimeTransport {
	return &streamTransport{client: client}
}

func (t *streamTransport) Enqueue(ctx context.Context, ev events.Envelope) error {
	payload, err := infraevents.MarshalEnvelope(ev)
	if err != nil {
		return err
	}
	return t.client.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		MaxLen: streamMaxLen,
		Approx: true,
		// payload 以 string 提交（go-redis 无法直接 marshal json.RawMessage）。
		Values: map[string]any{"payload": string(payload)},
	}).Err()
}

var _ shared.RealtimeTransport = (*streamTransport)(nil)
