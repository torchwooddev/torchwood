package torchwood

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestServerClient_AttachesAPIKeyAndProjectHeaders(t *testing.T) {
	c, rec := serverAPI(t, WithServerAPIKey("key-1"), WithProjectID("proj-1"))

	_, err := c.Health.Check(context.Background())
	require.NoError(t, err)
	require.Equal(t, "key-1", rec.auth("x-api-key"))
	require.Equal(t, "proj-1", rec.auth("x-torchwood-project"))
}

func TestServerClient_HealthCheckAndVersion(t *testing.T) {
	c, _ := serverAPI(t, WithServerAPIKey("key-1"))
	ctx := context.Background()

	check, err := c.Health.Check(ctx)
	require.NoError(t, err)
	require.Equal(t, "SERVING", check.Status)

	version, err := c.Health.Version(ctx)
	require.NoError(t, err)
	require.Equal(t, "v0.1.0", version.Version)
	require.Equal(t, "abc123", version.Commit)
}

func TestServerClient_SetAPIKey(t *testing.T) {
	c, _ := serverAPI(t)
	c.SetAPIKey("key-2")

	_, err := c.Health.Check(context.Background())
	require.NoError(t, err)
}

func TestServerUsers_CreateUserAndToken(t *testing.T) {
	c, rec := serverAPI(t, WithServerAPIKey("key-1"))
	ctx := context.Background()

	user, err := c.Users.CreateUser(ctx, "agent-1@agents.local", "pw", "Agent One", "active", nil, nil)
	require.NoError(t, err)
	require.Equal(t, "user-1", user.Id)
	require.Equal(t, "active", user.Status)

	rec.mu.Lock()
	require.Equal(t, "agent-1@agents.local", rec.createdUser.Email)
	require.Equal(t, "pw", rec.createdUser.Password)
	rec.mu.Unlock()

	got, err := c.Users.GetUser(ctx, "user-1")
	require.NoError(t, err)
	require.Equal(t, "agent-1@agents.local", got.Email)

	tok, err := c.Users.CreateUserToken(ctx, "user-1")
	require.NoError(t, err)
	require.Equal(t, "agent-token", tok.Tokens.AccessToken)
}

func TestServerUsers_ListSessionsAndDelete(t *testing.T) {
	c, _ := serverAPI(t, WithServerAPIKey("key-1"))
	ctx := context.Background()

	sessions, err := c.Users.ListUserSessions(ctx, "user-1")
	require.NoError(t, err)
	require.Len(t, sessions.Sessions, 1)

	require.NoError(t, c.Users.DeleteUserSession(ctx, "user-1", "sess-1"))
	require.NoError(t, c.Users.DeleteUser(ctx, "user-1"))
}

func TestServerTeams_CreateTeamAndMembership(t *testing.T) {
	c, _ := serverAPI(t, WithServerAPIKey("key-1"))
	ctx := context.Background()

	team, err := c.Teams.CreateTeam(ctx, "Team One", []string{"read"})
	require.NoError(t, err)
	require.Equal(t, "Team One", team.Name)
	require.Equal(t, []string{"read"}, team.Permissions)

	got, err := c.Teams.GetTeam(ctx, "team-1")
	require.NoError(t, err)
	require.Equal(t, "team-1", got.Id)

	mem, err := c.Teams.CreateMembership(ctx, "team-1", "user-1", "", "", []string{"member"}, "active")
	require.NoError(t, err)
	require.Equal(t, "user-1", mem.UserId)
	require.Equal(t, "active", mem.Status)

	listed, err := c.Teams.ListMemberships(ctx, "team-1")
	require.NoError(t, err)
	require.Len(t, listed.Memberships, 1)
}

func TestServerDatabases_SchemaSetup(t *testing.T) {
	c, rec := serverAPI(t, WithServerAPIKey("key-1"), WithDatabaseID("app"))
	ctx := context.Background()
	db := c.Databases

	created, err := db.CreateDatabase(ctx, "app", "Application DB")
	require.NoError(t, err)
	require.Equal(t, "app", created.Id)

	col, err := db.CreateCollection(ctx, "members", "Members",
		[]string{"read:user:*"}, true)
	require.NoError(t, err)
	require.Equal(t, "members", col.Id)
	require.Equal(t, "Members", col.Name)

	rec.mu.Lock()
	require.NotNil(t, rec.lastCollection)
	require.True(t, *rec.lastCollection.DocumentSecurity)
	require.Equal(t, []string{"read:user:*"}, rec.lastCollection.Permissions)
	rec.mu.Unlock()

	attr, err := db.CreateAttribute(ctx, "members", "channel_id", "string", 64, true, false)
	require.NoError(t, err)
	require.Equal(t, "channel_id", attr.Key)
	require.Equal(t, "string", attr.Type)
	require.True(t, attr.Required)

	idx, err := db.CreateIndex(ctx, "members", "members_channel_user", "unique",
		[]string{"channel_id", "user_id"})
	require.NoError(t, err)
	require.Equal(t, "unique", idx.Type)
	require.Equal(t, []string{"channel_id", "user_id"}, idx.Attributes)
}

func TestServerDatabases_CountAndListDocuments(t *testing.T) {
	c, _ := serverAPI(t, WithServerAPIKey("key-1"), WithDatabaseID("app"))
	ctx := context.Background()

	count, err := c.Databases.CountDocuments(ctx, "messages",
		[]string{`equal("channel_id","ch1")`})
	require.NoError(t, err)
	require.Equal(t, int64(42), count)

	docs, next, err := c.Databases.ListDocuments(ctx, "messages",
		[]string{`equal("channel_id","ch1")`}, 20, "")
	require.NoError(t, err)
	require.Len(t, docs, 2)
	require.Equal(t, "next-token", next)
}

func TestServerDatabases_BulkOperations(t *testing.T) {
	c, _ := serverAPI(t, WithServerAPIKey("key-1"), WithDatabaseID("app"))
	ctx := context.Background()

	resp, err := c.Databases.BulkUpdateDocuments(ctx, "members", []string{"m1", "m2"},
		map[string]any{"last_read_seq": 1}, nil)
	require.NoError(t, err)
	require.Equal(t, int64(2), resp.Affected)

	del, err := c.Databases.BulkDeleteDocuments(ctx, "members", []string{"m1"})
	require.NoError(t, err)
	require.Equal(t, int64(1), del.Affected)
}
