package assets

import (
	"context"
	"time"
)

// ExpireDue 扫描到期持有：产 expire 流水并删行（worker 周期任务）。
// 每行独立短事务；SKIP LOCKED 保证多副本互不阻塞。
func (a *Assets) ExpireDue(ctx context.Context, now time.Time) (int64, error) {
	if now.IsZero() {
		now = a.ts()
	}
	if a.projects == nil {
		return 0, nil
	}
	all, err := a.projects.ListProjects(ctx)
	if err != nil {
		return 0, err
	}
	remaining := expireBatch
	var expired int64
	for i := range all {
		if remaining <= 0 {
			break
		}
		if all[i].Status != "active" {
			continue
		}
		n, err := a.expireProject(ctx, all[i].ID, now, remaining)
		if err != nil {
			a.logger.Error("expire holdings failed", "project_id", all[i].ID, "error", err)
			continue
		}
		expired += n
		remaining -= int(n)
	}
	return expired, nil
}

func (a *Assets) expireProject(ctx context.Context, projectID string, now time.Time, limit int) (int64, error) {
	var n int64
	err := a.db.RunInTx(ctx, func(txCtx context.Context) error {
		txCtx = withSystemPrincipal(txCtx, projectID)
		if err := requireAssetWrite(txCtx); err != nil {
			return err
		}
		batch, err := a.holdings.ListExpiredInProject(txCtx, projectID, now, limit)
		if err != nil {
			return err
		}
		for i := range batch {
			h := &batch[i]
			key := "expire:" + h.ID
			if h.ExpiresAt != nil {
				key += ":" + h.ExpiresAt.UTC().Format(time.RFC3339Nano)
			}
			if _, err := a.expireHolding(txCtx, h.ProjectID, h.ID, key); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}
