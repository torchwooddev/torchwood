package auth

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCanonicalizeSessionSecretHash_DualRead(t *testing.T) {
	t.Parallel()
	plain := "550e8400-e29b-41d4-a716-446655440000"
	hashed := HashOTP(plain)
	require.Len(t, hashed, sha256HexLen)
	require.True(t, sessionSecretLooksHashed(hashed))
	require.False(t, sessionSecretLooksHashed(plain))
	require.False(t, sessionSecretLooksHashed(""))
	require.False(t, sessionSecretLooksHashed("secret"))

	require.Equal(t, hashed, canonicalizeSessionSecretHash(hashed))
	require.Equal(t, hashed, canonicalizeSessionSecretHash(plain))
	require.Equal(t, "", canonicalizeSessionSecretHash(""))
}
