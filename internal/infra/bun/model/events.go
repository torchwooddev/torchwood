package model

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

// DocumentEventsOutbox 是用户集合文档写事件的 transactional outbox 行
// （public 元数据库，与文档写同 COMMIT；payload 为完整事件信封 JSON，含 acl）。
type DocumentEventsOutbox struct {
	bun.BaseModel `bun:"table:document_events_outbox,alias:deo"`

	EventID      string          `bun:"event_id,pk"`
	ProjectID    string          `bun:"project_id,notnull"`
	Topic        string          `bun:"topic,notnull"`
	Payload      json.RawMessage `bun:"payload,type:jsonb,notnull"`
	CreatedAt    time.Time       `bun:"created_at,notnull"`
	AvailableAt  time.Time       `bun:"available_at,notnull"`
	Attempts     int             `bun:"attempts,notnull,default:0"`
	DispatchedAt *time.Time      `bun:"dispatched_at"`
	PublishedAt  *time.Time      `bun:"published_at"`
}
