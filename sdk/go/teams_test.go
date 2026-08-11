package torchwood

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientTeams_CreateAndList(t *testing.T) {
	c, _ := clientAPI(t, WithAccessToken("jwt-1"))
	ctx := context.Background()

	team, err := c.Teams.CreateTeam(ctx, "Team One")
	require.NoError(t, err)
	require.Equal(t, "team-1", team.Id)
	require.Equal(t, "Team One", team.Name)

	got, err := c.Teams.GetTeam(ctx, "team-1")
	require.NoError(t, err)
	require.Equal(t, "Team One", got.Name)

	list, err := c.Teams.ListTeams(ctx)
	require.NoError(t, err)
	require.Len(t, list.Teams, 1)
}

func TestClientTeams_Memberships(t *testing.T) {
	c, _ := clientAPI(t, WithAccessToken("jwt-1"))
	ctx := context.Background()

	mem, err := c.Teams.CreateMembership(ctx, "team-1", "bob@example.com", "Bob", []string{"member"})
	require.NoError(t, err)
	require.Equal(t, "team-1", mem.TeamId)
	require.Equal(t, []string{"member"}, mem.Roles)

	listed, err := c.Teams.ListMemberships(ctx, "team-1")
	require.NoError(t, err)
	require.Len(t, listed.Memberships, 1)

	updated, err := c.Teams.UpdateMembershipStatus(ctx, "team-1", "mem-1", "active")
	require.NoError(t, err)
	require.Equal(t, "active", updated.Status)

	require.NoError(t, c.Teams.DeleteMembership(ctx, "team-1", "mem-1"))
}
