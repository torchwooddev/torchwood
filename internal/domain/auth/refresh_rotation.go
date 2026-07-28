package auth

import (
	"context"
	"strings"
	"time"
)

// RotateResult is the outcome of an atomic refresh-token rotation attempt.
type RotateResult int

const (
	// RotateOK means the presented token id matched the stored one and the
	// store now holds the new token id.
	RotateOK RotateResult = iota
	// RotateMismatch means the presented token id does not match the stored
	// one: the token was already rotated (potential reuse).
	RotateMismatch
	// RotateMissing means there is no rotation record for the key (session
	// predates rotation, or the record expired/was lost).
	RotateMissing
)

// RefreshRotationStore tracks the currently valid refresh token id (jti) per
// session/admin so refresh tokens rotate on every use and reuse of an old
// token can be detected.
type RefreshRotationStore interface {
	// Register stores the currently valid refresh token id for the key.
	Register(ctx context.Context, key, tokenID string, ttl time.Duration) error
	// Rotate atomically compares the stored token id with presentedTokenID and,
	// on match, replaces it with newTokenID (with a fresh TTL).
	Rotate(ctx context.Context, key, presentedTokenID, newTokenID string, ttl time.Duration) (RotateResult, error)
}

// RefreshRotationKey builds the rotation store key for a scope
// (e.g. projectID + sessionID, or "admin" + adminID).
func RefreshRotationKey(parts ...string) string {
	return "Graviton:refresh:" + strings.Join(parts, ":")
}
