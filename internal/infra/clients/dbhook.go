package clients

import (
	"context"
	"log/slog"
	"regexp"
	"time"

	"github.com/uptrace/bun"
)

// DefaultSlowQueryThreshold 是 slow_query_threshold 为空字符串时的默认阈值。
const DefaultSlowQueryThreshold = 500 * time.Millisecond

// SlowQueryHook 记录超过阈值的 SQL；LogAll 时记录全部 SQL（debug 模式）。
// 实现 bun.QueryHook。
type SlowQueryHook struct {
	Threshold time.Duration
	LogAll    bool
	Logger    *slog.Logger
}

// NewSlowQueryHook 按配置构建 hook：
//   - debug=true → LogAll=true（全量 SQL Debug 日志）；
//   - 阈值 "" → 默认 500ms；"0" → 禁用（返回 nil）；解析失败 → Warn 并禁用。
func NewSlowQueryHook(threshold string, debug bool, logger *slog.Logger) *SlowQueryHook {
	if logger == nil {
		logger = slog.Default()
	}
	if debug {
		return &SlowQueryHook{Logger: logger, LogAll: true}
	}
	d := DefaultSlowQueryThreshold
	switch threshold {
	case "":
		// 默认 500ms。
	case "0":
		return nil
	default:
		parsed, err := time.ParseDuration(threshold)
		if err != nil {
			logger.Warn("invalid slow_query_threshold, slow query logging disabled",
				slog.String("value", threshold), slog.String("error", err.Error()))
			return nil
		}
		d = parsed
	}
	return &SlowQueryHook{Threshold: d, Logger: logger}
}

func (h *SlowQueryHook) BeforeQuery(ctx context.Context, _ *bun.QueryEvent) context.Context {
	return ctx
}

func (h *SlowQueryHook) AfterQuery(_ context.Context, e *bun.QueryEvent) {
	if h.LogAll {
		h.Logger.Debug("sql",
			slog.String("operation", e.Operation()),
			slog.String("table", tableFromQuery(e)),
			slog.String("query", redactQuery(e)),
			slog.Duration("duration", time.Since(e.StartTime)),
			slog.String("error", errorString(e.Err)),
		)
		return
	}
	if h.Threshold <= 0 {
		return
	}
	if d := time.Since(e.StartTime); d >= h.Threshold {
		h.Logger.Warn("slow query",
			slog.String("operation", e.Operation()),
			slog.String("table", tableFromQuery(e)),
			slog.String("query", redactQuery(e)),
			slog.Duration("duration", d),
			slog.String("error", errorString(e.Err)),
		)
	}
}

// sensitiveColumnPattern 匹配敏感列的赋值/比较片段，命中即整体替换为
// '[REDACTED]'，兜底防御 QueryTemplate 缺失时回退内联 SQL 的泄漏。
var sensitiveColumnPattern = regexp.MustCompile(`(?i)\b(password_hash|password|secret|secret_hash|token|access_token|refresh_token|auth_token|otp|otp_code|api_key)\s*(=|<>|IN|LIKE|ILIKE)\s*(?:'[^']*'|"[^"]*"|\([^)]*\)|\S+)`)

// tablePattern 提取主操作表名（FROM/INTO/UPDATE/JOIN 后首个标识符）。
var tablePattern = regexp.MustCompile(`(?i)\b(?:FROM|INTO|UPDATE|JOIN)\s+"?([a-z_][a-z0-9_]*)`)

// redactQuery 返回日志安全的 SQL：优先使用占位符模板（不含内联参数）；
// 兜底使用内联 SQL 时先对敏感列值做强制掩码。
func redactQuery(e *bun.QueryEvent) string {
	q := e.QueryTemplate
	if q == "" {
		q = e.Query
	}
	return sensitiveColumnPattern.ReplaceAllString(q, "$1 $2 '[REDACTED]'")
}

// tableFromQuery 从查询中提取操作目标表名；提取失败返回空串。
func tableFromQuery(e *bun.QueryEvent) string {
	q := e.QueryTemplate
	if q == "" {
		q = e.Query
	}
	if m := tablePattern.FindStringSubmatch(q); len(m) > 1 {
		return m[1]
	}
	return ""
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

var _ bun.QueryHook = (*SlowQueryHook)(nil)
