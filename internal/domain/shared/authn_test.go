package shared

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAuthnRequest_ConsoleCookieIsToken(t *testing.T) {
	t.Parallel()
	ct, raw, err := ParseAuthnRequest(AuthnRequest{
		CookieHeaders: []string{ConsoleSessionCookieName + "=jwt-access"},
	})
	require.NoError(t, err)
	require.Equal(t, CredentialTypeToken, ct)
	require.Equal(t, "jwt-access", raw)
}

func TestParseAuthnRequest_ProjectCookieIsSession(t *testing.T) {
	t.Parallel()
	ct, raw, err := ParseAuthnRequest(AuthnRequest{
		CookieHeaders: []string{SessionCookiePrefix + "proj-1=hmac-session"},
	})
	require.NoError(t, err)
	require.Equal(t, CredentialTypeSession, ct)
	require.Equal(t, "hmac-session", raw)
}

func TestParseAuthnRequest_MultipleCredentials(t *testing.T) {
	t.Parallel()
	_, _, err := ParseAuthnRequest(AuthnRequest{
		Authorization: []string{"Bearer tok"},
		APIKey:        []string{"key"},
	})
	require.ErrorIs(t, err, ErrMultipleCredentials)
}

func TestParseAuthnRequest_SameKeyMultipleValues(t *testing.T) {
	t.Parallel()
	_, _, err := ParseAuthnRequest(AuthnRequest{
		APIKey: []string{"k1", "k2"},
	})
	require.ErrorIs(t, err, ErrMultipleCredentials)
}

func TestParseAuthnRequest_Missing(t *testing.T) {
	t.Parallel()
	_, _, err := ParseAuthnRequest(AuthnRequest{})
	require.True(t, errors.Is(err, ErrMissingCredential))
}

func TestParseAuthorizationHeader(t *testing.T) {
	t.Parallel()
	ct, tok, ok := ParseAuthorizationHeader("Bearer abc")
	require.True(t, ok)
	require.Equal(t, CredentialTypeToken, ct)
	require.Equal(t, "abc", tok)
	_, _, ok = ParseAuthorizationHeader("Basic abc")
	require.False(t, ok)
}
