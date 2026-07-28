package auth

import "context"

// Session provider identifiers stored on session documents.
const (
	ProviderEmail     = "email"
	ProviderEmailOTP  = "email_otp"
	ProviderPhoneOTP  = "phone_otp"
	ProviderPhone     = "phone"
	ProviderAnonymous = "anonymous"
)

const (
	OTPChannelEmail = "email"
	OTPChannelPhone = "phone"
)

// TokenBundle holds JWT access and refresh tokens for an authenticated session.
type TokenBundle struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    int64
	// RefreshTokenID is the jti of RefreshToken; used for rotation bookkeeping
	// and never mapped to proto responses.
	RefreshTokenID string
}

// UserRoleResolver loads JWT role claims for a user at token issuance time.
type UserRoleResolver interface {
	LoadUserRoles(ctx context.Context, projectID, userID string) ([]string, error)
}

// SessionService creates sessions and issues JWT tokens for authenticated users.
type SessionService interface {
	CreateSessionAndTokens(ctx context.Context, projectID, userID, email, provider string) (*TokenBundle, string, error)
	IssueTokens(ctx context.Context, projectID, userID, email, sessionID string) (*TokenBundle, string, error)
	// IssueTokensWithRefreshID issues tokens with a caller-provided refresh token id
	// (jti) so the rotation store and the issued token stay in sync.
	IssueTokensWithRefreshID(ctx context.Context, projectID, userID, email, sessionID, refreshTokenID string) (*TokenBundle, string, error)
	EnsureActiveSession(ctx context.Context, projectID, sessionID, userID string) error
	// DeleteSessionsByUser removes every session of the user (e.g. after a password change).
	DeleteSessionsByUser(ctx context.Context, projectID, userID string) error
}
