package client

import (
	"unicode"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// 密码强度策略：最少 8 字符、最长 72（bcrypt 输入上限）、必须同时含字母和数字。
const (
	passwordMinLength = 8
	passwordMaxLength = 72
)

func validatePasswordStrength(pw string) error {
	if len(pw) < passwordMinLength {
		return status.Errorf(codes.InvalidArgument, "password must be at least %d characters", passwordMinLength)
	}
	if len(pw) > passwordMaxLength {
		return status.Errorf(codes.InvalidArgument, "password must be at most %d characters", passwordMaxLength)
	}
	var hasLetter, hasDigit bool
	for _, r := range pw {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return status.Error(codes.InvalidArgument, "password must contain both letters and digits")
	}
	return nil
}
