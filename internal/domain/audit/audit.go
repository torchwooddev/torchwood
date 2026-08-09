package audit

import (
	"context"
	"time"
)

type Entry struct {
	ID         string
	ProjectID  string
	ActorID    string
	ActorKind  string
	Action     string
	ResourceID string
	Status     string
	IP         string
	UserAgent  string
	Metadata   map[string]any
	CreatedAt  time.Time
}

type Repository interface {
	Insert(ctx context.Context, entry *Entry) error
	// ListByActor 返回某项目下指定 actor 的日志（created_at DESC，limit ≤ 100）。
	ListByActor(ctx context.Context, projectID, actorID string, limit int) ([]Entry, error)
}
