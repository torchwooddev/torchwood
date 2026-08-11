package client

import (
	"context"
	"testing"

	clientv1 "github.com/torchwooddev/torchwood/genproto/client/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func tokenBundle() *clientv1.TokenBundle {
	return &clientv1.TokenBundle{AccessToken: "jwt-1", RefreshToken: "rt-1", ExpiresAt: 1893456000}
}

func TestNewClient_RequiresTarget(t *testing.T) {
	_, err := New("")
	require.Error(t, err)
}

func TestClientAccount_SignUp(t *testing.T) {
	c, _ := newTestClient(t, WithProjectID("proj-1"))

	resp, err := c.Account.SignUp(context.Background(), "alice@example.com", "s3cret", "Alice")
	require.NoError(t, err)
	require.Equal(t, "acc-1", resp.Account.Id)
	require.Equal(t, "alice@example.com", resp.Account.Email)
}

func TestSignInSavesTokens(t *testing.T) {
	var cb *clientv1.TokenBundle
	c, fake := newTestClient(t, WithProjectID("proj-1"), WithOnTokensChanged(func(b *clientv1.TokenBundle) { cb = b }))
	fake.signInResp = &clientv1.SignInResponse{
		Account: &clientv1.Account{Id: "acc-1", Email: "a@b.c"},
		Tokens:  tokenBundle(),
	}

	resp, err := c.Account.SignIn(context.Background(), "a@b.c", "pw")
	require.NoError(t, err)
	require.Equal(t, "jwt-1", resp.Tokens.AccessToken)
	got, err := c.store.Load()
	require.NoError(t, err)
	require.Equal(t, "jwt-1", got.AccessToken)
	require.Equal(t, "rt-1", got.RefreshToken)
	require.NotNil(t, cb)
	require.Equal(t, "jwt-1", cb.AccessToken)
}

func TestSignInMFADoesNotSaveTokens(t *testing.T) {
	called := false
	c, fake := newTestClient(t, WithOnTokensChanged(func(*clientv1.TokenBundle) { called = true }))
	fake.signInResp = &clientv1.SignInResponse{
		Account:    &clientv1.Account{Id: "acc-1"},
		MfaRequired: true,
	}

	resp, err := c.Account.SignIn(context.Background(), "a@b.c", "pw")
	require.NoError(t, err)
	require.True(t, resp.MfaRequired)
	got, err := c.store.Load()
	require.NoError(t, err)
	require.Nil(t, got)
	require.False(t, called)
}

func TestClientAccount_MeAttachesBearerToken(t *testing.T) {
	c, fake := newTestClient(t, WithInitialTokens(&clientv1.TokenBundle{AccessToken: "jwt-1"}))

	me, err := c.Account.Me(context.Background())
	require.NoError(t, err)
	require.Equal(t, "acc-1", me.Id)
	auth := fake.lastAuth.Load().([]string)
	require.Equal(t, []string{"Bearer jwt-1"}, auth)
}

func TestClientAccount_RefreshToken(t *testing.T) {
	c, fake := newTestClient(t)
	fake.tokens = &clientv1.TokenBundle{AccessToken: "jwt-2", RefreshToken: "rt-2"}

	resp, err := c.Account.RefreshToken(context.Background(), "refresh-1")
	require.NoError(t, err)
	require.Equal(t, "jwt-2", resp.Tokens.AccessToken)
	got, err := c.store.Load()
	require.NoError(t, err)
	require.Equal(t, "jwt-2", got.AccessToken)
}

func TestSignOutClearsOnSuccess(t *testing.T) {
	var cb *clientv1.TokenBundle
	c, _ := newTestClient(t, WithInitialTokens(tokenBundle()), WithOnTokensChanged(func(b *clientv1.TokenBundle) { cb = b }))

	require.NoError(t, c.Account.SignOut(context.Background()))
	got, err := c.store.Load()
	require.NoError(t, err)
	require.Nil(t, got)
	require.Nil(t, cb)
}

func TestSignOutClearsOnUnauthenticated(t *testing.T) {
	c, fake := newTestClient(t, WithInitialTokens(tokenBundle()))
	fake.signOutErr = status.Error(codes.Unauthenticated, "session expired")

	err := c.Account.SignOut(context.Background())
	require.Error(t, err)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	got, err := c.store.Load()
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestSignOutKeepsOnNetworkError(t *testing.T) {
	c, fake := newTestClient(t, WithInitialTokens(tokenBundle()))
	fake.signOutErr = status.Error(codes.Unavailable, "network down")

	err := c.Account.SignOut(context.Background())
	require.Error(t, err)
	require.Equal(t, codes.Unavailable, status.Code(err))
	got, err := c.store.Load()
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "jwt-1", got.AccessToken)
}

func TestClientAccount_MeWithoutTokenOmitsAuthHeader(t *testing.T) {
	c, fake := newTestClient(t)

	_, err := c.Account.Me(context.Background())
	require.NoError(t, err)
	require.Nil(t, fake.lastAuth.Load())
}
