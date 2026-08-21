package users

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/pkg/password"
)

func TestRegister_PasswordUser(t *testing.T) {
	t.Parallel()

	u, err := Register(RegisterInput{
		ID:       "user-1",
		Email:    " Alice@Torchwood.local ",
		Password: "Passw0rd",
		Name:     "Alice",
	})
	require.NoError(t, err)
	require.Equal(t, "user-1", u.ID)
	require.Equal(t, "alice@torchwood.local", u.Email)
	require.Equal(t, "Alice", u.Name)
	require.Equal(t, StatusActive, u.Status)
	require.False(t, u.EmailVerified)
	require.False(t, u.IsAnonymous())
	require.True(t, u.CanAuthenticate())
	require.NotEmpty(t, u.PasswordHash)
	ok, err := password.Verify("Passw0rd", u.PasswordHash)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []any{}, u.DocumentData()["labels"])
}

func TestRegister_RejectsWeakPasswordAndMissingIdentity(t *testing.T) {
	t.Parallel()

	_, err := Register(RegisterInput{ID: "u1", Email: "a@b.c", Password: "short"})
	require.ErrorIs(t, err, ErrPasswordTooShort)

	_, err = Register(RegisterInput{Email: "a@b.c", Password: "Passw0rd"})
	require.ErrorIs(t, err, ErrUserIDRequired)

	_, err = Register(RegisterInput{ID: "u1", Password: "Passw0rd"})
	require.ErrorIs(t, err, ErrEmailRequired)

	_, err = Register(RegisterInput{ID: "u1", Email: "a@b.c", Status: "pending"})
	require.Error(t, err)
}

func TestRegister_AnonymousAndPasswordless(t *testing.T) {
	t.Parallel()

	anonID := "anon-user-9f3a"
	u, err := Register(RegisterInput{
		ID:        anonID,
		Email:     AnonymousEmail(anonID),
		Anonymous: true,
	})
	require.NoError(t, err)
	require.True(t, u.IsAnonymous())
	require.Equal(t, "Anonymous", u.Name)
	require.Equal(t, "", u.PasswordHash)
	require.Equal(t, []string{LabelAnonymous}, u.Labels)
	require.Equal(t, "anon_anon-use@torchwood.local", u.Email)

	otp, err := Register(RegisterInput{
		ID:            "otp-1",
		Email:         "otp@torchwood.local",
		Name:          "otp",
		EmailVerified: true,
	})
	require.NoError(t, err)
	require.True(t, otp.EmailVerified)
	require.Empty(t, otp.PasswordHash)
	require.False(t, otp.IsAnonymous())
}

func TestRequireUniqueEmail(t *testing.T) {
	t.Parallel()

	require.NoError(t, RequireUniqueEmail(nil))
	err := RequireUniqueEmail(&User{ID: "u1", Email: "a@b.c"})
	require.ErrorIs(t, err, ErrEmailAlreadyRegistered)
	require.True(t, errors.Is(err, ErrEmailAlreadyRegistered))
}

func TestUserCanAuthenticate(t *testing.T) {
	t.Parallel()

	require.False(t, (*User)(nil).CanAuthenticate())
	require.True(t, (&User{Status: ""}).CanAuthenticate())
	require.True(t, (&User{Status: StatusActive}).CanAuthenticate())
	require.False(t, (&User{Status: StatusBlocked}).CanAuthenticate())
}

func TestDocumentPermissions(t *testing.T) {
	t.Parallel()

	got := DocumentPermissions("abc")
	require.Equal(t, []Permission{
		{Type: "read", Role: "user:abc"},
		{Type: "read", Role: "keys"},
		{Type: "read", Role: "admin"},
		{Type: "update", Role: "user:abc"},
		{Type: "update", Role: "admin"},
		{Type: "delete", Role: "user:abc"},
		{Type: "delete", Role: "admin"},
	}, got)
}
