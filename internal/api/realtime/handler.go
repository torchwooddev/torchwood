// Package realtime 实现 GET /v1/realtime 的 WebSocket 门面（v2 设计
// §4）：hello 握手（SDK 首帧 JWT / Console cookie，拒 API Key 与 guest）、
// 订阅协议、配额（4 连 / 32 订）、ping 滑窗保活与 JWT 到期断开。
// handler 只做握手与帧编解码，订阅校验调 databases.DocumentDB 端口，
// 扇出走 shared.RealtimeHub 端口（实现位于 internal/infra/realtime）。
package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/grpc/interceptor"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/pkg/idgen"
	"github.com/torchwooddev/torchwood/pkg/jwtparser"
)

// 保活与超时（v2 设计 §4.1 / §4.3）：http.Server 读写超时已由 gateway
// 置 0；这里每条 Read/Write 前设短 deadline（收到帧 / 写完即释放），
// 服务端每 30s 发 ping，2 个间隔未收到 pong 则断开。
const (
	helloTimeout      = 10 * time.Second
	frameReadTimeout  = 60 * time.Second
	frameWriteTimeout = 10 * time.Second
	pingInterval      = 30 * time.Second
	pongWindow        = 2 * pingInterval
	// maxClientFrameBytes 是客户端帧（hello/subscribe/ping）上限。
	maxClientFrameBytes = 1 << 20
)

// 出站错误码（与协议约定一致，客户端按码分支）。
const (
	errCodeUnauthenticated   = "UNAUTHENTICATED"
	errCodeResourceExhausted = "RESOURCE_EXHAUSTED"
	errCodeNotFound          = "NOT_FOUND"
	errCodeInvalidArgument   = "INVALID_ARGUMENT"
	errCodeInternal          = "INTERNAL"
)

// CredentialValidator 是握手所需的凭证校验面（auth.Validator 满足；
// 接口化便于单测）。
type CredentialValidator interface {
	Authenticate(ctx context.Context, req shared.AuthnRequest) (*shared.Principal, error)
	ValidateToken(ctx context.Context, token string) (*shared.Principal, error)
	ValidateCredential(ctx context.Context, raw string, credentialType shared.CredentialType) (*shared.Principal, error)
	ValidateAdminProjectAccess(ctx context.Context, principal *shared.Principal) error
	// ParseClaims 解析 JWT claims（ttp / exp 检查用），非 JWT 凭证返回 false。
	ParseClaims(raw string) (*jwtparser.Claims, bool)
}

// Handler 是 /v1/realtime 的 WS handler。
type Handler struct {
	cfg       *config.AppConfig
	validator CredentialValidator
	docDB     databases.DocumentDB
	hub       shared.RealtimeHub
	conns     *connectionRegistry
	logger    *slog.Logger
}

// NewHandler 构造 WS handler。
func NewHandler(
	cfg *config.AppConfig,
	validator CredentialValidator,
	docDB databases.DocumentDB,
	hub shared.RealtimeHub,
	logger *slog.Logger,
) (*Handler, error) {
	if cfg == nil {
		return nil, errors.New("config cannot be nil")
	}
	if validator == nil {
		return nil, errors.New("validator cannot be nil")
	}
	if docDB == nil {
		return nil, errors.New("document db cannot be nil")
	}
	if hub == nil {
		return nil, errors.New("hub cannot be nil")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		cfg:       cfg,
		validator: validator,
		docDB:     docDB,
		hub:       hub,
		conns:     newConnectionRegistry(),
		logger:    logger,
	}, nil
}

// ServeHTTP 处理 WS 升级。握手（hello 帧）在升级后进行。
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	conn, err := websocket.Accept(w, r, h.acceptOptions())
	if err != nil {
		return // Accept 已写 HTTP 错误响应
	}
	// 关于 hijack 后 conn deadline（v2 设计 §4.1 第 2 件事）：gateway 的
	// http.Server 已置 ReadTimeout=0/WriteTimeout=0（grpc_gateway.go），
	// listener 从不向 net.Conn 打 deadline，hijack 后无 deadline 可清。
	// 设计示例里的 websocket.NetConn + SetReadDeadline 实测（coder/websocket
	// v1.8.14 netconn.go）只操作包装层自己的计时器，够不到底层 conn，
	// 故此处不做空调用；保活完全靠下面的帧级 deadline + ping 滑窗。
	defer func() { _ = conn.CloseNow() }()
	h.serveConn(r, conn)
}

// acceptOptions 组装 origin 校验：沿用现有 CORS 配置（allow_origins
// 作为 OriginPatterns；配置 "*" 时放开校验，与 CORSMiddleware 一致）。
func (h *Handler) acceptOptions() *websocket.AcceptOptions {
	opts := &websocket.AcceptOptions{}
	if srv := h.cfg.GetServer(); srv != nil && srv.GetHttp() != nil {
		if cors := srv.GetHttp().GetCors(); cors != nil {
			patterns := make([]string, 0, len(cors.GetAllowOrigins()))
			for _, o := range cors.GetAllowOrigins() {
				if o == "*" {
					opts.InsecureSkipVerify = true
					continue
				}
				patterns = append(patterns, o)
			}
			opts.OriginPatterns = patterns
		}
	}
	return opts
}

// helloFrame 是客户端首帧（SDK 必带 access_token；Console cookie 可省略）。
type helloFrame struct {
	Type        string `json:"type"`
	ProjectID   string `json:"project_id"`
	AccessToken string `json:"access_token"`
}

// inboundFrame 是客户端控制帧。
type inboundFrame struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Channel string `json:"channel"`
}

// outboundFrame 是服务端出站帧（事件帧由 Hub 组装为 map 直写）。
type outboundFrame struct {
	Type         string `json:"type"`
	ID           string `json:"id,omitempty"`
	Code         string `json:"code,omitempty"`
	Message      string `json:"message,omitempty"`
	ConnectionID string `json:"connection_id,omitempty"`
	Channel      string `json:"channel,omitempty"`
}

// connState 是一条 WS 连接的运行时状态。
type connState struct {
	id            string
	projectID     string
	principal     *shared.Principal
	platformAdmin bool
	docPrincipal  databases.Principal
	quotaKey      string
	expiresAt     time.Time // JWT 到期时间；WS 只收 JWT 凭证（SDK access_token / console cookie 均为 JWT），正常不会为零值

	hubConn *shared.RealtimeConn
	subs    map[string]struct{} // 已订阅频道（去重 + 计数）

	lastPong atomic.Int64 // unix nano；hello_ok 后置初值
	cancel   context.CancelFunc
	// expiryTimer 是 JWT 到期关连接定时器；连接提前断开时必须 Stop
	// （P2-5：否则 timer 持有 conn 状态直至 token 到期，高 churn 下累积）。
	expiryTimer *time.Timer
	closeMu     sync.Mutex
	closed      bool
}

func newConnState(principal *shared.Principal, projectID, quotaKey string, claims *jwtparser.Claims) *connState {
	connID := idgen.ULID().String()
	st := &connState{
		id:            connID,
		projectID:     projectID,
		principal:     principal,
		platformAdmin: principal.IsPlatformAdmin,
		docPrincipal:  principal.DocPrincipal(),
		quotaKey:      quotaKey,
		subs:          make(map[string]struct{}),
	}
	if claims != nil && claims.ExpiresAt > 0 {
		st.expiresAt = time.Unix(claims.ExpiresAt, 0)
	}
	st.lastPong.Store(time.Now().UnixNano())
	st.hubConn = &shared.RealtimeConn{
		ID:            connID,
		PlatformAdmin: st.platformAdmin,
		DocPrincipal:  st.docPrincipal,
		Send:          make(chan map[string]any, 64),
	}
	return st
}

// quotaKeyOf 生成连接配额键（v2 设计 §4.4）：
// Client JWT → user:{UserID}；Console admin → admin:{ActorID}。
func quotaKeyOf(p *shared.Principal) string {
	if p.ActorKind == shared.ActorKindAdmin {
		return "admin:" + string(p.ActorID)
	}
	return "user:" + p.UserID
}

// serveConn 完成握手后进入读/写双循环，任一退出即清理。
func (h *Handler) serveConn(r *http.Request, c *websocket.Conn) {
	ctx := r.Context()
	c.SetReadLimit(maxClientFrameBytes)

	hello, err := readHello(ctx, c)
	if err != nil {
		h.failHandshake(c, errCodeUnauthenticated, "missing or invalid hello frame")
		return
	}
	principal, claims, err := h.authenticate(ctx, r, hello)
	if err != nil {
		RealtimeHandshakeTotal.WithLabelValues("unauthenticated").Inc()
		h.failHandshake(c, errCodeUnauthenticated, err.Error())
		return
	}
	quotaKey := quotaKeyOf(principal)
	if !h.conns.acquire(quotaKey) {
		RealtimeHandshakeTotal.WithLabelValues("exhausted").Inc()
		h.failHandshake(c, errCodeResourceExhausted, "too many connections for this user")
		return
	}
	RealtimeHandshakeTotal.WithLabelValues("ok").Inc()

	st := newConnState(principal, hello.ProjectID, quotaKey, claims)
	RealtimeConnections.WithLabelValues(st.projectID).Inc()

	// hello_ok 出站后进入保活循环。
	if err := h.writeFrame(ctx, c, &outboundFrame{Type: "hello_ok", ConnectionID: st.id}); err != nil {
		h.cleanup(st)
		return
	}

	// JWT 到期主动关连接（StatusPolicyViolation + 原因 token_expired）。
	// 独立定时器而非依赖读超时：coder 的 ctx 读超时会直接裸关连接
	// （无 close frame），客户端只能看到 EOF；Close 则在等待握手前
	// 先把 close frame 写上线。
	if !st.expiresAt.IsZero() {
		st.expiryTimer = time.AfterFunc(time.Until(st.expiresAt), func() {
			_ = c.Close(websocket.StatusPolicyViolation, "token_expired")
		})
	}

	runCtx, cancel := context.WithCancel(ctx)
	st.cancel = cancel
	// 任一循环退出即取消 runCtx，让另一循环尽快返回（避免 wg.Wait
	// 死锁导致 cleanup 不执行、配额不释放）。
	var stopOnce sync.Once
	stop := func() { stopOnce.Do(cancel) }
	var wg sync.WaitGroup
	wg.Go(func() {
		defer stop()
		h.readLoop(runCtx, c, st)
	})
	wg.Go(func() {
		defer stop()
		h.writeLoop(runCtx, c, st)
	})
	wg.Wait()
	cancel()
	h.cleanup(st)
}

// cleanup 释放连接资源：从 Hub 摘除（扇出停止）、释放配额、更新指标。
func (h *Handler) cleanup(st *connState) {
	st.closeMu.Lock()
	if st.closed {
		st.closeMu.Unlock()
		return
	}
	st.closed = true
	st.closeMu.Unlock()

	if st.cancel != nil {
		st.cancel()
	}
	if st.expiryTimer != nil {
		st.expiryTimer.Stop()
	}
	h.hub.Remove(st.id)
	h.conns.release(st.quotaKey)
	RealtimeConnections.WithLabelValues(st.projectID).Dec()
}

// failHandshake 握手失败：发一帧 error 后以策略违规关闭。
func (h *Handler) failHandshake(c *websocket.Conn, code, message string) {
	_ = h.writeFrame(context.Background(), c, &outboundFrame{Type: "error", Code: code, Message: message})
	_ = c.Close(websocket.StatusPolicyViolation, code)
}

// readHello 读取并校验首帧 hello（10s 时限）。
func readHello(ctx context.Context, c *websocket.Conn) (*helloFrame, error) {
	ctxRead, cancel := context.WithTimeout(ctx, helloTimeout)
	defer cancel()
	var f helloFrame
	if err := readFrame(ctxRead, c, &f); err != nil {
		return nil, err
	}
	if f.Type != "hello" {
		return nil, errors.New("first frame must be hello")
	}
	return &f, nil
}

// readFrame 读取一帧文本消息并解码为 JSON（binary 帧拒绝）。
func readFrame(ctx context.Context, c *websocket.Conn, v any) error {
	typ, data, err := c.Read(ctx)
	if err != nil {
		return err
	}
	if typ != websocket.MessageText {
		return errors.New("binary frames are not supported")
	}
	return json.Unmarshal(data, v)
}

// authenticate 完成身份与项目绑定校验（v2 设计 §4.2）：
//   - 与 gRPC/HTTP 共用 Authenticate；拒 API Key、只收 console cookie 是 Grant
//   - 拒 guest / 非法 / 过期 / ttp != access
//   - admin：先 principal.ProjectID = hello.project_id（空则拒），
//     再 ValidateAdminProjectAccess；非 platform admin 无项目访问权则拒
//   - end_user：hello.project_id 必须与身份（claims pid / 会话 cookie pid）一致
func (h *Handler) authenticate(ctx context.Context, r *http.Request, hello *helloFrame) (*shared.Principal, *jwtparser.Claims, error) {
	if hello.ProjectID == "" {
		return nil, nil, errors.New("project_id is required")
	}
	// Grant：Realtime 禁止 API key（不是第三份解析器）。
	if len(r.Header.Values("X-Api-Key")) > 0 {
		return nil, nil, errors.New("api key credentials are not allowed")
	}
	if raw := r.Header.Get("Authorization"); raw != "" {
		if ct, _, ok := interceptor.ParseAuthorizationHeader(raw); ok && ct == shared.CredentialTypeAPIKey {
			return nil, nil, errors.New("api key credentials are not allowed")
		}
	}

	req := shared.AuthnRequest{
		Authorization: r.Header.Values("Authorization"),
		CookieHeaders: realtimeSessionCookieHeaders(r),
		AccessToken:   hello.AccessToken,
	}
	principal, err := h.validator.Authenticate(ctx, req)
	if err != nil || principal == nil || !principal.IsAuthenticated() {
		if _, _, parseErr := shared.ParseAuthnRequest(req); parseErr != nil {
			if errors.Is(parseErr, shared.ErrMissingCredential) {
				return nil, nil, errors.New("authentication required")
			}
			return nil, nil, parseErr
		}
		return nil, nil, errors.New("invalid or expired credential")
	}
	if principal.ActorKind == shared.ActorKindService || principal.IsSystem() {
		return nil, nil, errors.New("api key credentials are not allowed")
	}

	// ttp / exp：读取 JWT claims 供握手后的到期断开用（SDK access_token
	// 与 console cookie 值都是 JWT）。
	ct, cred, parseErr := shared.ParseAuthnRequest(req)
	var claims *jwtparser.Claims
	if parseErr == nil && (ct == shared.CredentialTypeToken || ct == shared.CredentialTypeSession) {
		claims, _ = h.validator.ParseClaims(cred)
	}
	if claims != nil && claims.TokenType != "" && claims.TokenType != jwtparser.TokenTypeAccess {
		return nil, nil, errors.New("access token required")
	}

	switch principal.ActorKind {
	case shared.ActorKindAdmin:
		// 锁定顺序：先绑 ProjectID 再 ValidateAdminProjectAccess
		// （空 ProjectID 时 ValidateAdminProjectAccess 直接成功）。
		principal.ProjectID = hello.ProjectID
		if err := h.validator.ValidateAdminProjectAccess(ctx, principal); err != nil {
			return nil, nil, errors.New("admin has no access to this project")
		}
	case shared.ActorKindEndUser:
		if principal.ProjectID != hello.ProjectID {
			return nil, nil, errors.New("project_id does not match credential")
		}
	default:
		return nil, nil, errors.New("credential type not allowed")
	}
	return principal, claims, nil
}

// realtimeSessionCookieHeaders 收集 WS 握手可用的 cookie：
// - TORCHWOOD_session_console（Console admin 会话）
// - TORCHWOOD_session_<project>（B10 端用户 cookie，与 JWT 二选一，仍拒 API key）
func realtimeSessionCookieHeaders(r *http.Request) []string {
	var out []string
	for _, c := range r.Cookies() {
		if c.Value == "" {
			continue
		}
		if c.Name == shared.ConsoleSessionCookieName {
			out = append(out, c.Name+"="+c.Value)
			continue
		}
		if strings.HasPrefix(c.Name, shared.SessionCookiePrefix) {
			out = append(out, c.Name+"="+c.Value)
		}
	}
	return out
}

// consoleSessionCookieHeaders 保留别名兼容旧测试桩（若有外部调用），内部已由 realtimeSessionCookieHeaders 接管。
func consoleSessionCookieHeaders(r *http.Request) []string {
	return realtimeSessionCookieHeaders(r)
}

// readLoop 逐帧读取客户端控制帧；60s 无帧时按 pong 滑窗判定，
// 2 个 ping 间隔无 pong 则断开；JWT 到期由握手后的定时器断开
// （token_expired），循环顶部的 expired 检查只是兜底。
func (h *Handler) readLoop(ctx context.Context, c *websocket.Conn, st *connState) {
	for {
		if st.expired() {
			h.closeWithReason(c, "token_expired")
			return
		}
		ctxRead, cancel := context.WithTimeout(ctx, frameReadTimeout)
		var f inboundFrame
		err := readFrame(ctxRead, c, &f)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if errors.Is(err, context.DeadlineExceeded) {
				if pongStale(st) {
					h.closeWithReason(c, "pong_timeout")
					return
				}
				continue
			}
			return // 客户端断开或协议错误
		}
		switch f.Type {
		case "ping":
			if err := h.writeFrame(ctx, c, &outboundFrame{Type: "pong"}); err != nil {
				return
			}
		case "pong":
			st.lastPong.Store(time.Now().UnixNano())
		case "subscribe":
			h.handleSubscribe(ctx, c, st, &f)
		case "unsubscribe":
			h.handleUnsubscribe(st, &f)
		default:
			_ = h.writeFrame(ctx, c, &outboundFrame{
				ID: f.ID, Type: "error", Code: errCodeInvalidArgument,
				Message: "unknown frame type",
			})
		}
	}
}

// writeLoop 消费 Hub 发送 chan 与 30s ping ticker。
func (h *Handler) writeLoop(ctx context.Context, c *websocket.Conn, st *connState) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case frame := <-st.hubConn.Send:
			data, err := json.Marshal(frame)
			if err != nil {
				return
			}
			if err := h.writeBytes(ctx, c, data); err != nil {
				return
			}
		case <-ticker.C:
			if st.expired() {
				h.closeWithReason(c, "token_expired")
				return
			}
			if err := h.writeFrame(ctx, c, &outboundFrame{Type: "ping"}); err != nil {
				return
			}
		}
	}
}

// handleSubscribe 校验并登记订阅（v2 设计 §4.3 / §4.4）：
// 集合频道：GetCollection 存在、!IsSystem、!Disabled（一律 NOT_FOUND）；
// 文档频道：GetDocument 存在且当前 principal 可 read（一律 NOT_FOUND）。
// 超 32 订返回 RESOURCE_EXHAUSTED，连接保持。
func (h *Handler) handleSubscribe(ctx context.Context, c *websocket.Conn, st *connState, f *inboundFrame) {
	if len(st.subs) >= MaxSubscriptionsPerConn {
		_ = h.writeFrame(ctx, c, &outboundFrame{
			ID: f.ID, Type: "error", Code: errCodeResourceExhausted,
			Message: "subscription limit reached",
		})
		return
	}
	parsed, ok := parseChannel(f.Channel)
	if !ok {
		_ = h.writeFrame(ctx, c, &outboundFrame{
			ID: f.ID, Type: "error", Code: errCodeInvalidArgument,
			Message: "invalid channel",
		})
		return
	}
	if !h.channelAllowed(ctx, st, parsed) {
		_ = h.writeFrame(ctx, c, &outboundFrame{
			ID: f.ID, Type: "error", Code: errCodeNotFound,
			Message: "channel not found",
		})
		return
	}
	if _, dup := st.subs[f.Channel]; !dup {
		st.subs[f.Channel] = struct{}{}
		h.hub.Subscribe(f.Channel, st.hubConn)
	}
	_ = h.writeFrame(ctx, c, &outboundFrame{
		ID: f.ID, Type: "subscribed", Channel: f.Channel,
	})
}

// channelAllowed 判定频道可订。databases：集合/文档存在且可读；
// accounts：本人（JWT sub == userId）或 platform admin（D17）；出站无 acl。
func (h *Handler) channelAllowed(ctx context.Context, st *connState, ch parsedChannel) bool {
	switch ch.kind {
	case channelKindAccounts:
		if st.platformAdmin {
			return true
		}
		return st.principal != nil && st.principal.UserID != "" && st.principal.UserID == ch.userID
	case channelKindDatabases:
		return h.databasesChannelAllowed(ctx, st, ch.dbID, ch.collID, ch.docID)
	default:
		return false
	}
}

// databasesChannelAllowed 判定集合或文档频道可订（存在且可读）。
func (h *Handler) databasesChannelAllowed(ctx context.Context, st *connState, dbID, collID, docID string) bool {
	if docID != "" {
		doc, err := h.docDB.GetDocument(ctx, st.projectID, dbID, collID, docID, st.docPrincipal)
		if err != nil {
			if isPermissionOrNotFound(err) {
				return false
			}
			h.logger.Error("realtime subscribe document check failed",
				"connection_id", st.id, "project_id", st.projectID,
				"database_id", dbID, "collection_id", collID, "document_id", docID, "error", err)
			return false
		}
		return doc != nil
	}
	coll, err := h.docDB.GetCollection(ctx, st.projectID, dbID, collID)
	if err != nil {
		h.logger.Error("realtime subscribe collection check failed",
			"connection_id", st.id, "project_id", st.projectID,
			"database_id", dbID, "collection_id", collID, "error", err)
		return false
	}
	return coll != nil && !coll.IsSystem && !coll.Disabled
}

// handleUnsubscribe 移除订阅（未订阅则忽略）。
func (h *Handler) handleUnsubscribe(st *connState, f *inboundFrame) {
	if _, ok := st.subs[f.Channel]; !ok {
		return
	}
	delete(st.subs, f.Channel)
	h.hub.Unsubscribe(f.Channel, st.id)
}

// expired 报告 JWT 是否已到期（expiresAt 零值恒 false；正常不会发生，
// 见 connState.expiresAt 注释）。
func (st *connState) expired() bool {
	return !st.expiresAt.IsZero() && !time.Now().Before(st.expiresAt)
}

// pongStale 报告超过 2 个 ping 间隔未收到 pong。
func pongStale(st *connState) bool {
	return time.Since(time.Unix(0, st.lastPong.Load())) > pongWindow
}

// closeWithReason 以策略违规 + 原因关闭（best-effort）。
func (h *Handler) closeWithReason(c *websocket.Conn, reason string) {
	_ = c.Close(websocket.StatusPolicyViolation, reason)
}

// writeFrame 序列化并写入一帧（带写 deadline）。
func (h *Handler) writeFrame(ctx context.Context, c *websocket.Conn, f *outboundFrame) error {
	data, err := json.Marshal(f)
	if err != nil {
		return err
	}
	return h.writeBytes(ctx, c, data)
}

// writeBytes 写入一帧（带写 deadline：写完即释放，不残留 conn deadline）。
func (h *Handler) writeBytes(ctx context.Context, c *websocket.Conn, payload []byte) error {
	ctxWrite, cancel := context.WithTimeout(ctx, frameWriteTimeout)
	defer cancel()
	return c.Write(ctxWrite, websocket.MessageText, payload)
}

// isPermissionOrNotFound 识别订阅校验中「无权限/不存在」类错误
// （GetDocument 失败统一 NOT_FOUND，防枚举）。
func isPermissionOrNotFound(err error) bool {
	return errors.Is(err, databases.ErrPermissionDenied) ||
		errors.Is(err, databases.ErrDocumentNotFound) ||
		strings.Contains(err.Error(), "not found")
}

// connectionRegistry 是每用户连接数配额表（4 连 / 用户）。
type connectionRegistry struct {
	mu     sync.Mutex
	counts map[string]int
}

func newConnectionRegistry() *connectionRegistry {
	return &connectionRegistry{counts: make(map[string]int)}
}

func (r *connectionRegistry) acquire(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.counts[key] >= MaxConnectionsPerUser {
		return false
	}
	r.counts[key]++
	return true
}

func (r *connectionRegistry) release(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.counts[key] <= 1 {
		delete(r.counts, key)
		return
	}
	r.counts[key]--
}
