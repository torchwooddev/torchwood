package assets

import (
	"context"
	"time"
)

// ExpireDue 扫描到期持有：产 expire 流水并删行（worker 周期任务）。
// 每行独立短事务；SKIP LOCKED 保证多副本互不阻塞。全局预算 expireBatch
// （K22）；项目遍历按轮转游标起始（队尾饥饿防护）。单项目不变式在领域 Service。
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
	n := len(all)
	start := a.scanCursor.Start(n)
	remaining := expireBatch
	var expired int64
	stopped := -1
	for i := 0; i < n; i++ {
		idx := (start + i) % n
		if remaining <= 0 {
			stopped = idx
			break
		}
		if all[idx].Status != "active" {
			continue
		}
		c, err := a.expireProject(ctx, all[idx].ID, now, remaining)
		if err != nil {
			a.logger.Error("expire holdings failed", "project_id", all[idx].ID, "error", err)
			continue
		}
		expired += c
		remaining -= int(c)
	}
	if stopped >= 0 {
		a.scanCursor.ResumeAt(stopped)
	} else {
		a.scanCursor.Complete()
	}
	return expired, nil
}

func (a *Assets) expireProject(ctx context.Context, projectID string, now time.Time, limit int) (int64, error) {
	ctx = withSystemPrincipal(ctx, projectID)
	if err := requireAssetWrite(ctx); err != nil {
		return 0, err
	}
	scope, err := a.writeScope(ctx)
	if err != nil {
		return 0, err
	}
	return a.svc.ExpireDue(ctx, scope, now, limit)
}
