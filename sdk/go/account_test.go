package torchwood

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewClient_RequiresTarget(t *testing.T) {
	_, err := NewClient("")
	require.Error(t, err)
}

func TestClientAccount_SignUp(t *testing.T) {
	c, _ := clientAPI(t, WithProjectID("proj-1"))
	ctx := context.Background()

	resp, err := c.Account.SignUp(ctx, "alice@example.com", "s3cret", "Alice")
	require.NoError(t, err)
	require.Equal(t, "acc-1", resp.Account.Id)
	require.Equal(t, "alice@example.com", resp.Account.Email)
}

func TestClientAccount_SignInSetsProject(t *testing.T) {
	c, _ := clientAPI(t, WithProjectID("proj-1"))
	ctx := context.Background()

	resp, err := c.Account.SignIn(ctx, "alice@example.com", "s3cret")
	require.NoError(t, err)
	require.Equal(t, "jwt-1", resp.Tokens.AccessToken)
	require.Equal(t, "rt-1", resp.Tokens.RefreshToken)
}

func TestClientAccount_MeAttachesBearerToken(t *testing.T) {
	c, rec := clientAPI(t, WithAccessToken("jwt-1"))

	me, err := c.Account.Me(context.Background())
	require.NoError(t, err)
	require.Equal(t, "acc-1", me.Id)
	require.Equal(t, "Bearer jwt-1", rec.auth("authorization"))
}

func TestClientAccount_RefreshToken(t *testing.T) {
	c, _ := clientAPI(t)

	resp, err := c.Account.RefreshToken(context.Background(), "rt-1")
	require.NoError(t, err)
	require.Equal(t, "jwt-2", resp.Tokens.AccessToken)
}

func TestClientAccount_SignOut(t *testing.T) {
	c, _ := clientAPI(t, WithAccessToken("jwt-1"))

	err := c.Account.SignOut(context.Background())
	require.NoError(t, err)
}

func TestClientAccount_MeWithoutTokenOmitsAuthHeader(t *testing.T) {
	c, rec := clientAPI(t)

	_, err := c.Account.Me(context.Background())
	require.NoError(t, err)
	require.Equal(t, "", rec.auth("authorization"))
}
