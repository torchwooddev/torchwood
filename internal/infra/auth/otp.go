package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
)

// GenerateOTP returns a numeric one-time password with the given number of digits.
func GenerateOTP(digits int) (string, error) {
	if digits <= 0 || digits > 10 {
		return "", fmt.Errorf("invalid otp digits: %d", digits)
	}
	max := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(digits)), nil)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	format := fmt.Sprintf("%%0%dd", digits)
	return fmt.Sprintf(format, n), nil
}

// HashOTP returns a SHA-256 hex digest of the given value.
// 仅适用于高熵 secret（如 account token，256-bit）；低熵 OTP 验证码
// 必须使用带密钥的哈希（见 RedisOTPChallengeStore 的 HMAC-SHA256）。
func HashOTP(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}
