package assets

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	// MaxIdempotencyKey 是幂等键最大长度。
	MaxIdempotencyKey = 128
)

var codePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// ValidateCode 规范化并校验定义 code。
func ValidateCode(code string) (string, error) {
	c := NormalizeCode(code)
	if !codePattern.MatchString(c) {
		return "", fmt.Errorf("%w: def code must match ^[a-z][a-z0-9_]{0,63}$", ErrInvalidCode)
	}
	return c, nil
}

// ValidateIdempotencyKey 校验写路径幂等键（必填、长度上限）。
func ValidateIdempotencyKey(key string) (string, error) {
	k := strings.TrimSpace(key)
	if k == "" {
		return "", ErrIdempotencyRequired
	}
	if len(k) > MaxIdempotencyKey {
		return "", fmt.Errorf("%w: exceeds %d characters", ErrIdempotencyRequired, MaxIdempotencyKey)
	}
	return k, nil
}
