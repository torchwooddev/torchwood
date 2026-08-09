package bunrepo_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	domainauth "github.com/torchwooddev/torchwood/internal/domain/auth"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"github.com/torchwooddev/torchwood/pkg/secretbox"
)

func TestOAuthProviderRepository_CRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	repo := bunrepo.NewOAuthProviderRepository(db, &config.AppConfig{
		Security: &config.Security{
			Jwt: &config.Security_Jwt{Secret: "test-jwt-secret"},
		},
	})

	list, err := repo.ListOAuthProviders(ctx, projectID)
	require.NoError(t, err)
	require.Empty(t, list)

	cfg := &projects.OAuthProvider{
		ProjectID:    projectID,
		Provider:     domainauth.ProviderGoogle,
		Enabled:      true,
		ClientID:     "id-1",
		ClientSecret: "secret-1",
		Scopes:       []string{"openid", "email"},
	}
	require.NoError(t, repo.UpsertOAuthProvider(ctx, cfg))

	stored, err := repo.GetOAuthProvider(ctx, projectID, domainauth.ProviderGoogle)
	require.NoError(t, err)
	require.NotNil(t, stored)
	require.Equal(t, "id-1", stored.ClientID)
	require.Equal(t, "secret-1", stored.ClientSecret)

	var row model.ProjectOAuthProvider
	require.NoError(t, db.NewSelect().Model(&row).
		Where("project_id = ? AND provider = ?", projectID, domainauth.ProviderGoogle).
		Scan(ctx))
	require.NotEqual(t, "secret-1", row.ClientSecret)
	require.True(t, stringsHasEncPrefix(row.ClientSecret))
	plain, err := secretbox.Decrypt(row.ClientSecret, "test-jwt-secret")
	require.NoError(t, err)
	require.Equal(t, "secret-1", plain)

	cfg.ClientID = "id-2"
	cfg.ClientSecret = "secret-2"
	cfg.Scopes = []string{"openid"}
	require.NoError(t, repo.UpsertOAuthProvider(ctx, cfg))

	stored, err = repo.GetOAuthProvider(ctx, projectID, domainauth.ProviderGoogle)
	require.NoError(t, err)
	require.Equal(t, "id-2", stored.ClientID)

	list, err = repo.ListOAuthProviders(ctx, projectID)
	require.NoError(t, err)
	require.Len(t, list, 1)

	require.NoError(t, repo.DeleteOAuthProvider(ctx, projectID, domainauth.ProviderGoogle))

	stored, err = repo.GetOAuthProvider(ctx, projectID, domainauth.ProviderGoogle)
	require.NoError(t, err)
	require.Nil(t, stored)
}

func stringsHasEncPrefix(s string) bool {
	return strings.HasPrefix(s, "enc:v1:")
}
