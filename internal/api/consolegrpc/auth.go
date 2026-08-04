package consolegrpc

import (
	"context"

	consolev1 "github.com/deeploop-ai/graviton/genproto/console/v1"
	sharedv1 "github.com/deeploop-ai/graviton/genproto/shared/v1"
	"github.com/deeploop-ai/graviton/internal/app/console"
)

type AuthService struct {
	consolev1.UnimplementedConsoleAuthServiceServer
	auth *console.Auth
}

func NewAuthService(auth *console.Auth) *AuthService {
	return &AuthService{auth: auth}
}

func (s *AuthService) SignIn(ctx context.Context, req *consolev1.SignInRequest) (*consolev1.SignInResponse, error) {
	tokens, err := s.auth.SignIn(ctx, console.SignInCommand{
		Email:    req.GetEmail(),
		Password: req.GetPassword(),
	})
	if err != nil {
		return nil, err
	}
	setSessionCookies(ctx, s.auth, tokens)
	return mapSignInResponse(tokens), nil
}

func (s *AuthService) RefreshToken(ctx context.Context, req *consolev1.RefreshTokenRequest) (*consolev1.SignInResponse, error) {
	refreshToken := req.GetRefreshToken()
	if refreshToken == "" {
		// Cookie-only 浏览器流：refresh token 由 HttpOnly cookie 携带。
		refreshToken = refreshTokenFromCookie(ctx)
	}
	tokens, err := s.auth.RefreshToken(ctx, console.RefreshTokenCommand{
		RefreshToken: refreshToken,
	})
	if err != nil {
		return nil, err
	}
	setSessionCookies(ctx, s.auth, tokens)
	return mapSignInResponse(tokens), nil
}

func (s *AuthService) SignOut(ctx context.Context, _ *consolev1.SignOutRequest) (*sharedv1.Empty, error) {
	if err := s.auth.SignOut(ctx); err != nil {
		return nil, err
	}
	clearSessionCookies(ctx, s.auth)
	return &sharedv1.Empty{}, nil
}

func mapSignInResponse(tokens *console.TokenPair) *consolev1.SignInResponse {
	if tokens == nil {
		return &consolev1.SignInResponse{}
	}
	return &consolev1.SignInResponse{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		ExpiresAt:    tokens.ExpiresAt,
	}
}
