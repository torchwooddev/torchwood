package auth

import (
	"context"
	"time"
)

const (
	AccountTokenPurposeVerification = "verification"
	AccountTokenPurposeRecovery     = "recovery"
	AccountTokenPurposeMagicURL     = "magic_url"
)

// AccountTokenStore persists one-time account action tokens (email verification, password recovery).
type AccountTokenStore interface {
	CheckSendRateLimit(ctx context.Context, projectID, target, ip string) error
	CreateVerificationToken(ctx context.Context, projectID, userID, email string) (secret string, expireAt time.Time, err error)
	VerifyVerificationToken(ctx context.Context, projectID, userID, secret string) error
	CreateRecoveryToken(ctx context.Context, projectID, userID, email string) (secret string, expireAt time.Time, err error)
	VerifyRecoveryToken(ctx context.Context, projectID, userID, secret string) error
	CreateMagicURLToken(ctx context.Context, projectID, userID, email string) (secret string, expireAt time.Time, err error)
	VerifyMagicURLToken(ctx context.Context, projectID, userID, secret string) error
}
