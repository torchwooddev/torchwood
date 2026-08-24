package auth

// OTPGenerator 生成一次性数字验证码。
type OTPGenerator interface {
	GenerateOTP(digits int) (string, error)
}
