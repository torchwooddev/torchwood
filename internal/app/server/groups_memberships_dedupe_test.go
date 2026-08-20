package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/groups"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Round3 H5-4：CreateMembership 幂等——同一 group 下同 user / 同（规范化）
// email 的第二次创建（含 pending 重复邀请、accepted 重复）必须 AlreadyExists，
// 且不重复写行、不再 +1 total。
func TestGroups_CreateMembership_Idempotent(t *testing.T) {
	docDB := newFakeDocDB()
	docDB.seed("groups",
		databases.Document{ID: "group-1", Data: map[string]any{"name": "T", "total": int64(1)}},
	)
	docDB.seed("users",
		databases.Document{ID: "u-1", Data: map[string]any{"email": "a@b.c"}},
	)
	docDB.seed("memberships",
		databases.Document{ID: "m-1", Data: map[string]any{"group_id": "group-1", "user_id": "u-1", "email": "a@b.c", "status": groups.StatusAccepted}},
		databases.Document{ID: "m-2", Data: map[string]any{"group_id": "group-1", "user_id": "", "email": "p@x.com", "status": groups.StatusPending}},
	)
	uc := NewGroups(fakeProjectRepo{}, docDB)
	principal := databases.Principal{Roles: []string{"admin"}}

	groupTotal := func() int64 {
		doc, err := docDB.GetDocument(context.Background(), "proj-1", "default", "groups", "group-1", databases.SystemPrincipal)
		require.NoError(t, err)
		require.NotNil(t, doc)
		return doc.Data["total"].(int64)
	}
	membershipCount := func() int {
		list, err := docDB.ListDocuments(context.Background(), "proj-1", "default", "memberships", databases.Query{}, databases.SystemPrincipal)
		require.NoError(t, err)
		return len(list.Documents)
	}

	t.Run("accepted duplicate by user_id", func(t *testing.T) {
		before := membershipCount()
		_, err := uc.CreateMembership(context.Background(), "proj-1", CreateMembershipCommand{
			GroupID: "group-1", UserID: "u-1", Roles: []string{groups.RoleMember}, Status: groups.StatusAccepted,
		}, principal)
		require.Equal(t, codes.AlreadyExists, status.Code(err), "同 user 重复 accepted 必须 AlreadyExists")
		require.Equal(t, before, membershipCount(), "不得重复写 membership 行")
		require.Equal(t, int64(1), groupTotal(), "total 不得 +1")
	})

	t.Run("duplicate by normalized email resolving same user", func(t *testing.T) {
		before := membershipCount()
		_, err := uc.CreateMembership(context.Background(), "proj-1", CreateMembershipCommand{
			GroupID: "group-1", Email: "A@B.C", Roles: []string{groups.RoleMember}, Status: groups.StatusPending,
		}, principal)
		require.Equal(t, codes.AlreadyExists, status.Code(err), "大小写不同但解析为同 user 必须 AlreadyExists")
		require.Equal(t, before, membershipCount())
		require.Equal(t, int64(1), groupTotal())
	})

	t.Run("pending duplicate by email", func(t *testing.T) {
		before := membershipCount()
		_, err := uc.CreateMembership(context.Background(), "proj-1", CreateMembershipCommand{
			GroupID: "group-1", Email: "P@X.com", Roles: []string{groups.RoleMember}, Status: groups.StatusPending,
		}, principal)
		require.Equal(t, codes.AlreadyExists, status.Code(err), "pending 重复邀请必须 AlreadyExists")
		require.Equal(t, before, membershipCount())
		require.Equal(t, int64(1), groupTotal())
	})

	t.Run("fresh accepted invite succeeds and increments total", func(t *testing.T) {
		before := membershipCount()
		mem, err := uc.CreateMembership(context.Background(), "proj-1", CreateMembershipCommand{
			GroupID: "group-1", UserID: "u-2", Email: "new@x.com", Roles: []string{groups.RoleMember}, Status: groups.StatusAccepted,
		}, principal)
		require.NoError(t, err)
		require.Equal(t, "new@x.com", mem.Data["email"], "email 落库为规范化小写")
		require.Equal(t, before+1, membershipCount())
		require.Equal(t, int64(2), groupTotal(), "accepted 成功后 total 才 +1")
	})
}
