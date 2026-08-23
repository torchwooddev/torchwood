package model

import (
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

// DocumentEventsOutboxDead 是死信表行（W-J）。
type DocumentEventsOutboxDead struct {
	bun.BaseModel `bun:"table:document_events_outbox_dead,alias:deod"`

	EventID   string          `bun:"event_id,pk"`
	ProjectID string          `bun:"project_id,notnull"`
	Topic     string          `bun:"topic,notnull"`
	Channel   *string         `bun:"channel"`
	Payload   json.RawMessage `bun:"payload,type:jsonb,notnull"`
	Attempts  int             `bun:"attempts,notnull"`
	LastError string          `bun:"last_error"`
	FailedAt  time.Time       `bun:"failed_at,notnull"`
	CreatedAt time.Time       `bun:"created_at,notnull"`
}
