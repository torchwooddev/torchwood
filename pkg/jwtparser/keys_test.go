package jwtparser

import (
	"testing"
	"time"
)

func TestDeriveKey_DomainSeparation(t *testing.T) {
	t.Parallel()
	master := "master-secret"
	claims := Claims{
		UserID:    "user-1",
		ActorKind: "admin",
		TokenType: TokenTypeAccess,
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}
	token, err := Generate(DeriveKey(master, PurposeAdminJWT), claims)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := Parse(DeriveKey(master, PurposeAdminJWT), token); !ok {
		t.Fatal("token must verify under its own purpose key")
	}
	if _, ok := Parse(DeriveKey(master, PurposeEndUserJWT), token); ok {
		t.Fatal("token must not verify under a different purpose key")
	}
	if _, ok := Parse([]byte(master), token); ok {
		t.Fatal("token must not verify under the raw master secret")
	}
}

func TestDeriveKey_Deterministic(t *testing.T) {
	t.Parallel()
	a := DeriveKey("secret", PurposeSessionCookie)
	b := DeriveKey("secret", PurposeSessionCookie)
	if string(a) != string(b) {
		t.Fatal("derivation must be deterministic")
	}
	if len(a) != 32 {
		t.Fatalf("expected 32-byte key, got %d", len(a))
	}
}
