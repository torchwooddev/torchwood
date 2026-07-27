package auth

import (
	"context"
	"time"
)

// OTPChallengeStore persists one-time password challenges and enforces send rate limits.
// code 为明文验证码，由实现方负责带密钥哈希后存储（见 infra/auth RedisOTPChallengeStore）。
type OTPChallengeStore interface {
	CheckSendRateLimit(ctx context.Context, projectID, target, ip string) error
	CreateEmailChallenge(ctx context.Context, projectID, email, code string) (challengeID string, expireAt time.Time, err error)
	VerifyEmailChallenge(ctx context.Context, projectID, challengeID, email, code string) error
	CreatePhoneChallenge(ctx context.Context, projectID, phone, code string) (challengeID string, expireAt time.Time, err error)
	VerifyPhoneChallenge(ctx context.Context, projectID, challengeID, phone, code string) error
}
