package realtime

import (
	"context"

	"github.com/redis/go-redis/v9"
	"github.com/torchwooddev/torchwood/internal/domain/events"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	infraevents "github.com/torchwooddev/torchwood/internal/infra/events"
)

const (
	// streamKey 是实时事件的 Redis Stream 键（历史，保留用于 metrics 兼容）。
	streamKey = "torchwood:realtime"
	// realtimeChannel 是广播扇出的 Pub/Sub 频道（A6：每副本都收到，不再消费组抢同一条）。
	realtimeChannel = "torchwood:realtime"
	// streamMaxLen 是历史 XADD 近似裁剪上限（保留未用）。
	streamMaxLen = 50000
)

// streamTransport 是 shared.RealtimeTransport 的 Redis Pub/Sub 实现
// （A6 修复：worker PUBLISH 完整信封 JSON（含 acl，与 outbox.payload 同形），
// server 端 SUBSCRIBE 广播，替代消费者组抢占语义）。
type streamTransport struct {
	client *redis.Client
}

// NewStreamTransport 构造 Pub/Sub 写入端。
func NewStreamTransport(client *redis.Client) shared.RealtimeTransport {
	return &streamTransport{client: client}
}

func (t *streamTransport) Enqueue(ctx context.Context, ev events.Envelope) error {
	payload, err := infraevents.MarshalEnvelope(ev)
	if err != nil {
		return err
	}
	// WHY: Pub/Sub 广播保证多 server 副本各自 Hub 都收到同一事件，consumer group 的“每条只给一个消费者”会导致在线副本静默。
	return t.client.Publish(ctx, realtimeChannel, string(payload)).Err()
}

var _ shared.RealtimeTransport = (*streamTransport)(nil)
