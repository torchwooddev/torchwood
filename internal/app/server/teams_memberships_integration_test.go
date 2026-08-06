package server

import (
	"context"
	"testing"

	"github.com/torchwoodio/torchwood/internal/domain/databases"
	"github.com/torchwoodio/torchwood/internal/domain/shared"
	"github.com/torchwoodio/torchwood/internal/domain/teams"
	"github.com/torchwoodio/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwoodio/torchwood/internal/infra/documentdb"
	"github.com/torchwoodio/torchwood/internal/pkg/contexts"
	"github.com/torchwoodio/torchwood/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestTeams_Memberships(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, internalID, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := documentdb.NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))

	projectRepo := bunrepo.NewProjectRepository(db)
	uc := NewTeams(projectRepo, docDB)
	ownerID := "owner-user-id"
	ownerEmail := "owner@torchwood.local"
	principal := databases.Principal{Roles: []string{"users", "user:" + ownerID}}
	team, ownerMembership, err := uc.CreateTeamWithOwner(ctx, projectID, "Engineering", ownerID, ownerEmail, principal)
	require.NoError(t, err)
	require.NotEmpty(t, team.ID)
	require.Equal(t, teams.StatusAccepted, ownerMembership.Data["status"])
	require.Equal(t, int64(1), teamTotal(t, team))

	ownerRoles := databases.Principal{Roles: []string{"users", "user:" + ownerID, "team:" + team.ID}}

	memberUserID := "member-user-id"
	_, err = docDB.CreateDocument(ctx, projectID, "default", "users", databases.Document{
		ID: memberUserID,
		Data: map[string]any{
			"email":         "member@torchwood.local",
			"password_hash": "hash",
			"name":          "Member User",
			"status":        "active",
		},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)

	invite, err := uc.CreateMembership(ctx, projectID, CreateMembershipCommand{
		TeamID: team.ID,
		Email:  "member@torchwood.local",
		Name:   "Member User",
		Roles:  []string{teams.RoleMember},
	}, ownerRoles)
	require.NoError(t, err)
	require.Equal(t, teams.StatusPending, invite.Data["status"])
	require.Equal(t, memberUserID, invite.Data["user_id"])

	memberRoles := databases.Principal{Roles: []string{"users", "user:" + memberUserID}}
	authCtx := contexts.WithPrincipal(ctx, &shared.Principal{
		ProjectID: projectID,
		UserID:    memberUserID,
		Email:     "member@torchwood.local",
		Roles:     memberRoles.Roles,
	})

	accepted, err := uc.UpdateMembershipStatus(authCtx, projectID, team.ID, invite.ID, teams.StatusAccepted, memberRoles)
	require.NoError(t, err)
	require.Equal(t, teams.StatusAccepted, accepted.Data["status"])
	require.Equal(t, memberUserID, accepted.Data["user_id"])

	teamAfter, err := uc.GetTeam(ctx, projectID, team.ID, ownerRoles)
	require.NoError(t, err)
	require.Equal(t, int64(2), teamTotal(t, teamAfter))

	list, _, _, err := uc.ListMemberships(ctx, projectID, team.ID, databases.Query{}, ownerRoles)
	require.NoError(t, err)
	require.Len(t, list, 2)

	updated, err := uc.UpdateMembership(ctx, projectID, team.ID, accepted.ID, UpdateMembershipCommand{
		Roles: []string{teams.RoleAdmin},
	}, ownerRoles)
	require.NoError(t, err)
	require.Equal(t, []string{teams.RoleAdmin}, stringSliceField(updated.Data["roles"]))

	teamRoles, err := uc.ListAcceptedTeamRoles(ctx, projectID, memberUserID)
	require.NoError(t, err)
	require.Contains(t, teamRoles, "team:"+team.ID)
	require.Contains(t, teamRoles, "member:"+accepted.ID)

	require.NoError(t, uc.DeleteMembership(authCtx, projectID, team.ID, accepted.ID, memberRoles))

	teamAfterLeave, err := uc.GetTeam(ctx, projectID, team.ID, ownerRoles)
	require.NoError(t, err)
	require.Equal(t, int64(1), teamTotal(t, teamAfterLeave))

	require.NoError(t, uc.DeleteTeam(ctx, projectID, team.ID, ownerRoles))
	left, err := uc.GetTeam(ctx, projectID, team.ID, ownerRoles)
	require.NoError(t, err)
	require.Nil(t, left)
}

func teamTotal(t *testing.T, doc *databases.Document) int64 {
	t.Helper()
	switch v := doc.Data["total"].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	default:
		t.Fatalf("unexpected total type %T", v)
		return 0
	}
}

func stringSliceField(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		if s, ok := v.([]string); ok {
			return s
		}
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
