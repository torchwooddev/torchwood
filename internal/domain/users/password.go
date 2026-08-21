package users

import (
	"errors"
	"fmt"
	"unicode"
)

// 密码强度策略：最少 8 字符、最长 72（bcrypt 输入上限）、必须同时含字母和数字。
const (
	PasswordMinLength = 8
	PasswordMaxLength = 72
)

var (
	ErrPasswordTooShort = fmt.Errorf("password must be at least %d characters", PasswordMinLength)
	ErrPasswordTooLong  = fmt.Errorf("password must be at most %d characters", PasswordMaxLength)
	ErrPasswordWeak     = errors.New("password must contain both letters and digits")
)

func ValidatePasswordStrength(pw string) error {
	if len(pw) < PasswordMinLength {
		return ErrPasswordTooShort
	}
	if len(pw) > PasswordMaxLength {
		return ErrPasswordTooLong
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
		return ErrPasswordWeak
	}
	return nil
}
