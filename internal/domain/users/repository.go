package users

import "context"

// Repository 是 User 聚合的持久化端口。适配器可仍落在系统集合 users（sentinel `_`）。
type Repository interface {
	GetByEmail(ctx context.Context, projectID, email string) (*User, error)
	GetByID(ctx context.Context, projectID, id string) (*User, error)
	GetByPhone(ctx context.Context, projectID, phone string) (*User, error)
	Insert(ctx context.Context, projectID string, user *User) error
}
