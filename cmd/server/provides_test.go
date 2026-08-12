package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateJWTSecret(t *testing.T) {
	t.Parallel()

	// 40 字符的强随机串，不含任何弱子串。
	const strong = "B9f2kQx7LmZp4RtW8vNc2hJ6xKq3sM5uA1eD7gYp"

	cases := []struct {
		name    string
		secret  string
		wantErr string // 空串表示期望通过
	}{
		{"empty", "", "must be set"},
		{"whitespace only", "   \t  ", "must be set"},
		{"short", "abcdefgh", "too short"},
		{"short but exact weak value", "change-me", "too short"},
		{"weak value padded to length", strong[:20] + "changeme" + strong[28:], "known weak"},
		{"weak substring in middle", "B9f2kQ-x7Lm-change-me-Zp4RtW8vNc2hJ6xKq3sM", "known weak"},
		{"weak substring case-insensitive", "MINIOADMIN" + strong[10:], "known weak"},
		{"substring resembling secret", "Xy1" + "secret" + strings.Repeat("z", 29), "known weak"},
		{"strong value", strong, ""},
		{"strong value with surrounding spaces", "  " + strong + "  ", ""},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateJWTSecret(tc.secret)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestValidateJWTSecret_WeakSubstringNeverPasses(t *testing.T) {
	t.Parallel()
	for _, w := range weakJWTSecretTokens {
		secret := "K7pQ2xR9mW4tZ8vC3hN6jB1sL5dF0gY" + w + "H8uE2iA4oP6qS7rT9wX"
		require.Error(t, validateJWTSecret(secret), "weak token %q must be rejected", w)
	}
}
