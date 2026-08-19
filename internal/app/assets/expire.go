package assets

import (
	"context"
	"time"

	domainassets "github.com/torchwooddev/torchwood/internal/domain/assets"
)

// ExpireDue 扫描到期持有：产 expire 流水并删行（worker 周期任务）。
// 每行独立短事务；SKIP LOCKED 保证多副本互不阻塞。
func (a *Assets) ExpireDue(ctx context.Context, now time.Time) (int64, error) {
	if now.IsZero() {
		now = a.ts()
	}
	ctx = withSystemPrincipal(ctx, "")
	if err := requireAssetWrite(ctx); err != nil {
		return 0, err
	}
	var expired int64
	for {
		var batch []domainassets.Holding
		err := a.db.RunInTx(ctx, func(txCtx context.Context) error {
			var err error
			batch, err = a.holdings.ListExpired(txCtx, now, expireBatch)
			if err != nil {
				return err
			}
			for i := range batch {
				h := &batch[i]
				txCtx = withSystemPrincipal(txCtx, h.ProjectID)
				key := "expire:" + h.ID
				if h.ExpiresAt != nil {
					key += ":" + h.ExpiresAt.UTC().Format(time.RFC3339Nano)
				}
				if _, err := a.expireHolding(txCtx, h.ProjectID, h.ID, key); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return expired, err
		}
		expired += int64(len(batch))
		if len(batch) < expireBatch {
			return expired, nil
		}
	}
}
