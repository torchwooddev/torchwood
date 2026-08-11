package torchwood

import (
	"context"

	clientv1 "github.com/torchwooddev/torchwood/genproto/client/v1"
)

// AccountService 封装 Client API 的 Account 服务。
type AccountService struct{ c *Client }

// SignUp 使用邮箱/密码注册新账户，返回账户与令牌。
// ProjectID 取客户端配置（[WithProjectID]）。
func (a *AccountService) SignUp(ctx context.Context, email, password, name string) (*clientv1.SignUpResponse, error) {
	return a.c.account.SignUp(a.c.AuthContext(ctx), &clientv1.SignUpRequest{
		Email:     email,
		Password:  password,
		Name:      name,
		ProjectId: a.c.cfg.ProjectID,
	})
}

// SignIn 使用邮箱/密码登录，返回账户与令牌。
func (a *AccountService) SignIn(ctx context.Context, email, password string) (*clientv1.SignInResponse, error) {
	return a.c.account.SignIn(a.c.AuthContext(ctx), &clientv1.SignInRequest{
		Email:     email,
		Password:  password,
		ProjectId: a.c.cfg.ProjectID,
	})
}

// RefreshToken 用刷新令牌换取新的访问令牌。
func (a *AccountService) RefreshToken(ctx context.Context, refreshToken string) (*clientv1.RefreshTokenResponse, error) {
	return a.c.account.RefreshToken(a.c.AuthContext(ctx), &clientv1.RefreshTokenRequest{
		ProjectId:    a.c.cfg.ProjectID,
		RefreshToken: refreshToken,
	})
}

// Me 返回当前登录账户信息。
func (a *AccountService) Me(ctx context.Context) (*clientv1.Account, error) {
	return a.c.account.Me(a.c.AuthContext(ctx), &clientv1.MeRequest{
		ProjectId: a.c.cfg.ProjectID,
	})
}

// SignOut 注销当前会话。
func (a *AccountService) SignOut(ctx context.Context) error {
	_, err := a.c.account.SignOut(a.c.AuthContext(ctx), &clientv1.SignOutRequest{
		ProjectId: a.c.cfg.ProjectID,
	})
	return err
}
