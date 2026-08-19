package client

// Realtime 实现 /v1/realtime WebSocket 订阅（v2 设计 §4.2/§4.3，TS SDK
// 对等能力）：hello 握手携带 Client JWT access token、subscribe/unsubscribe
// 帧、服务端 30s ping 回 pong。JWT 到期服务端以 token_expired 关连接
// （无连接内 refresh）：断线后刷新 access token → 重新 hello → 重订全部
// 频道，不补历史；重连采用指数退避。version 跳号 / truncated 由调用方
// 自行 GetDocument 补读。Realtime 不在 Server SDK 提供（API Key 不能连）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	// 默认重连退避：500ms 起步，翻倍至 10s 封顶。
	defaultRealtimeBackoffInitial = 500 * time.Millisecond
	defaultRealtimeBackoffMax     = 10 * time.Second

	realtimeWriteTimeout = 10 * time.Second
	realtimeHelloTimeout = 10 * time.Second
)

// RealtimeEvent 是一条实时事件（payload 为 Envelope.ClientPayload()，无 acl）。
type RealtimeEvent struct {
	Channel string
	Payload map[string]any
}

// RealtimeHandler 处理订阅频道的事件。
type RealtimeHandler func(RealtimeEvent)

// RealtimeOption 修改 RealtimeConn 配置。
type RealtimeOption func(*RealtimeConn)

// WithRealtimeBackoff 设置重连退避（默认 500ms 起步，10s 封顶；测试可调小）。
func WithRealtimeBackoff(initial, max time.Duration) RealtimeOption {
	return func(r *RealtimeConn) {
		r.backoffInitial = initial
		r.backoffMax = max
	}
}

// realtimeHelloFrame 是客户端首帧（§4.2）。
type realtimeHelloFrame struct {
	Type        string `json:"type"`
	ProjectID   string `json:"project_id"`
	AccessToken string `json:"access_token,omitempty"`
}

// realtimeInboundFrame 是客户端控制帧。
type realtimeInboundFrame struct {
	Type    string `json:"type"`
	ID      string `json:"id,omitempty"`
	Channel string `json:"channel,omitempty"`
}

// realtimeServerFrame 是服务端出站帧。
type realtimeServerFrame struct {
	Type    string         `json:"type"`
	ID      string         `json:"id"`
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Channel string         `json:"channel"`
	Payload map[string]any `json:"payload"`
}

// RealtimeSubscription 是退订句柄。
type RealtimeSubscription struct {
	rc      *RealtimeConn
	channel string
	handler RealtimeHandler
}

// Unsubscribe 退订；连接已断开时仅本地移除，重连后不再重订。
func (s *RealtimeSubscription) Unsubscribe() {
	s.rc.unsubscribe(s)
}

// RealtimeConn 是 /v1/realtime 的连接句柄（断线自动重连 + 重订）。
type RealtimeConn struct {
	client         *Client
	url            string
	backoffInitial time.Duration
	backoffMax     time.Duration

	mu     sync.Mutex
	subs   map[string]map[*RealtimeSubscription]RealtimeHandler
	subIDs map[string]string
	subSeq int
	conn   *websocket.Conn // 当前连接；nil 表示处于重连间隙
	closed bool

	writeMu sync.Mutex // coder/websocket 只允许单并发写
	done    chan struct{}
	once    sync.Once
}

// ConnectRealtime 建立实时连接并完成 hello 握手（同步返回握手错误）。
// httpEndpoint 为 HTTP 网关地址（如 http://localhost:9099），内部转为
// ws(s)://…/v1/realtime。access token 取自 Client 的 TokenStore（到期自动
// 刷新），断线重连前强制刷新（§4.2：不补历史）。
func (c *Client) ConnectRealtime(ctx context.Context, httpEndpoint string, opts ...RealtimeOption) (*RealtimeConn, error) {
	if c.cfg.ProjectID == "" {
		return nil, errors.New("realtime requires WithProjectID")
	}
	u, err := realtimeURL(httpEndpoint)
	if err != nil {
		return nil, err
	}
	r := &RealtimeConn{
		client:         c,
		url:            u,
		backoffInitial: defaultRealtimeBackoffInitial,
		backoffMax:     defaultRealtimeBackoffMax,
		subs:           make(map[string]map[*RealtimeSubscription]RealtimeHandler),
		subIDs:         make(map[string]string),
		done:           make(chan struct{}),
	}
	for _, o := range opts {
		o(r)
	}
	if err := r.dial(ctx); err != nil {
		return nil, err
	}
	go r.run()
	return r, nil
}

// realtimeURL 把 http(s)://host[/...] 转为 ws(s)://host/v1/realtime。
func realtimeURL(endpoint string) (string, error) {
	if endpoint == "" {
		return "", errors.New("realtime endpoint cannot be empty")
	}
	rest := strings.TrimSuffix(endpoint, "/")
	switch {
	case strings.HasPrefix(rest, "https://"):
		return "wss://" + strings.TrimPrefix(rest, "https://") + "/v1/realtime", nil
	case strings.HasPrefix(rest, "http://"):
		return "ws://" + strings.TrimPrefix(rest, "http://") + "/v1/realtime", nil
	default:
		return "", fmt.Errorf("realtime endpoint must start with http:// or https://: %q", endpoint)
	}
}

// realtimeAccessToken 取当前 access token（临期先主动刷新）。
func (c *Client) realtimeAccessToken(ctx context.Context) (string, error) {
	if err := c.refreshIfExpiring(ctx); err != nil {
		return "", err
	}
	tok, _ := c.store.Load()
	if tok == nil {
		return "", nil
	}
	return tok.AccessToken, nil
}

// forceRefreshToken 断线重连前强制用 refresh token 换新（§4.2 token_expired
// 场景）；失败时保留现状，hello 仍用旧 token，由下一轮退避重试。
func (c *Client) forceRefreshToken(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	tok, _ := c.store.Load()
	if tok == nil || tok.RefreshToken == "" {
		return
	}
	_ = c.doRefreshLocked(ctx, tok.RefreshToken)
}

// dial 建立 WS 连接并完成 hello 握手（hello_ok 或 error 帧）。
func (r *RealtimeConn) dial(ctx context.Context) error {
	token, err := r.client.realtimeAccessToken(ctx)
	if err != nil {
		return err
	}
	conn, _, err := websocket.Dial(ctx, r.url, nil)
	if err != nil {
		return err
	}
	ctxWrite, cancel := context.WithTimeout(ctx, realtimeWriteTimeout)
	err = conn.Write(ctxWrite, websocket.MessageText, mustMarshal(&realtimeHelloFrame{
		Type:        "hello",
		ProjectID:   r.client.cfg.ProjectID,
		AccessToken: token,
	}))
	cancel()
	if err != nil {
		conn.CloseNow()
		return err
	}
	// 握手应答：hello_ok 或 error（服务端随后关连接）。
	ctxRead, cancel := context.WithTimeout(ctx, realtimeHelloTimeout)
	_, data, err := conn.Read(ctxRead)
	cancel()
	if err != nil {
		conn.CloseNow()
		return fmt.Errorf("realtime handshake: %w", err)
	}
	var f realtimeServerFrame
	if err := json.Unmarshal(data, &f); err != nil {
		conn.CloseNow()
		return fmt.Errorf("realtime handshake: %w", err)
	}
	if f.Type != "hello_ok" {
		conn.CloseNow()
		if f.Type == "error" {
			return fmt.Errorf("realtime handshake: %s: %s", f.Code, f.Message)
		}
		return fmt.Errorf("realtime handshake: unexpected frame type %q", f.Type)
	}
	r.mu.Lock()
	r.conn = conn
	r.mu.Unlock()
	return nil
}

// run 是连接生命周期循环：读循环退出后（非主动关闭）刷新 token、
// 退避重连并重订全部频道（不补历史）。
func (r *RealtimeConn) run() {
	attempt := 0
	for {
		r.readLoop()
		if r.isClosed() {
			return
		}
		r.mu.Lock()
		old := r.conn
		r.conn = nil
		r.mu.Unlock()
		if old != nil {
			old.CloseNow()
		}
		r.client.forceRefreshToken(context.Background())

		delay := r.backoffInitial << min(attempt, 20)
		if delay > r.backoffMax {
			delay = r.backoffMax
		}
		attempt++
		select {
		case <-r.done:
			return
		case <-time.After(delay):
		}
		if err := r.dial(context.Background()); err != nil {
			continue
		}
		attempt = 0
		r.resubscribeAll()
	}
}

// readLoop 逐帧处理服务端消息，直到连接断开。
func (r *RealtimeConn) readLoop() {
	for {
		r.mu.Lock()
		conn := r.conn
		r.mu.Unlock()
		if conn == nil {
			return
		}
		_, data, err := conn.Read(context.Background())
		if err != nil {
			return
		}
		var f realtimeServerFrame
		if err := json.Unmarshal(data, &f); err != nil {
			continue
		}
		switch f.Type {
		case "event":
			r.dispatch(f.Channel, f.Payload)
		case "ping":
			r.writeFrame(conn, &realtimeInboundFrame{Type: "pong"})
		case "subscribed", "pong", "error":
			// subscribed/pong 无需处理；订阅级 error（NOT_FOUND /
			// RESOURCE_EXHAUSTED）连接保持，由调用方决定是否退订。
		}
	}
}

// dispatch 把事件分发给频道订阅者。
func (r *RealtimeConn) dispatch(channel string, payload map[string]any) {
	r.mu.Lock()
	handlers := make([]RealtimeHandler, 0, len(r.subs[channel]))
	for _, h := range r.subs[channel] {
		handlers = append(handlers, h)
	}
	r.mu.Unlock()
	ev := RealtimeEvent{Channel: channel, Payload: payload}
	for _, h := range handlers {
		h(ev)
	}
}

// resubscribeAll 重连成功后重订全部频道（复用原订阅 id，不补历史）。
func (r *RealtimeConn) resubscribeAll() {
	r.mu.Lock()
	conn := r.conn
	channels := make([]string, 0, len(r.subs))
	for ch := range r.subs {
		channels = append(channels, ch)
	}
	r.mu.Unlock()
	for _, ch := range channels {
		r.writeFrame(conn, &realtimeInboundFrame{Type: "subscribe", ID: r.subID(ch), Channel: ch})
	}
}

// Subscribe 订阅频道；连接已就绪时立即发送 subscribe 帧，否则重连后
// 自动补订。返回退订句柄。
func (r *RealtimeConn) Subscribe(channel string, h RealtimeHandler) *RealtimeSubscription {
	sub := &RealtimeSubscription{rc: r, channel: channel, handler: h}
	r.mu.Lock()
	set := r.subs[channel]
	if set == nil {
		set = make(map[*RealtimeSubscription]RealtimeHandler)
		r.subs[channel] = set
	}
	set[sub] = h
	conn := r.conn
	first := len(set) == 1
	r.mu.Unlock()
	if first && conn != nil {
		r.writeFrame(conn, &realtimeInboundFrame{Type: "subscribe", ID: r.subID(channel), Channel: channel})
	}
	return sub
}

func (r *RealtimeConn) unsubscribe(s *RealtimeSubscription) {
	r.mu.Lock()
	set := r.subs[s.channel]
	if set == nil {
		r.mu.Unlock()
		return
	}
	delete(set, s)
	if len(set) > 0 {
		r.mu.Unlock()
		return
	}
	delete(r.subs, s.channel)
	id := r.subIDs[s.channel]
	delete(r.subIDs, s.channel)
	conn := r.conn
	r.mu.Unlock()
	if conn != nil {
		r.writeFrame(conn, &realtimeInboundFrame{Type: "unsubscribe", ID: id, Channel: s.channel})
	}
}

// subID 返回频道稳定的订阅 id（调用方须持 r.mu 或在读快照后使用）。
func (r *RealtimeConn) subID(channel string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.subIDs[channel]
	if !ok {
		r.subSeq++
		id = fmt.Sprintf("c%d", r.subSeq)
		r.subIDs[channel] = id
	}
	return id
}

// writeFrame 序列化并写入一帧（单并发写 + 写超时）。
func (r *RealtimeConn) writeFrame(conn *websocket.Conn, f *realtimeInboundFrame) {
	if conn == nil {
		return
	}
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), realtimeWriteTimeout)
	defer cancel()
	_ = conn.Write(ctx, websocket.MessageText, mustMarshal(f))
}

// Close 主动关闭：停止重连并关闭底层连接。
func (r *RealtimeConn) Close() error {
	r.once.Do(func() {
		r.mu.Lock()
		r.closed = true
		conn := r.conn
		r.mu.Unlock()
		close(r.done)
		if conn != nil {
			_ = conn.Close(websocket.StatusNormalClosure, "")
		}
	})
	return nil
}

func (r *RealtimeConn) isClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

func mustMarshal(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err) // 帧结构均为可序列化字段，不可达
	}
	return data
}
