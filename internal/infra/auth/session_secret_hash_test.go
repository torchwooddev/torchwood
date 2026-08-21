package auth

import (
	"strings"
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
	require.False(t, sessionSecretLooksHashed(strings.Repeat("g", 64)))
	require.False(t, sessionSecretLooksHashed(hashed[:63]))
	require.False(t, sessionSecretLooksHashed(hashed+"a"))

	upper := strings.ToUpper(hashed)
	require.True(t, sessionSecretLooksHashed(upper), "hex.DecodeString 接受 A-F")
	require.Equal(t, upper, canonicalizeSessionSecretHash(upper), "已是 64 hex 则不得二次哈希")

	require.Equal(t, hashed, canonicalizeSessionSecretHash(hashed), "已哈希行原样返回")
	require.Equal(t, hashed, canonicalizeSessionSecretHash(plain))
	require.Equal(t, HashOTP(strings.Repeat("g", 64)), canonicalizeSessionSecretHash(strings.Repeat("g", 64)))
	require.Equal(t, "", canonicalizeSessionSecretHash(""))
}
