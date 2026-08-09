package client

import (
	"context"
	"time"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/pkg/jwtparser"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const oneTimeJWTTTL = 5 * time.Minute

// CreateJWT 用当前会话换取 5 分钟 TTL 的一次性 JWT（与 end-user token 同一
// 派生 key，validator 可直接验证）。
func (a *Account) CreateJWT(ctx context.Context) (string, error) {
	p, err := a.requireUser(ctx)
	if err != nil {
		return "", err
	}
	doc, err := a.docDB.GetDocument(ctx, p.ProjectID, "default", "users", p.UserID, databases.SystemPrincipal)
	if err != nil {
		return "", err
	}
	if doc == nil {
		return "", status.Error(codes.NotFound, "user not found")
	}
	email := stringValue(doc.Data["email"])

	var roles []string
	if a.roles != nil {
		roles, err = a.roles.LoadUserRoles(ctx, p.ProjectID, p.UserID)
		if err != nil {
			return "", err
		}
	}
	now := time.Now()
	claims := jwtparser.Claims{
		UserID:    p.UserID,
		Username:  email,
		ActorKind: "end_user",
		ProjectID: p.ProjectID,
		TokenType: jwtparser.TokenTypeAccess,
		Roles:     roles,
		ExpiresAt: now.Add(oneTimeJWTTTL).Unix(),
		IssuedAt:  now.Unix(),
	}
	key := jwtparser.DeriveKey(a.cfg.GetSecurity().GetJwt().GetSecret(), jwtparser.PurposeEndUserJWT)
	token, err := jwtparser.Generate(key, claims)
	if err != nil {
		return "", status.Error(codes.Internal, "jwt generation failed")
	}
	return token, nil
}
