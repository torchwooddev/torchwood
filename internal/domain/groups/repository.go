package groups

import (
	"context"
	"errors"
	"time"
)

var (
	ErrGroupIDRequired         = errors.New("group id is required")
	ErrMembershipIDRequired    = errors.New("membership id is required")
	ErrMembershipNotPending    = errors.New("membership status is not pending")
	ErrMembershipAlreadyExists = errors.New("membership already exists")
	ErrInvalidUpdate           = errors.New("invalid group update")
)

// GroupRepository 把用户组持久化到项目 schema。
type GroupRepository interface {
	Insert(ctx context.Context, projectID string, group *Group) error
	GetByID(ctx context.Context, projectID, id string) (*Group, error)
	// Update 只 SET 白名单列（name/permissions/prefs），禁止写 total。
	Update(ctx context.Context, projectID, id string, cols map[string]any) error
	Delete(ctx context.Context, projectID, id string) error
	List(ctx context.Context, projectID string) ([]*Group, error)
	// AddTotal 用 SQL GREATEST(total+delta, 0)，禁止读-改-写。
	AddTotal(ctx context.Context, projectID, groupID string, delta int64) error
	// RecountAccepted 把 total 重数为 accepted 成员数（DeleteUser 后）。
	RecountAccepted(ctx context.Context, projectID, groupID string) error
}

// MembershipRepository 把组成员持久化到项目 schema。
type MembershipRepository interface {
	Insert(ctx context.Context, projectID string, m *Membership) error
	GetByID(ctx context.Context, projectID, id string) (*Membership, error)
	ListByGroup(ctx context.Context, projectID, groupID string) ([]*Membership, error)
	ListByUser(ctx context.Context, projectID, userID string) ([]*Membership, error)
	Delete(ctx context.Context, projectID, id string) error
	// Accept 在同一 Tx 内 CAS pending→accepted 再 AddTotal(+1)。
	Accept(ctx context.Context, projectID, id, userID string, joinedAt time.Time) error
	// UpdateRoles 只 SET roles；对行 FOR UPDATE。last-owner 检查留在 use-case。
	UpdateRoles(ctx context.Context, projectID, id string, roles []string) error
}
