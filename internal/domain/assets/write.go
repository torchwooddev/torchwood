package assets

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// Grant 发放资产（幂等键必填）。
func (s *Service) Grant(ctx context.Context, scope Scope, cmd GrantCommand) (*OpResult, error) {
	if err := s.requireScope(scope); err != nil {
		return nil, err
	}
	ownerType, key, err := s.prepareWrite(cmd.OwnerType, cmd.OwnerID, cmd.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	code, err := ValidateCode(cmd.DefCode)
	if err != nil {
		return nil, err
	}
	projectID := scope.ProjectID
	var result OpResult
	err = s.db.Run(ctx, func(txCtx context.Context) error {
		if replay, ok, err := s.loadReplay(txCtx, projectID, key); err != nil {
			return err
		} else if ok {
			result = OpResult{Entries: replay, IdempotentReplay: true}
			return nil
		}
		def, err := s.requireActiveDef(txCtx, projectID, code)
		if err != nil {
			return err
		}
		expiresAt, err := s.resolveGrantExpiry(def, cmd.ExpiresAt)
		if err != nil {
			return err
		}
		if err := ValidateGrant(def.Class, cmd.Quantity, expiresAt != nil); err != nil {
			return err
		}
		holdings, err := s.holdings.ListForUpdate(txCtx, projectID, ownerType, cmd.OwnerID, def.ID)
		if err != nil {
			return err
		}
		now := s.ts()
		live := liveHoldings(holdings, now)
		if def.Class != ClassCurrency && def.NaturalUniquePerOwner() && len(live) > 0 {
			return ErrUniquePerOwner
		}
		if err := checkMaxQuantity(def, live, cmd.Quantity); err != nil {
			return err
		}

		refType, refID := s.normalizeRef(cmd.RefType, cmd.RefID, "grant")
		h, created, err := s.upsertGrantHolding(def, projectID, ownerType, cmd, expiresAt, live, now)
		if err != nil {
			return err
		}
		entry := s.newEntry(LedgerEntry{
			ProjectID:      projectID,
			HoldingID:      h.ID,
			OwnerType:      ownerType,
			OwnerID:        cmd.OwnerID,
			DefID:          def.ID,
			Kind:           KindGrant,
			Delta:          cmd.Quantity,
			QuantityAfter:  h.Quantity,
			ExpiresAt:      h.ExpiresAt,
			BucketKey:      h.BucketKey,
			RefType:        refType,
			RefID:          refID,
			IdempotencyKey: key,
			CreatedAt:      now,
		}, scope.Operator)
		if _, inserted, err := s.ledger.InsertIfAbsent(txCtx, entry); err != nil {
			return err
		} else if !inserted {
			replay, _, err := s.loadReplay(txCtx, projectID, key)
			if err != nil {
				return err
			}
			result = OpResult{Entries: replay, IdempotentReplay: true}
			return nil
		}
		if created {
			if err := s.holdings.Insert(txCtx, h); err != nil {
				return err
			}
		} else {
			expect := h.Version - 1
			if err := s.holdings.Update(txCtx, h, expect); err != nil {
				return err
			}
		}
		if err := s.publish(txCtx, def, cmd.OwnerID, KindGrant, cmd.Quantity, h.Quantity, now); err != nil {
			return err
		}
		result = OpResult{Entries: []LedgerEntry{*entry}}
		return nil
	})
	return &result, err
}

func (s *Service) upsertGrantHolding(
	def *Def,
	projectID string,
	ownerType OwnerType,
	cmd GrantCommand,
	expiresAt *time.Time,
	live []Holding,
	now time.Time,
) (*Holding, bool, error) {
	if def.InstanceBucket() {
		id := s.newID()
		h := &Holding{
			ID:        id,
			ProjectID: projectID,
			OwnerType: ownerType,
			OwnerID:   cmd.OwnerID,
			DefID:     def.ID,
			Quantity:  1,
			ExpiresAt: expiresAt,
			Level:     cmd.Level,
			Metadata:  cmd.Metadata,
			BucketKey: id,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
		}
		return h, true, nil
	}
	for i := range live {
		h := &live[i]
		if sameExpiry(h.ExpiresAt, expiresAt) && h.BucketKey == "" {
			h.Quantity += cmd.Quantity
			h.Version++
			h.UpdatedAt = now
			return h, false, nil
		}
	}
	h := &Holding{
		ID:        s.newID(),
		ProjectID: projectID,
		OwnerType: ownerType,
		OwnerID:   cmd.OwnerID,
		DefID:     def.ID,
		Quantity:  cmd.Quantity,
		ExpiresAt: expiresAt,
		Level:     cmd.Level,
		Metadata:  cmd.Metadata,
		BucketKey: "",
		Version:   1,
		CreatedAt: now,
		UpdatedAt: now,
	}
	return h, true, nil
}

// Consume 按 FEFO 扣桶；数量不足整体失败。
func (s *Service) Consume(ctx context.Context, scope Scope, cmd ConsumeCommand) (*OpResult, error) {
	if err := s.requireScope(scope); err != nil {
		return nil, err
	}
	ownerType, key, err := s.prepareWrite(cmd.OwnerType, cmd.OwnerID, cmd.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	code, err := ValidateCode(cmd.DefCode)
	if err != nil {
		return nil, err
	}
	projectID := scope.ProjectID
	var result OpResult
	err = s.db.Run(ctx, func(txCtx context.Context) error {
		if replay, ok, err := s.loadReplay(txCtx, projectID, key); err != nil {
			return err
		} else if ok {
			result = OpResult{Entries: replay, IdempotentReplay: true}
			return nil
		}
		def, err := s.requireActiveDef(txCtx, projectID, code)
		if err != nil {
			return err
		}
		if err := ValidateConsumeQuantity(def.Class, cmd.Quantity); err != nil {
			return err
		}
		holdings, err := s.holdings.ListForUpdate(txCtx, projectID, ownerType, cmd.OwnerID, def.ID)
		if err != nil {
			return err
		}
		now := s.ts()
		live := liveHoldings(holdings, now)
		var avail int64
		for i := range live {
			avail += live[i].Quantity
		}
		if avail < cmd.Quantity {
			return fmt.Errorf("%w: have %d, want %d", ErrInsufficient, avail, cmd.Quantity)
		}
		refType, refID := s.normalizeRef(cmd.RefType, cmd.RefID, string(KindConsume))
		remain := cmd.Quantity
		var entries []LedgerEntry
		remainingTotal := avail - cmd.Quantity
		for i := range live {
			if remain == 0 {
				break
			}
			h := &live[i]
			take := h.Quantity
			if take > remain {
				take = remain
			}
			remain -= take
			after := h.Quantity - take
			idem := key
			if len(entries) > 0 {
				idem = key + "#" + strconv.Itoa(len(entries))
			}
			entry := s.newEntry(LedgerEntry{
				ProjectID:      projectID,
				HoldingID:      h.ID,
				OwnerType:      ownerType,
				OwnerID:        cmd.OwnerID,
				DefID:          def.ID,
				Kind:           KindConsume,
				Delta:          -take,
				QuantityAfter:  after,
				ExpiresAt:      h.ExpiresAt,
				BucketKey:      h.BucketKey,
				RefType:        refType,
				RefID:          refID,
				IdempotencyKey: idem,
				CreatedAt:      now,
			}, scope.Operator)
			if _, inserted, err := s.ledger.InsertIfAbsent(txCtx, entry); err != nil {
				return err
			} else if !inserted && len(entries) == 0 {
				replay, _, err := s.loadReplay(txCtx, projectID, key)
				if err != nil {
					return err
				}
				result = OpResult{Entries: replay, IdempotentReplay: true}
				return nil
			}
			entries = append(entries, *entry)
			expect := h.Version
			if after == 0 {
				if err := s.holdings.Delete(txCtx, projectID, h.ID, expect); err != nil {
					return err
				}
			} else {
				h.Quantity = after
				h.Version++
				h.UpdatedAt = now
				if err := s.holdings.Update(txCtx, h, expect); err != nil {
					return err
				}
			}
		}
		if err := s.publish(txCtx, def, cmd.OwnerID, KindConsume, -cmd.Quantity, remainingTotal, now); err != nil {
			return err
		}
		result = OpResult{Entries: entries}
		return nil
	})
	return &result, err
}

// Transfer 原子转让（仅 tradable 定义；entitlement 禁止）。
func (s *Service) Transfer(ctx context.Context, scope Scope, cmd TransferCommand) (*OpResult, error) {
	if err := s.requireScope(scope); err != nil {
		return nil, err
	}
	key, err := ValidateIdempotencyKey(cmd.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if cmd.FromOwnerID == "" || cmd.ToOwnerID == "" {
		return nil, ErrTransferOwnersRequired
	}
	if cmd.FromOwnerID == cmd.ToOwnerID {
		return nil, ErrSameOwner
	}
	code, err := ValidateCode(cmd.DefCode)
	if err != nil {
		return nil, err
	}
	projectID := scope.ProjectID
	var result OpResult
	err = s.db.Run(ctx, func(txCtx context.Context) error {
		if replay, ok, err := s.loadReplay(txCtx, projectID, key); err != nil {
			return err
		} else if ok {
			result = OpResult{Entries: replay, IdempotentReplay: true}
			return nil
		}
		def, err := s.requireActiveDef(txCtx, projectID, code)
		if err != nil {
			return err
		}
		if !def.Tradable || def.Class == ClassEntitlement {
			return ErrNotTradable
		}
		if err := ValidateConsumeQuantity(def.Class, cmd.Quantity); err != nil {
			return err
		}
		now := s.ts()
		src, err := s.holdings.ListForUpdate(txCtx, projectID, OwnerTypeUser, cmd.FromOwnerID, def.ID)
		if err != nil {
			return err
		}
		dst, err := s.holdings.ListForUpdate(txCtx, projectID, OwnerTypeUser, cmd.ToOwnerID, def.ID)
		if err != nil {
			return err
		}
		liveSrc := liveHoldings(src, now)
		liveDst := liveHoldings(dst, now)
		var avail int64
		for i := range liveSrc {
			avail += liveSrc[i].Quantity
		}
		if avail < cmd.Quantity {
			return fmt.Errorf("%w: have %d, want %d", ErrInsufficient, avail, cmd.Quantity)
		}
		if err := checkMaxQuantity(def, liveDst, cmd.Quantity); err != nil {
			return err
		}
		if def.Class != ClassCurrency && def.NaturalUniquePerOwner() && len(liveDst) > 0 {
			return ErrUniquePerOwner
		}
		refType, refID := s.normalizeRef(cmd.RefType, cmd.RefID, "transfer")
		remain := cmd.Quantity
		var entries []LedgerEntry
		seq := 0
		nextKey := func(side string) string {
			if seq == 0 && side == "out" {
				seq++
				return key
			}
			k := key + ":" + side + ":" + strconv.Itoa(seq)
			seq++
			return k
		}
		var moves []incomingMove
		for i := range liveSrc {
			if remain == 0 {
				break
			}
			h := &liveSrc[i]
			take := h.Quantity
			if take > remain {
				take = remain
			}
			remain -= take
			after := h.Quantity - take
			entry := s.newEntry(LedgerEntry{
				ProjectID:      projectID,
				HoldingID:      h.ID,
				OwnerType:      OwnerTypeUser,
				OwnerID:        cmd.FromOwnerID,
				DefID:          def.ID,
				Kind:           KindTransferOut,
				Delta:          -take,
				QuantityAfter:  after,
				ExpiresAt:      h.ExpiresAt,
				BucketKey:      h.BucketKey,
				RefType:        refType,
				RefID:          refID,
				IdempotencyKey: nextKey("out"),
				CreatedAt:      now,
			}, scope.Operator)
			if _, inserted, err := s.ledger.InsertIfAbsent(txCtx, entry); err != nil {
				return err
			} else if !inserted && len(entries) == 0 {
				replay, _, err := s.loadReplay(txCtx, projectID, key)
				if err != nil {
					return err
				}
				result = OpResult{Entries: replay, IdempotentReplay: true}
				return nil
			}
			entries = append(entries, *entry)
			moves = append(moves, incomingMove{
				qty: take, expiresAt: h.ExpiresAt, level: h.Level,
				metadata: h.Metadata, bucketKey: h.BucketKey,
				instance: def.InstanceBucket(),
			})
			expect := h.Version
			if after == 0 {
				if err := s.holdings.Delete(txCtx, projectID, h.ID, expect); err != nil {
					return err
				}
			} else {
				h.Quantity = after
				h.Version++
				h.UpdatedAt = now
				if err := s.holdings.Update(txCtx, h, expect); err != nil {
					return err
				}
			}
		}
		var destAfter int64
		for _, m := range liveDst {
			destAfter += m.Quantity
		}
		destAfter += cmd.Quantity
		for _, mv := range moves {
			dh, created, err := s.applyIncoming(def, projectID, cmd.ToOwnerID, mv, liveDst, now)
			if err != nil {
				return err
			}
			inEntry := s.newEntry(LedgerEntry{
				ProjectID:      projectID,
				HoldingID:      dh.ID,
				OwnerType:      OwnerTypeUser,
				OwnerID:        cmd.ToOwnerID,
				DefID:          def.ID,
				Kind:           KindTransferIn,
				Delta:          mv.qty,
				QuantityAfter:  dh.Quantity,
				ExpiresAt:      dh.ExpiresAt,
				BucketKey:      dh.BucketKey,
				RefType:        refType,
				RefID:          refID,
				IdempotencyKey: nextKey("in"),
				CreatedAt:      now,
			}, scope.Operator)
			if _, _, err := s.ledger.InsertIfAbsent(txCtx, inEntry); err != nil {
				return err
			}
			entries = append(entries, *inEntry)
			if created {
				if err := s.holdings.Insert(txCtx, dh); err != nil {
					return err
				}
				liveDst = append(liveDst, *dh)
			} else {
				expect := dh.Version - 1
				if err := s.holdings.Update(txCtx, dh, expect); err != nil {
					return err
				}
				for i := range liveDst {
					if liveDst[i].ID == dh.ID {
						liveDst[i] = *dh
						break
					}
				}
			}
		}
		if err := s.publish(txCtx, def, cmd.FromOwnerID, KindTransferOut, -cmd.Quantity, avail-cmd.Quantity, now); err != nil {
			return err
		}
		if err := s.publish(txCtx, def, cmd.ToOwnerID, KindTransferIn, cmd.Quantity, destAfter, now); err != nil {
			return err
		}
		result = OpResult{Entries: entries}
		return nil
	})
	return &result, err
}

type incomingMove struct {
	qty       int64
	expiresAt *time.Time
	level     int32
	metadata  json.RawMessage
	bucketKey string
	instance  bool
}

func (s *Service) applyIncoming(
	def *Def,
	projectID, toOwner string,
	mv incomingMove,
	liveDst []Holding,
	now time.Time,
) (*Holding, bool, error) {
	if mv.instance {
		id := s.newID()
		return &Holding{
			ID: id, ProjectID: projectID, OwnerType: OwnerTypeUser,
			OwnerID: toOwner, DefID: def.ID, Quantity: 1,
			ExpiresAt: mv.expiresAt, Level: mv.level, Metadata: mv.metadata,
			BucketKey: id, Version: 1, CreatedAt: now, UpdatedAt: now,
		}, true, nil
	}
	for i := range liveDst {
		h := &liveDst[i]
		if sameExpiry(h.ExpiresAt, mv.expiresAt) && h.BucketKey == "" {
			h.Quantity += mv.qty
			h.Version++
			h.UpdatedAt = now
			return h, false, nil
		}
	}
	return &Holding{
		ID: s.newID(), ProjectID: projectID, OwnerType: OwnerTypeUser,
		OwnerID: toOwner, DefID: def.ID, Quantity: mv.qty,
		ExpiresAt: mv.expiresAt, Level: mv.level, Metadata: mv.metadata,
		BucketKey: "", Version: 1, CreatedAt: now, UpdatedAt: now,
	}, true, nil
}

// Mutate 变更实例/权益属性（level / metadata / expires_at）。
func (s *Service) Mutate(ctx context.Context, scope Scope, cmd MutateCommand) (*OpResult, error) {
	if err := s.requireScope(scope); err != nil {
		return nil, err
	}
	key, err := ValidateIdempotencyKey(cmd.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if cmd.HoldingID == "" {
		return nil, ErrHoldingIDRequired
	}
	projectID := scope.ProjectID
	var result OpResult
	err = s.db.Run(ctx, func(txCtx context.Context) error {
		if replay, ok, err := s.loadReplay(txCtx, projectID, key); err != nil {
			return err
		} else if ok {
			result = OpResult{Entries: replay, IdempotentReplay: true}
			return nil
		}
		h, err := s.holdings.GetByIDForUpdate(txCtx, projectID, cmd.HoldingID)
		if err != nil {
			return err
		}
		if h == nil {
			return ErrHoldingNotFound
		}
		def, err := s.defs.GetByIDForShare(txCtx, projectID, h.DefID)
		if err != nil {
			return err
		}
		if def == nil {
			return ErrDefNotFound
		}
		if err := ValidateMutateClass(def.Class); err != nil {
			return err
		}
		now := s.ts()
		if cmd.Level != nil {
			h.Level = *cmd.Level
		}
		if cmd.Metadata != nil {
			h.Metadata = cmd.Metadata
		}
		if cmd.ExpiresAt != nil {
			exp := normalizeExpiry(cmd.ExpiresAt)
			if def.Class == ClassEntitlement && exp == nil {
				return ErrExpiresAtRequired
			}
			h.ExpiresAt = exp
		}
		expect := h.Version
		h.Version++
		h.UpdatedAt = now
		refType, refID := s.normalizeRef(cmd.RefType, cmd.RefID, "mutate")
		entry := s.newEntry(LedgerEntry{
			ProjectID:      projectID,
			HoldingID:      h.ID,
			OwnerType:      h.OwnerType,
			OwnerID:        h.OwnerID,
			DefID:          h.DefID,
			Kind:           KindMutate,
			Delta:          0,
			QuantityAfter:  h.Quantity,
			ExpiresAt:      h.ExpiresAt,
			BucketKey:      h.BucketKey,
			RefType:        refType,
			RefID:          refID,
			IdempotencyKey: key,
			CreatedAt:      now,
		}, scope.Operator)
		if _, inserted, err := s.ledger.InsertIfAbsent(txCtx, entry); err != nil {
			return err
		} else if !inserted {
			replay, _, err := s.loadReplay(txCtx, projectID, key)
			if err != nil {
				return err
			}
			result = OpResult{Entries: replay, IdempotentReplay: true}
			return nil
		}
		if err := s.holdings.Update(txCtx, h, expect); err != nil {
			return err
		}
		if err := s.publish(txCtx, def, h.OwnerID, KindMutate, 0, h.Quantity, now); err != nil {
			return err
		}
		result = OpResult{Entries: []LedgerEntry{*entry}}
		return nil
	})
	return &result, err
}

// Expire 强制失效一行持有（删行 + expire 流水）。
func (s *Service) Expire(ctx context.Context, scope Scope, cmd ExpireCommand) (*OpResult, error) {
	if err := s.requireScope(scope); err != nil {
		return nil, err
	}
	key, err := ValidateIdempotencyKey(cmd.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if cmd.HoldingID == "" {
		return nil, ErrHoldingIDRequired
	}
	var result OpResult
	err = s.db.Run(ctx, func(txCtx context.Context) error {
		res, err := s.expireHolding(txCtx, scope, scope.ProjectID, cmd.HoldingID, key)
		if err != nil {
			return err
		}
		result = *res
		return nil
	})
	return &result, err
}
