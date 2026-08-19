package assets

import (
	"context"

	domainassets "github.com/torchwooddev/torchwood/internal/domain/assets"
)

// LiveHoldingForUpdate 返回业主某定义下未过期的第一行持有（FOR UPDATE）。
// 供订阅 entitlement 续期：已有持有则 Mutate，否则 Grant。须在外层事务内调用。
func (a *Assets) LiveHoldingForUpdate(ctx context.Context, ownerID, defCode string) (*domainassets.Holding, error) {
	if err := requireAssetWrite(ctx); err != nil {
		return nil, err
	}
	projectID, ownerType, _, err := a.prepareWrite(ctx, domainassets.OwnerTypeUser, ownerID, "lookup:"+defCode)
	if err != nil {
		return nil, err
	}
	code, err := validateCode(defCode)
	if err != nil {
		return nil, err
	}
	def, err := a.requireActiveDef(ctx, projectID, code)
	if err != nil {
		return nil, err
	}
	holdings, err := a.holdings.ListForUpdate(ctx, projectID, ownerType, ownerID, def.ID)
	if err != nil {
		return nil, err
	}
	live := liveHoldings(holdings, a.ts())
	if len(live) == 0 {
		return nil, nil
	}
	h := live[0]
	return &h, nil
}
