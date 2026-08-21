package groups

import (
	"fmt"
	"time"
)

const (
	StatusPending  = "pending"
	StatusAccepted = "accepted"
	StatusRejected = "rejected"

	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"
)

// Membership 是用户与组的关系。UserID 为空表示待接受的邮箱邀请（库内存 NULL）。
type Membership struct {
	ID        string
	GroupID   string
	UserID    string
	Email     string
	Name      string
	Roles     []string
	Status    string
	InvitedAt time.Time
	JoinedAt  time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

func ValidateStatus(s string) error {
	switch s {
	case StatusPending, StatusAccepted, StatusRejected:
		return nil
	default:
		return fmt.Errorf("invalid membership status %q (allowed: pending, accepted, rejected)", s)
	}
}

func ValidateRole(role string) error {
	switch role {
	case RoleOwner, RoleAdmin, RoleMember:
		return nil
	default:
		return fmt.Errorf("invalid membership role %q (allowed: owner, admin, member)", role)
	}
}

func PrimaryRole(roles []string) string {
	if len(roles) == 0 {
		return RoleMember
	}
	return roles[0]
}
