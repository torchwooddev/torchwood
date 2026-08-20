package groups

import (
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	StatusPending  = "pending"
	StatusAccepted = "accepted"
	StatusRejected = "rejected"

	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"
)

func ValidateStatus(s string) error {
	switch s {
	case StatusPending, StatusAccepted, StatusRejected:
		return nil
	default:
		return status.Error(codes.InvalidArgument,
			fmt.Sprintf("invalid membership status %q (allowed: pending, accepted, rejected)", s))
	}
}

func ValidateRole(role string) error {
	switch role {
	case RoleOwner, RoleAdmin, RoleMember:
		return nil
	default:
		return status.Error(codes.InvalidArgument,
			fmt.Sprintf("invalid membership role %q (allowed: owner, admin, member)", role))
	}
}

func PrimaryRole(roles []string) string {
	if len(roles) == 0 {
		return RoleMember
	}
	return roles[0]
}
