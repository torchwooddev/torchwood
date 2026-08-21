package users

import (
	"context"
	"encoding/json"
)

// ListFilter 编译现有 ListRequest.queries（列白名单）。
type ListFilter struct {
	Queries   []string
	PageSize  int32
	PageToken string
}

// ListResult 是 User 列表页。
type ListResult struct {
	Users         []*User
	TotalCount    int64
	NextPageToken string
}

// Repository 是 User 聚合的持久化端口。
type Repository interface {
	GetByEmail(ctx context.Context, projectID, email string) (*User, error)
	GetByID(ctx context.Context, projectID, id string) (*User, error)
	GetByPhone(ctx context.Context, projectID, phone string) (*User, error)
	Insert(ctx context.Context, projectID string, user *User) error
	// Update 只 SET cols 中的白名单列，禁止整行覆盖。
	Update(ctx context.Context, projectID, id string, cols map[string]any) error
	Delete(ctx context.Context, projectID, id string) error
	List(ctx context.Context, projectID string, f ListFilter) (*ListResult, error)
	// UpdateFactors 读-改-写 factors：同一 Tx 内 SELECT … FOR UPDATE 后再 SET；文档适配器无行锁。
	UpdateFactors(ctx context.Context, projectID, id string, mutate func(current json.RawMessage) (json.RawMessage, error)) error
}
