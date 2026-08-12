package auth

import (
	"context"
	"time"
)

const (
	AccountTokenPurposeVerification = "verification"
	AccountTokenPurposeRecovery     = "recovery"
	AccountTokenPurposeMagicURL     = "magic_url"
	AccountTokenPurposeEmailChange  = "email_change"
)

// AccountTokenStore persists one-time account action tokens (email verification, password recovery).
type AccountTokenStore interface {
	CheckSendRateLimit(ctx context.Context, projectID, target, ip string) error
	CreateVerificationToken(ctx context.Context, projectID, userID, email string) (secret string, expireAt time.Time, err error)
	VerifyVerificationToken(ctx context.Context, projectID, userID, secret string) error
	CreateRecoveryToken(ctx context.Context, projectID, userID, email string) (secret string, expireAt time.Time, err error)
	VerifyRecoveryToken(ctx context.Context, projectID, userID, secret string) error
	// CreateMagicURLToken 返回不透明 challengeID（API 回传用）与真实 secret
	// （仅存在于邮件链接中）。
	CreateMagicURLToken(ctx context.Context, projectID, userID, email string) (challengeID string, secret string, expireAt time.Time, err error)
	VerifyMagicURLToken(ctx context.Context, projectID, userID, secret string) error
	// CreateEmailChangeToken 签发邮箱变更确认 token（record 中携带新邮箱）。
	CreateEmailChangeToken(ctx context.Context, projectID, userID, email string) (secret string, expireAt time.Time, err error)
	// VerifyEmailChangeToken 原子消费邮箱变更 token 并返回 record 中的新邮箱；
	// 消费后二次使用返回 Unauthenticated。
	VerifyEmailChangeToken(ctx context.Context, projectID, userID, secret string) (email string, err error)
}
