package jwtparser

import (
	"crypto/hmac"
	"crypto/sha256"
)

// Key purposes for domain-separated sub-key derivation. Tokens signed for one
// purpose must never validate under another purpose's key.
const (
	PurposeEndUserJWT    = "end-user-jwt"
	PurposeAdminJWT      = "admin-jwt"
	PurposeSessionCookie = "session-cookie"
	PurposeFileToken     = "file-token"
)

// DeriveKey derives a purpose-specific sub-key from the master secret using
// HMAC-SHA256(master, purpose). Changing the master secret or a purpose label
// invalidates all credentials derived for that purpose.
func DeriveKey(master, purpose string) []byte {
	mac := hmac.New(sha256.New, []byte(master))
	_, _ = mac.Write([]byte(purpose))
	return mac.Sum(nil)
}
