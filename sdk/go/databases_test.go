package torchwood

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientDatabases_DocumentCRUD(t *testing.T) {
	c, _ := clientAPI(t, WithAccessToken("jwt-1"), WithDatabaseID("app"))
	ctx := context.Background()
	docs := c.Databases

	created, err := docs.CreateDocument(ctx, "members", "m1",
		map[string]any{"channel_id": "ch1", "user_id": "u1", "last_read_seq": 10},
		[]string{"read:user:u1"})
	require.NoError(t, err)
	require.Equal(t, "m1", created.Id)
	require.Equal(t, float64(10), created.Data.GetFields()["last_read_seq"].GetNumberValue())

	got, err := docs.GetDocument(ctx, "members", "m1")
	require.NoError(t, err)
	require.Equal(t, "m1", got.Id)

	updated, err := docs.UpdateDocument(ctx, "members", "m1",
		map[string]any{"last_read_seq": 42}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "m1", updated.Id)

	require.NoError(t, docs.DeleteDocument(ctx, "members", "m1"))
}

func TestClientDatabases_UpsertForwardsConflictColumns(t *testing.T) {
	c, rec := clientAPI(t, WithAccessToken("jwt-1"), WithDatabaseID("app"))
	ctx := context.Background()

	doc, err := c.Databases.UpsertDocument(ctx, "members", "m1",
		map[string]any{"channel_id": "ch1", "user_id": "u1", "last_read_seq": 42},
		[]string{"channel_id", "user_id"},
		[]string{"read:user:u1"})
	require.NoError(t, err)
	require.Equal(t, "m1", doc.Id)

	rec.mu.Lock()
	defer rec.mu.Unlock()
	require.Len(t, rec.upserts, 1)
	require.Equal(t, []string{"channel_id", "user_id"}, rec.upserts[0].ConflictColumns)
	require.Equal(t, []string{"read:user:u1"}, rec.upserts[0].Permissions)
	require.Equal(t, "app", rec.upserts[0].DatabaseId)
}

func TestClientDatabases_ListAndCount(t *testing.T) {
	c, _ := clientAPI(t, WithAccessToken("jwt-1"), WithDatabaseID("app"))
	ctx := context.Background()

	docs, next, err := c.Databases.ListDocuments(ctx, "members",
		[]string{`equal("channel_id","ch1")`}, 20, "")
	require.NoError(t, err)
	require.Len(t, docs, 1)
	require.Equal(t, "", next)

	count, err := c.Databases.CountDocuments(ctx, "members",
		[]string{`equal("channel_id","ch1")`})
	require.NoError(t, err)
	require.Equal(t, int64(7), count)
}

func TestClientDatabases_UseDatabaseOverride(t *testing.T) {
	c, rec := clientAPI(t, WithAccessToken("jwt-1"), WithDatabaseID("app"))
	ctx := context.Background()

	_, err := c.UseDatabase("other").GetDocument(ctx, "members", "m1")
	require.NoError(t, err)

	rec.mu.Lock()
	defer rec.mu.Unlock()
	require.NotNil(t, rec.lastGetDocument)
	require.Equal(t, "other", rec.lastGetDocument.DatabaseId)
	require.Equal(t, "app", c.Databases.db)
}
