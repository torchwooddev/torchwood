package client

import (
	"context"

	clientv1 "github.com/torchwooddev/torchwood/genproto/client/v1"
)

// AccountService 封装 Client API 的 Account 服务。
type AccountService struct{ c *Client }

// Me 返回当前登录账户信息。
func (a *AccountService) Me(ctx context.Context) (*clientv1.Account, error) {
	return a.c.account.Me(ctx, &clientv1.MeRequest{ProjectId: a.c.cfg.ProjectID})
}
