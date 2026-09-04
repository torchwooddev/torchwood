package realtime

import (
	"context"

	"github.com/redis/go-redis/v9"
	"github.com/torchwooddev/torchwood/internal/domain/events"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	infraevents "github.com/torchwooddev/torchwood/internal/infra/events"
)

const (
	// eventsStream 是事件投递的 Redis Stream（阶段④ B3，§4.5）：
	// worker dispatcher XADD 完整信封 JSON（含 acl + seq），每个 server
	// 实例一个消费组（见 subscriber.go）XREADGROUP 消费后 XACK。
	// 回到 Stream 是有意决策：消费组模式同样解决多副本可见性，且换来
	// 不丢帧与位点回放（A6 的 Pub/Sub 广播在阶段④被取代）。
	eventsStream = "torchwood:events"
)

// eventsStreamMaxLen 是周期 XTRIM 水位（近似裁剪）：Stream 只是投递
// 通道，不承担重放（重放窗口在 outbox 表）；100k 条覆盖消费组最大
// 在途积压 + 实例重启追平窗口，内存有界。var 而非 const：测试覆写
// 缩小水位验证裁剪语义（同 hub.dedupWindow 先例）。
var eventsStreamMaxLen int64 = 100000

// streamTransport 是 shared.RealtimeTransport 的 Redis Stream 实现
//（阶段④：XADD 替代 PUBLISH）。
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
	// WHY: XADD 不带 MAXLEN——未投递消息不得被近似裁剪丢弃（积压治理
	// 交给 Trim 的低频周期调用，水位见 eventsStreamMaxLen）。
	return t.client.XAdd(ctx, &redis.XAddArgs{
		Stream: eventsStream,
		Values: map[string]any{"payload": string(payload)},
	}).Err()
}

func (t *streamTransport) Trim(ctx context.Context) error {
	return t.client.XTrimMaxLenApprox(ctx, eventsStream, eventsStreamMaxLen, 0).Err()
}

var _ shared.RealtimeTransport = (*streamTransport)(nil)
