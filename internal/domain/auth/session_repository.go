package auth

import (
	"context"
	"encoding/json"
	"time"
)

// Session 是项目内终端用户会话（静态表 sessions / staging sys_sessions）。
type Session struct {
	ID         string
	UserID     string
	SecretHash string
	Provider   string
	UserAgent  string
	IP         string
	Country    string
	Factors    json.RawMessage
	ExpireAt   time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// SessionRepository 把会话持久化到项目 schema。E5-4 前生产仍走 DocumentDB。
type SessionRepository interface {
	Insert(ctx context.Context, projectID string, s *Session) error
	GetByID(ctx context.Context, projectID, id string) (*Session, error)
	ListByUser(ctx context.Context, projectID, userID string) ([]Session, error)
	Delete(ctx context.Context, projectID, id string) error
	DeleteByUser(ctx context.Context, projectID, userID string) error
	// DeleteOldestByUser 按 expire_at ASC 删最旧，使剩余条数 = keep（K-22）。
	DeleteOldestByUser(ctx context.Context, projectID, userID string, keep int) error
}
