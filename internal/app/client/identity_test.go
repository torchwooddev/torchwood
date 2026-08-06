package client

import (
	"context"
	"testing"

	domainauth "github.com/torchwoodio/torchwood/internal/domain/auth"
	"github.com/torchwoodio/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwoodio/torchwood/internal/infra/documentdb"
	"github.com/torchwoodio/torchwood/internal/testutil"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestResolveOAuthUser_RejectsExistingEmailWithoutIdentity(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
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
		Email:     "existing@torchwood.local",
		Password:  "User@123",
		Name:      "Existing",
	})
	require.NoError(t, err)

	_, err = account.resolveOAuthUser(ctx, projectID, domainauth.ProviderGoogle, &domainauth.OAuthUserInfo{
		ProviderUID: "google-123",
		Email:       "existing@torchwood.local",
		Name:        "OAuth User",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.FailedPrecondition, st.Code())
}
