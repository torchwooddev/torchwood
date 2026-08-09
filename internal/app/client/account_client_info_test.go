package client

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"github.com/torchwooddev/torchwood/pkg/query"
)

func TestAccount_SignInRecordsClientInfo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := contexts.WithClientInfo(context.Background(), contexts.ClientInfo{
		IP:        "198.51.100.42",
		UserAgent: "Mozilla/5.0 Torchwood",
	})

	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	cfg := buildTestConfig()
	projectRepo := bunrepo.NewProjectRepository(db)
	docDB := documentdb.NewPostgresDocumentDB(db)
	account := NewTestAccount(cfg, projectRepo, docDB)

	_, _, _, err := account.SignUp(ctx, SignUpCommand{
		ProjectID: projectID,
		Email:     "client-info@torchwood.local",
		Password:  "User@123",
		Name:      "Client Info",
	})
	require.NoError(t, err)

	_, _, _, err = account.SignIn(ctx, SignInCommand{
		ProjectID: projectID,
		Email:     "client-info@torchwood.local",
		Password:  "User@123",
	})
	require.NoError(t, err)

	list, err := docDB.ListDocuments(ctx, projectID, "default", "sessions", databases.Query{
		Queries:  []string{query.BuildEqual("ip", "198.51.100.42")},
		PageSize: 1,
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.NotEmpty(t, list.Documents)
	found := false
	for _, doc := range list.Documents {
		if doc.Data["user_agent"] == "Mozilla/5.0 Torchwood" {
			found = true
			break
		}
	}
	require.True(t, found, "expected session with recorded user agent")
}
