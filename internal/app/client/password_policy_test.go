package client

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestValidatePasswordStrength(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		pw   string
		ok   bool
	}{
		{"valid", "passw0rd", true},
		{"valid_long", strings.Repeat("a1", 36), true},
		{"empty", "", false},
		{"too_short", "a1b2c3d", false},
		{"too_long", strings.Repeat("a1", 37), false},
		{"letters_only", "abcdefgh", false},
		{"digits_only", "12345678", false},
		{"unicode_letter_with_digit", "密码强度abc123", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePasswordStrength(tc.pw)
			if tc.ok {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			st, _ := status.FromError(err)
			require.Equal(t, codes.InvalidArgument, st.Code())
			require.NotEmpty(t, st.Message())
		})
	}
}
