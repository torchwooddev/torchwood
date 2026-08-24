package interceptor

import (
	"context"
	"strings"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	domainauth "github.com/torchwooddev/torchwood/internal/domain/auth"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
)

// 通用 API 限流默认值（roadmap §3.4）：固定窗口，60s 量级。
const (
	defaultRateLimitWindow = 60 * time.Second
	defaultIPRateLimit     = 300  // per-IP：每分钟几百
	defaultUserRateLimit   = 1000 // per-user：每分钟千级
	defaultAPIKeyRateLimit = 6000 // per-API-key：每分钟数千
)

// rateLimitExemptPrefixes 是限流豁免的 gRPC 框架内置服务（健康检查与
// reflection），避免探活/服务发现被业务限流打挂；与 server 侧
// authzExemptServicePrefixes 维持同一白名单。
var rateLimitExemptPrefixes = []string{
	"/grpc.health.v1.",
	"/grpc.reflection.",
}

// RateLimitInterceptor 按 API Key > user > IP 三个维度对经过 server gRPC
// 管道的所有业务 RPC 做通用限流（roadmap §3.4）：带 API Key principal 的
// 请求按 API Key 维度计数，带 user/session principal 的请求按 user 维度
// 计数，未认证请求按客户端 IP 计数；同一请求只按命中的一个维度计数。
// 与注册/匿名会话/MFA/OTP 发送 4 处局部限流的键空间不同，互不冲突、叠加生效。
type RateLimitInterceptor struct {
	limiter domainauth.RateLimiter
	enabled bool
	ip      rateLimitDimension
	user    rateLimitDimension
	apiKey  rateLimitDimension
}

// rateLimitDimension 是单一维度的限流参数：key 前缀 + 窗口阈值。
type rateLimitDimension struct {
	keyPrefix string
	limit     int
	window    time.Duration
}

func (d rateLimitDimension) key(id string) string {
	return d.keyPrefix + id
}

// NewRateLimitInterceptor 从配置构造通用限流拦截器。security.rate_limit
// 未配置或部分配置时启用内置默认值（总开关默认 true）；limiter 复用
// domainauth.RateLimiter 端口（Redis 固定窗口实现）。
func NewRateLimitInterceptor(limiter domainauth.RateLimiter, cfg *config.AppConfig) *RateLimitInterceptor {
	rl := cfg.GetSecurity().GetRateLimit()
	// proto3 optional：仅显式配置为 false 才关闭，未设置视为开启（默认 true）。
	enabled := rl == nil || rl.Enabled == nil || rl.GetEnabled()
	return &RateLimitInterceptor{
		limiter: limiter,
		enabled: enabled,
		ip:      resolveRateLimitDimension("api:ip:", rl.GetIp(), defaultIPRateLimit),
		user:    resolveRateLimitDimension("api:user:", rl.GetUser(), defaultUserRateLimit),
		apiKey:  resolveRateLimitDimension("api:apikey:", rl.GetApiKey(), defaultAPIKeyRateLimit),
	}
}

// resolveRateLimitDimension 应用单维度配置，未配置/非法值回落默认值。
func resolveRateLimitDimension(keyPrefix string, d *config.Security_RateLimit_Dimension, defaultLimit int) rateLimitDimension {
	dim := rateLimitDimension{keyPrefix: keyPrefix, limit: defaultLimit, window: defaultRateLimitWindow}
	if d == nil {
		return dim
	}
	if d.GetLimit() > 0 {
		dim.limit = int(d.GetLimit())
	}
	if w := parseRateLimitWindow(d.GetWindow()); w > 0 {
		dim.window = w
	}
	return dim
}

// parseRateLimitWindow 解析窗口时长字符串（如 "60s"）；空/非法/非正返回 0。
func parseRateLimitWindow(s string) time.Duration {
	if s == "" {
		return 0
	}
	w, err := time.ParseDuration(s)
	if err != nil || w <= 0 {
		return 0
	}
	return w
}

func (r *RateLimitInterceptor) UnaryRateLimitMiddleware(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	if !r.enabled || rateLimitExempt(info.FullMethod) {
		return handler(ctx, req)
	}
	dim, key := r.dimension(ctx)
	if dim == nil || key == "" {
		// 没有可用维度（如未认证且解析不到 IP）时不计数，放行给后续链路。
		return handler(ctx, req)
	}
	// 超限返回 ResourceExhausted（grpc-gateway 映射 HTTP 429），Redis 故障
	// 由端口实现返回 Internal（fail-closed），均原样透传。ResourceExhausted
	// 统一携带 google.rpc.RetryInfo detail：端口实现已附精确剩余窗口时尊重
	// 原值，否则以本维度整窗口作为保守估计（Round4 J3-6）。
	if err := r.limiter.Allow(ctx, key, dim.limit, dim.window); err != nil {
		return nil, withRetryInfoFallback(err, dim.window)
	}
	return handler(ctx, req)
}

// withRetryInfoFallback 为无 RetryInfo detail 的 ResourceExhausted 错误补上
// 以 window 为建议退避的 detail；其他 code 或已有 detail 时原样返回。
func withRetryInfoFallback(err error, window time.Duration) error {
	if status.Code(err) != codes.ResourceExhausted || window <= 0 {
		return err
	}
	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	for _, d := range st.Details() {
		if _, has := d.(*errdetails.RetryInfo); has {
			return err
		}
	}
	enriched, derr := st.WithDetails(&errdetails.RetryInfo{
		RetryDelay: durationpb.New(window),
	})
	if derr != nil {
		return err
	}
	return enriched.Err()
}

// dimension 按优先级选择限流维度：API Key principal > user/session
// principal > 客户端 IP。同一请求只命中一个维度（不做叠加计数）。
func (r *RateLimitInterceptor) dimension(ctx context.Context) (*rateLimitDimension, string) {
	if p, ok := contexts.Principal(ctx); ok && p != nil {
		if p.APIKeyID != "" {
			if id := string(p.ActorID); id != "" {
				return &r.apiKey, r.apiKey.key(id)
			}
			return &r.apiKey, r.apiKey.key(p.APIKeyID)
		}
		if id := string(p.ActorID); id != "" {
			return &r.user, r.user.key(id)
		}
		if p.UserID != "" {
			return &r.user, r.user.key(p.UserID)
		}
	}
	// 未认证（或 public 方法无 principal）：按客户端 IP 计数。IP 来自
	// ClientInfoInterceptor，已经过 trusted-proxy 校验。
	if ci := contexts.ClientInfoFrom(ctx); ci.IP != "" {
		return &r.ip, r.ip.key(ci.IP)
	}
	return nil, ""
}

func rateLimitExempt(fullMethod string) bool {
	for _, prefix := range rateLimitExemptPrefixes {
		if strings.HasPrefix(fullMethod, prefix) {
			return true
		}
	}
	return false
}
