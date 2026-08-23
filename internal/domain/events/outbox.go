package events

import (
	"context"
	"time"
)

// DeadLetter 是 document_events_outbox_dead 的领域视图（W-J）。
type DeadLetter struct {
	EventID   string
	ProjectID string
	Topic     string
	Channel   string
	Payload   []byte
	Attempts  int32
	LastError string
	CreatedAt time.Time
}

// OutboxRepository 扩展原有 outbox 能力，增加死信查询与重放（W-J）。
type OutboxRepository interface {
	ListDeadLetters(ctx context.Context, projectID string, pageSize int32, pageToken string) ([]DeadLetter, int64, string, error)
	ReplayDeadLetter(ctx context.Context, eventID, projectID string) error
}
