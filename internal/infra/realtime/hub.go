package realtime

import (
	"log/slog"
	"sync"
	"time"

	"github.com/torchwooddev/torchwood/internal/domain/events"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
)

// connSendBuffer 是每连接发送缓冲（帧数）。慢客户端落后则丢事件，
// 不阻塞 subscriber（重连不补历史）。
const connSendBuffer = 64

// dedupWindow 是 Hub 按 event_id 去重的时间窗（回收重放 / 崩溃重投
// 的重复条目不再二次扇出；窗口覆盖 2min 兜底 + 30s idle 认领余量）。
// var 而非 const：测试覆写缩短窗口验证刷新语义。
var dedupWindow = 5 * time.Minute

// dedupMax 是 event_id 去重表上限；超限先清理过期条目。
const dedupMax = 4096

// Conn 是 domain 端口 RealtimeConn 的别名（Hub 实现 shared.RealtimeHub
// 端口，连接句柄定义在 domain 层供 api 层握手代码使用）。
type Conn = shared.RealtimeConn

// Hub 是单进程内存订阅表：map[channel]map[connID]*Conn（v2 设计
// §4.5）。只存在于 cmd/server 进程。
type Hub struct {
	logger *slog.Logger

	mu       sync.RWMutex
	channels map[string]map[string]*Conn
	subCount int
	dedup    map[string]time.Time
}

// NewHub 构造 Hub。logger 可为 nil（回退 slog.Default）。
func NewHub(logger *slog.Logger) *Hub {
	if logger == nil {
		logger = slog.Default()
	}
	return &Hub{
		logger:   logger,
		channels: make(map[string]map[string]*Conn),
		dedup:    make(map[string]time.Time),
	}
}

// Subscribe 把 conn 注册到频道（幂等：同一连接重复订阅不重复计数）。
func (h *Hub) Subscribe(channel string, conn *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	m := h.channels[channel]
	if m == nil {
		m = make(map[string]*Conn)
		h.channels[channel] = m
	}
	if _, ok := m[conn.ID]; !ok {
		h.subCount++
	}
	m[conn.ID] = conn
	RealtimeSubscriptions.Set(float64(h.subCount))
}

// Unsubscribe 把 conn 从单个频道移除。
func (h *Hub) Unsubscribe(channel, connID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	m := h.channels[channel]
	if m == nil {
		return
	}
	if _, ok := m[connID]; ok {
		delete(m, connID)
		h.subCount--
	}
	if len(m) == 0 {
		delete(h.channels, channel)
	}
	RealtimeSubscriptions.Set(float64(h.subCount))
}

// Remove 把 conn 从全部频道移除（连接关闭时调用）。
func (h *Hub) Remove(connID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch, m := range h.channels {
		if _, ok := m[connID]; ok {
			delete(m, connID)
			h.subCount--
		}
		if len(m) == 0 {
			delete(h.channels, ch)
		}
	}
	RealtimeSubscriptions.Set(float64(h.subCount))
}

// Dispatch 收**完整**信封（含 ev.ACL），按集合 + 文档两个频道扇出。
// 连接只收 PlatformAdmin 旁路或 VisibleTo(ev.ACL) 通过的订阅者；
// 入队帧只用 ev.ClientPayload()（剥掉 acl），发送 chan 满载则丢事件。
// 经济事件（Domain 非空，v3 设计 §5.1/D17）：只扇出显式 Channel，无 acl——
// 可见性由频道本身保证（订阅侧 parseChannel 派发表仅允许本人订阅）。
func (h *Hub) Dispatch(ev events.Envelope) {
	if !h.markSeen(ev.EventID) {
		return // 重复 event_id（回收重放 / PEL 重投）
	}
	payload := ev.ClientPayload()
	var channels []string
	if ev.IsEconomy() {
		channels = []string{ev.Channel}
	} else {
		channels = []string{ev.CollectionChannel(), ev.DocumentChannel()}
	}
	for _, ch := range channels {
		h.mu.RLock()
		subs := make([]*Conn, 0, 8)
		for _, c := range h.channels[ch] {
			subs = append(subs, c)
		}
		h.mu.RUnlock()
		for _, c := range subs {
			if !ev.IsEconomy() && !c.PlatformAdmin && !events.VisibleTo(ev.ACL, c.DocPrincipal) {
				continue
			}
			frame := map[string]any{"type": "event", "channel": ch, "payload": payload}
			select {
			case c.Send <- frame:
				RealtimeEventsDeliveredTotal.WithLabelValues(ev.Event).Inc()
			default:
				RealtimeEventsDroppedTotal.WithLabelValues("slow_consumer").Inc()
				h.logger.Warn("realtime slow consumer dropped event",
					"event_id", ev.EventID, "connection_id", c.ID, "channel", ch)
			}
		}
	}
}

// markSeen 记录 event_id 并返回该事件是否首次出现（去重窗口内）。
func (h *Hub) markSeen(eventID string) bool {
	if eventID == "" {
		return true
	}
	now := time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.dedup[eventID]; ok {
		// 命中刷新时间戳（P1-13）：redispatch 每 2min 重发同一 event 时窗口
		// 随最新一次见到的时间滑动——否则窗口从首见起算，标记持续失败
		// >dedupWindow 后客户端会收到可见重复帧。
		h.dedup[eventID] = now
		return false
	}
	if len(h.dedup) >= dedupMax {
		for id, t := range h.dedup {
			if now.Sub(t) > dedupWindow {
				delete(h.dedup, id)
			}
		}
		if len(h.dedup) >= dedupMax {
			// 窗口内活跃事件超过上限时放弃登记新 ID（仍视为首次出现）：
			// 查重仍由已登记条目保证，内存不再无界增长。代价是极端洪峰下
			// 未登记的 event_id 重放时可能二次扇出，由客户端按 id 去重兜底
			//（v2 设计 at-least-once 语义本就要求客户端去重）。
			return true
		}
	}
	h.dedup[eventID] = now
	return true
}
