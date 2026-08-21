package users

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidatePasswordStrength(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		pw   string
		want error
	}{
		{"valid", "passw0rd", nil},
		{"valid_long", strings.Repeat("a1", 36), nil},
		{"empty", "", ErrPasswordTooShort},
		{"too_short", "a1b2c3d", ErrPasswordTooShort},
		{"too_long", strings.Repeat("a1", 37), ErrPasswordTooLong},
		{"letters_only", "abcdefgh", ErrPasswordWeak},
		{"digits_only", "12345678", ErrPasswordWeak},
		{"unicode_letter_with_digit", "密码强度abc123", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePasswordStrength(tc.pw)
			if tc.want == nil {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.True(t, errors.Is(err, tc.want), err)
		})
	}
}
