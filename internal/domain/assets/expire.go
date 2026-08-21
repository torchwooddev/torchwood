package assets

import (
	"context"
	"time"
)

// ExpireDue 扫描并失效一个项目内到期持有（与 Expire 同一引擎）。
// 每批在同一短事务内；SKIP LOCKED 由仓储 ListExpiredInProject 保证。
func (s *Service) ExpireDue(ctx context.Context, scope Scope, now time.Time, limit int) (int64, error) {
	if err := s.requireScope(scope); err != nil {
		return 0, err
	}
	if now.IsZero() {
		now = s.ts()
	}
	var n int64
	err := s.db.RunInTx(ctx, func(txCtx context.Context) error {
		batch, err := s.holdings.ListExpiredInProject(txCtx, scope.ProjectID, now, limit)
		if err != nil {
			return err
		}
		for i := range batch {
			h := &batch[i]
			key := "expire:" + h.ID
			if h.ExpiresAt != nil {
				key += ":" + h.ExpiresAt.UTC().Format(time.RFC3339Nano)
			}
			if _, err := s.expireHolding(txCtx, scope, h.ProjectID, h.ID, key); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}

func (s *Service) expireHolding(ctx context.Context, scope Scope, projectID, holdingID, key string) (*OpResult, error) {
	if replay, ok, err := s.loadReplay(ctx, projectID, key); err != nil {
		return nil, err
	} else if ok {
		return &OpResult{Entries: replay, IdempotentReplay: true}, nil
	}
	h, err := s.holdings.GetByIDForUpdate(ctx, projectID, holdingID)
	if err != nil {
		return nil, err
	}
	if h == nil {
		return nil, ErrHoldingNotFound
	}
	def, err := s.defs.GetByIDForShare(ctx, projectID, h.DefID)
	if err != nil {
		return nil, err
	}
	if def == nil {
		return nil, ErrDefNotFound
	}
	now := s.ts()
	entry := s.newEntry(LedgerEntry{
		ProjectID:      projectID,
		HoldingID:      h.ID,
		OwnerType:      h.OwnerType,
		OwnerID:        h.OwnerID,
		DefID:          h.DefID,
		Kind:           KindExpire,
		Delta:          -h.Quantity,
		QuantityAfter:  0,
		ExpiresAt:      h.ExpiresAt,
		BucketKey:      h.BucketKey,
		RefType:        "system",
		RefID:          h.ID,
		IdempotencyKey: key,
		CreatedAt:      now,
	}, scope.Operator)
	if _, inserted, err := s.ledger.InsertIfAbsent(ctx, entry); err != nil {
		return nil, err
	} else if !inserted {
		replay, _, err := s.loadReplay(ctx, projectID, key)
		if err != nil {
			return nil, err
		}
		return &OpResult{Entries: replay, IdempotentReplay: true}, nil
	}
	if err := s.holdings.Delete(ctx, projectID, h.ID, h.Version); err != nil {
		return nil, err
	}
	if err := s.publish(ctx, def, h.OwnerID, KindExpire, -h.Quantity, 0, now); err != nil {
		return nil, err
	}
	return &OpResult{Entries: []LedgerEntry{*entry}}, nil
}
