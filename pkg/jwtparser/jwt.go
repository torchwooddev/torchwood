package jwtparser

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	ClaimVarProjectID = "project_id"
	ClaimVarSessionID = "session_id"
	ClaimVarRoles     = "roles"
	ClaimVarTokenType = "token_type"
)

const (
	TokenTypeAccess  = "access"
	TokenTypeRefresh = "refresh"
)

const IssuerTorchwood = "torchwood"

type Claims struct {
	TokenID   string           `json:"tid,omitempty"`
	UserID    string           `json:"uid,omitempty"`
	Username  string           `json:"usn,omitempty"`
	ActorKind string           `json:"akd,omitempty"` // end_user / admin / service
	ProjectID string           `json:"pid,omitempty"`
	SessionID string           `json:"sid,omitempty"`
	TokenType string           `json:"ttp,omitempty"` // access / refresh
	Roles     []string         `json:"rls,omitempty"`
	Scopes    []string         `json:"scp,omitempty"`
	OneTime   bool             `json:"one_time,omitempty"` // 一次性 JWT：验证方必须消费后放行
	Imp       string           `json:"imp,omitempty"`      // 模拟登录时为 impersonator admin id（审计用），端用户正常登录为空
	Issuer    string           `json:"iss,omitempty"`
	Audience  jwt.ClaimStrings `json:"aud,omitempty"`
	ExpiresAt int64            `json:"exp,omitempty"`
	IssuedAt  int64            `json:"iat,omitempty"`
}

func (c *Claims) GetExpirationTime() (*jwt.NumericDate, error) {
	return jwt.NewNumericDate(time.Unix(c.ExpiresAt, 0)), nil
}

func (c *Claims) GetNotBefore() (*jwt.NumericDate, error) { return nil, nil }

func (c *Claims) GetIssuedAt() (*jwt.NumericDate, error) {
	return jwt.NewNumericDate(time.Unix(c.IssuedAt, 0)), nil
}

func (c *Claims) GetAudience() (jwt.ClaimStrings, error) { return c.Audience, nil }

func (c *Claims) GetIssuer() (string, error) { return c.Issuer, nil }

func (c *Claims) GetSubject() (string, error) { return c.UserID, nil }

// Parse validates and parses a JWT signed with HS256.
func Parse(secret []byte, tokenString string) (*Claims, bool) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return secret, nil
	}, jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		return nil, false
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, false
	}
	// iss 校验：已签发的新 token 必须为 IssuerTorchwood；空 iss 兼容存量旧 token（过渡期）。
	if claims.Issuer != "" && claims.Issuer != IssuerTorchwood {
		return nil, false
	}
	return claims, true
}

// ParseAllowExpired validates the signature and claims of a JWT signed with HS256
// but tolerates an expired token. Intended for best-effort flows such as sign-out,
// where the caller only needs the identity of an already-expired credential.
func ParseAllowExpired(secret []byte, tokenString string) (*Claims, bool) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return secret, nil
	}, jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithValidMethods([]string{"HS256"}))
	if err != nil && !errors.Is(err, jwt.ErrTokenExpired) {
		return nil, false
	}
	if token == nil {
		return nil, false
	}
	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, false
	}
	if err == nil && !token.Valid {
		return nil, false
	}
	if claims.Issuer != "" && claims.Issuer != IssuerTorchwood {
		return nil, false
	}
	return claims, true
}

// Generate creates a new signed JWT.
func Generate(secret []byte, claims Claims) (string, error) {
	now := time.Now()
	if claims.IssuedAt == 0 {
		claims.IssuedAt = now.Unix()
	}
	if claims.ExpiresAt == 0 {
		claims.ExpiresAt = now.Add(15 * time.Minute).Unix()
	}
	if claims.TokenID == "" {
		claims.TokenID = uuid.New().String()
	}
	if claims.Issuer == "" {
		claims.Issuer = IssuerTorchwood
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, &claims).SignedString(secret)
}
