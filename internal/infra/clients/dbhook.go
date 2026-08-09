package clients

import (
	"context"
	"log/slog"
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
			slog.String("query", e.Query),
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
			slog.String("query", e.Query),
			slog.Duration("duration", d),
			slog.String("error", errorString(e.Err)),
		)
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

var _ bun.QueryHook = (*SlowQueryHook)(nil)
