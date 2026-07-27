package jwtparser

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseAllowExpired(t *testing.T) {
	t.Parallel()
	secret := []byte("jwtparser-test-secret")

	expired, err := Generate(secret, Claims{
		UserID:    "user-1",
		ActorKind: "admin",
		TokenType: TokenTypeAccess,
		IssuedAt:  time.Now().Add(-2 * time.Hour).Unix(),
		ExpiresAt: time.Now().Add(-time.Hour).Unix(),
	})
	require.NoError(t, err)

	_, ok := Parse(secret, expired)
	require.False(t, ok, "Parse must reject expired tokens")

	claims, ok := ParseAllowExpired(secret, expired)
	require.True(t, ok)
	require.Equal(t, "user-1", claims.UserID)

	_, ok = ParseAllowExpired([]byte("wrong-secret"), expired)
	require.False(t, ok, "ParseAllowExpired must still verify the signature")

	_, ok = ParseAllowExpired(secret, "not-a-token")
	require.False(t, ok)

	valid, err := Generate(secret, Claims{
		UserID:    "user-2",
		TokenType: TokenTypeAccess,
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	require.NoError(t, err)
	claims, ok = ParseAllowExpired(secret, valid)
	require.True(t, ok)
	require.Equal(t, "user-2", claims.UserID)
}
