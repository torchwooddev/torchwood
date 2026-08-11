package client

import (
	"context"

	clientv1 "github.com/torchwooddev/torchwood/genproto/client/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// AccountService 封装 Client API 的 Account 服务。
type AccountService struct{ c *Client }

// SignUp 使用邮箱/密码注册新账户；成功（非 MFA 分支）后自动保存 token。
func (a *AccountService) SignUp(ctx context.Context, email, password, name string) (*clientv1.SignUpResponse, error) {
	resp, err := a.c.account.SignUp(ctx, &clientv1.SignUpRequest{
		Email:     email,
		Password:  password,
		Name:      name,
		ProjectId: a.c.cfg.ProjectID,
	})
	if err != nil {
		return nil, err
	}
	if !resp.MfaRequired && resp.Tokens != nil && resp.Tokens.AccessToken != "" {
		if err := a.c.saveTokens(resp.Tokens); err != nil {
			return nil, err
		}
	}
	return resp, nil
}

// SignIn 使用邮箱/密码登录；成功（非 MFA 分支）后自动保存 token。
func (a *AccountService) SignIn(ctx context.Context, email, password string) (*clientv1.SignInResponse, error) {
	resp, err := a.c.account.SignIn(ctx, &clientv1.SignInRequest{
		Email:     email,
		Password:  password,
		ProjectId: a.c.cfg.ProjectID,
	})
	if err != nil {
		return nil, err
	}
	if !resp.MfaRequired && resp.Tokens != nil && resp.Tokens.AccessToken != "" {
		if err := a.c.saveTokens(resp.Tokens); err != nil {
			return nil, err
		}
	}
	return resp, nil
}

// RefreshToken 用刷新令牌换取新令牌并保存。
func (a *AccountService) RefreshToken(ctx context.Context, refreshToken string) (*clientv1.RefreshTokenResponse, error) {
	resp, err := a.c.account.RefreshToken(ctx, &clientv1.RefreshTokenRequest{
		ProjectId:    a.c.cfg.ProjectID,
		RefreshToken: refreshToken,
	})
	if err != nil {
		return nil, err
	}
	if resp.Tokens != nil && resp.Tokens.AccessToken != "" {
		if err := a.c.saveTokens(resp.Tokens); err != nil {
			return nil, err
		}
	}
	return resp, nil
}

// Me 返回当前登录账户信息。
func (a *AccountService) Me(ctx context.Context) (*clientv1.Account, error) {
	return a.c.account.Me(ctx, &clientv1.MeRequest{ProjectId: a.c.cfg.ProjectID})
}

// SignOut 注销当前会话；成功或 token 已失效（Unauthenticated）都清空本地 token。
func (a *AccountService) SignOut(ctx context.Context) error {
	_, err := a.c.account.SignOut(ctx, &clientv1.SignOutRequest{ProjectId: a.c.cfg.ProjectID})
	if err == nil || status.Code(err) == codes.Unauthenticated {
		a.c.clearTokens()
	}
	return err
}
