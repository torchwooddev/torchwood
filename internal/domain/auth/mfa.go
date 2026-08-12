package auth

import (
	"context"
	"time"
)

// FactorType constants.
const (
	FactorTypeTOTP = "totp"
)

// FactorStatus constants.
const (
	FactorStatusPending  = "pending"
	FactorStatusVerified = "verified"
)

// Factor is a user MFA factor persisted in the users document "factors" field.
type Factor struct {
	ID        string
	Type      string // "totp"
	Secret    string // 加密后的密文（enc:v1:...）
	Status    string // "pending" | "verified"
	CreatedAt time.Time
}

// MFAService manages TOTP factor secrets and validates TOTP codes.
type MFAService interface {
	// CreateTOTPFactor 生成 TOTP secret 与 otpauth URL（issuer=project name、account=email）。
	// issuer 由调用方（app 层）从项目信息解析后传入。
	CreateTOTPFactor(ctx context.Context, issuer, userID, email string) (*Factor, string, string, error) // factor, plainSecret, otpauthURL
	// VerifyTOTPFactor 校验 code 并激活因子（防重放：同一 code 60s 内不可重用）。
	VerifyTOTPFactor(ctx context.Context, factor *Factor, code string) error
	// ValidateTOTP 校验 code（登录挑战用；同样做 60s 防重放与失败锁定）。
	ValidateTOTP(ctx context.Context, factor *Factor, code string) error
}

// MFAChallengeStore 存登录挑战 token（Redis，5min TTL，一次性消费）。
type MFAChallengeStore interface {
	Create(ctx context.Context, projectID, userID string) (token string, expireAt time.Time, err error)
	// Consume 一次性取出并删除；不存在/已用返回错误。
	Consume(ctx context.Context, token string) (projectID, userID string, err error)
	// RevokeByUser 作废该用户全部未消费的挑战（删除 MFA 因子时调用）。
	RevokeByUser(ctx context.Context, projectID, userID string) error
}
