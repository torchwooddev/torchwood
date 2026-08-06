package server

import (
	"context"
	"testing"

	"github.com/torchwoodio/torchwood/internal/domain/databases"
	"github.com/torchwoodio/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwoodio/torchwood/internal/infra/documentdb"
	"github.com/torchwoodio/torchwood/internal/testutil"
	"github.com/stretchr/testify/require"
)

// TestDatabases_AcceptanceChain covers manual checklist §4.14–4.18:
// create database → collection → attribute → index, then delete in reverse order.
func TestDatabases_AcceptanceChain(t *testing.T) {
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

	uc := NewDatabases(bunrepo.NewProjectRepository(db), docDB)

	const (
		dbID    = "app"
		collID  = "posts"
		attrKey = "title"
		indexID = "idx_title"
	)

	require.NoError(t, uc.CreateDatabase(ctx, projectID, dbID, "Application DB"))

	dbs, err := uc.ListDatabases(ctx, projectID)
	require.NoError(t, err)
	require.NotEmpty(t, dbs)

	gotDB, err := uc.GetDatabase(ctx, projectID, dbID)
	require.NoError(t, err)
	require.NotNil(t, gotDB)
	require.Equal(t, dbID, gotDB.ID)

	require.NoError(t, uc.CreateCollection(ctx, projectID, dbID, collID, "Posts", nil, nil, nil, true))

	colls, err := uc.ListCollections(ctx, projectID, dbID)
	require.NoError(t, err)
	require.Len(t, colls, 1)

	gotColl, err := uc.GetCollection(ctx, projectID, dbID, collID)
	require.NoError(t, err)
	require.Equal(t, collID, gotColl.ID)

	require.NoError(t, uc.CreateAttribute(ctx, projectID, dbID, collID, databases.Attribute{
		ID:   attrKey,
		Key:  attrKey,
		Type: "string",
		Size: 256,
	}))

	require.NoError(t, uc.CreateIndex(ctx, projectID, dbID, collID, databases.Index{
		ID:         indexID,
		Type:       "unique",
		Attributes: []string{attrKey},
	}))

	gotColl, err = uc.GetCollection(ctx, projectID, dbID, collID)
	require.NoError(t, err)
	require.Len(t, gotColl.Attributes, 1)
	require.Equal(t, attrKey, gotColl.Attributes[0].Key)
	require.Len(t, gotColl.Indexes, 1)
	require.Equal(t, indexID, gotColl.Indexes[0].ID)

	require.NoError(t, uc.DeleteCollection(ctx, projectID, dbID, collID))
	gotColl, err = uc.GetCollection(ctx, projectID, dbID, collID)
	require.NoError(t, err)
	require.Nil(t, gotColl)

	require.NoError(t, uc.DeleteDatabase(ctx, projectID, dbID))
	gotDB, err = uc.GetDatabase(ctx, projectID, dbID)
	require.NoError(t, err)
	require.Nil(t, gotDB)
}
