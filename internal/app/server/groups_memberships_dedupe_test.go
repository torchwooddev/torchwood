package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/groups"
	domainusers "github.com/torchwooddev/torchwood/internal/domain/users"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestGroups_CreateMembership_Idempotent(t *testing.T) {
	g := newMemGroupRepo()
	g.seed(&groups.Group{ID: "group-1", Name: "T", Total: 1})
	usersMem := newMemUserRepo()
	usersMem.seed(&domainusers.User{ID: "u-1", Email: "a@b.c"})
	mems := newMemMembershipRepo()
	mems.groups = g
	mems.seed(&groups.Membership{ID: "m-1", GroupID: "group-1", UserID: "u-1", Email: "a@b.c", Status: groups.StatusAccepted})
	mems.seed(&groups.Membership{ID: "m-2", GroupID: "group-1", Email: "p@x.com", Status: groups.StatusPending})
	uc := NewGroups(fakeProjectRepo{}, usersMem, g, mems)
	principal := databases.Principal{Roles: []string{"admin"}}

	t.Run("accepted duplicate by user_id", func(t *testing.T) {
		before := mems.count()
		_, err := uc.CreateMembership(context.Background(), "proj-1", CreateMembershipCommand{
			GroupID: "group-1", UserID: "u-1", Roles: []string{groups.RoleMember}, Status: groups.StatusAccepted,
		}, principal)
		require.Equal(t, codes.AlreadyExists, status.Code(err), "同 user 重复 accepted 必须 AlreadyExists")
		require.Equal(t, before, mems.count())
	})

	t.Run("pending duplicate by email", func(t *testing.T) {
		before := mems.count()
		_, err := uc.CreateMembership(context.Background(), "proj-1", CreateMembershipCommand{
			GroupID: "group-1", Email: "p@x.com", Roles: []string{groups.RoleMember}, Status: groups.StatusPending,
		}, principal)
		require.Equal(t, codes.AlreadyExists, status.Code(err))
		require.Equal(t, before, mems.count())
	})
}
