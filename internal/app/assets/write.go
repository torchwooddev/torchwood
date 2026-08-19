package assets

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	domainevents "github.com/torchwooddev/torchwood/internal/domain/events"
	domainassets "github.com/torchwooddev/torchwood/internal/domain/assets"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// OpResult 是五动词的统一返回：流水（可能多行）+ 是否幂等重放。
type OpResult struct {
	Entries          []domainassets.LedgerEntry
	IdempotentReplay bool
}

// GrantCommand 是发放入参。
type GrantCommand struct {
	OwnerType      domainassets.OwnerType
	OwnerID        string
	DefCode        string
	Quantity       int64
	ExpiresAt      *time.Time
	Level          int32
	Metadata       json.RawMessage
	IdempotencyKey string
	RefType        string
	RefID          string
}

// ConsumeCommand 是消耗入参（FEFO）。
type ConsumeCommand struct {
	OwnerType      domainassets.OwnerType
	OwnerID        string
	DefCode        string
	Quantity       int64
	IdempotencyKey string
	RefType        string
	RefID          string
}

// TransferCommand 是转让入参（原子：transfer_out + transfer_in 共享 ref_id）。
type TransferCommand struct {
	FromOwnerID    string
	ToOwnerID      string
	DefCode        string
	Quantity       int64
	IdempotencyKey string
	RefType        string
	RefID          string
}

// MutateCommand 是实例/权益属性变更入参。
type MutateCommand struct {
	HoldingID      string
	Level          *int32
	ExpiresAt      *time.Time
	Metadata       json.RawMessage
	IdempotencyKey string
	RefType        string
	RefID          string
}

// ExpireCommand 是单行过期/强制失效入参。
type ExpireCommand struct {
	HoldingID      string
	IdempotencyKey string
}

// Grant 发放资产（幂等键必填）。
func (a *Assets) Grant(ctx context.Context, cmd GrantCommand) (*OpResult, error) {
	if err := requireAssetWrite(ctx); err != nil {
		return nil, err
	}
	return a.grant(ctx, cmd)
}

func (a *Assets) grant(ctx context.Context, cmd GrantCommand) (*OpResult, error) {
	projectID, ownerType, key, err := a.prepareWrite(ctx, cmd.OwnerType, cmd.OwnerID, cmd.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	code, err := validateCode(cmd.DefCode)
	if err != nil {
		return nil, err
	}
	var result OpResult
	err = a.db.RunInTx(ctx, func(txCtx context.Context) error {
		if replay, ok, err := a.loadReplay(txCtx, projectID, key); err != nil {
			return err
		} else if ok {
			result = OpResult{Entries: replay, IdempotentReplay: true}
			return nil
		}
		def, err := a.requireActiveDef(txCtx, projectID, code)
		if err != nil {
			return err
		}
		expiresAt, err := a.resolveGrantExpiry(def, cmd.ExpiresAt)
		if err != nil {
			return err
		}
		if err := domainassets.ValidateGrant(def.Class, cmd.Quantity, expiresAt != nil); err != nil {
			return err
		}
		holdings, err := a.holdings.ListForUpdate(txCtx, projectID, ownerType, cmd.OwnerID, def.ID)
		if err != nil {
			return err
		}
		now := a.ts()
		live := liveHoldings(holdings, now)
		if def.Class != domainassets.ClassCurrency && def.NaturalUniquePerOwner() && len(live) > 0 {
			return domainassets.ErrUniquePerOwner
		}
		if err := checkMaxQuantity(def, live, cmd.Quantity); err != nil {
			return err
		}

		refType, refID := normalizeRef(cmd.RefType, cmd.RefID, "grant")
		h, created, err := a.upsertGrantHolding(txCtx, def, projectID, ownerType, cmd, expiresAt, live, now)
		if err != nil {
			return err
		}
		entry := a.newEntry(txCtx, domainassets.LedgerEntry{
			ProjectID:      projectID,
			HoldingID:      h.ID,
			OwnerType:      ownerType,
			OwnerID:        cmd.OwnerID,
			DefID:          def.ID,
			Kind:           domainassets.KindGrant,
			Delta:          cmd.Quantity,
			QuantityAfter:  h.Quantity,
			ExpiresAt:      h.ExpiresAt,
			BucketKey:      h.BucketKey,
			RefType:        refType,
			RefID:          refID,
			IdempotencyKey: key,
			CreatedAt:      now,
		})
		if _, inserted, err := a.ledger.InsertIfAbsent(txCtx, entry); err != nil {
			return err
		} else if !inserted {
			replay, _, err := a.loadReplay(txCtx, projectID, key)
			if err != nil {
				return err
			}
			result = OpResult{Entries: replay, IdempotentReplay: true}
			return nil
		}
		if created {
			if err := a.holdings.Insert(txCtx, h); err != nil {
				return err
			}
		} else {
			expect := h.Version - 1
			if err := a.holdings.Update(txCtx, h, expect); err != nil {
				return err
			}
		}
		if err := a.publish(txCtx, def, cmd.OwnerID, domainassets.KindGrant, cmd.Quantity, h.Quantity, now); err != nil {
			return err
		}
		result = OpResult{Entries: []domainassets.LedgerEntry{*entry}}
		return nil
	})
	a.observe(domainassets.KindGrant, cmd.DefCode, err, result.IdempotentReplay)
	return &result, mapWriteError(err)
}

func (a *Assets) upsertGrantHolding(
	ctx context.Context,
	def *domainassets.Def,
	projectID string,
	ownerType domainassets.OwnerType,
	cmd GrantCommand,
	expiresAt *time.Time,
	live []domainassets.Holding,
	now time.Time,
) (*domainassets.Holding, bool, error) {
	if def.InstanceBucket() {
		id := newID()
		h := &domainassets.Holding{
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
	h := &domainassets.Holding{
		ID:        newID(),
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
func (a *Assets) Consume(ctx context.Context, cmd ConsumeCommand) (*OpResult, error) {
	if err := requireAssetWrite(ctx); err != nil {
		return nil, err
	}
	return a.consume(ctx, cmd, domainassets.KindConsume, "")
}

func (a *Assets) consume(ctx context.Context, cmd ConsumeCommand, kind domainassets.EntryKind, keySuffix string) (*OpResult, error) {
	projectID, ownerType, key, err := a.prepareWrite(ctx, cmd.OwnerType, cmd.OwnerID, cmd.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if keySuffix != "" {
		key = key + keySuffix
	}
	code, err := validateCode(cmd.DefCode)
	if err != nil {
		return nil, err
	}
	var result OpResult
	err = a.db.RunInTx(ctx, func(txCtx context.Context) error {
		if replay, ok, err := a.loadReplay(txCtx, projectID, key); err != nil {
			return err
		} else if ok {
			result = OpResult{Entries: replay, IdempotentReplay: true}
			return nil
		}
		def, err := a.requireActiveDef(txCtx, projectID, code)
		if err != nil {
			return err
		}
		if err := domainassets.ValidateConsumeQuantity(def.Class, cmd.Quantity); err != nil {
			return err
		}
		holdings, err := a.holdings.ListForUpdate(txCtx, projectID, ownerType, cmd.OwnerID, def.ID)
		if err != nil {
			return err
		}
		now := a.ts()
		live := liveHoldings(holdings, now)
		var avail int64
		for i := range live {
			avail += live[i].Quantity
		}
		if avail < cmd.Quantity {
			assetNegativeBlockedTotal.Inc()
			return fmt.Errorf("%w: have %d, want %d", domainassets.ErrInsufficient, avail, cmd.Quantity)
		}
		refType, refID := normalizeRef(cmd.RefType, cmd.RefID, string(kind))
		remain := cmd.Quantity
		var entries []domainassets.LedgerEntry
		var remainingTotal int64 = avail - cmd.Quantity
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
			entry := a.newEntry(txCtx, domainassets.LedgerEntry{
				ProjectID:      projectID,
				HoldingID:      h.ID,
				OwnerType:      ownerType,
				OwnerID:        cmd.OwnerID,
				DefID:          def.ID,
				Kind:           kind,
				Delta:          -take,
				QuantityAfter:  after,
				ExpiresAt:      h.ExpiresAt,
				BucketKey:      h.BucketKey,
				RefType:        refType,
				RefID:          refID,
				IdempotencyKey: idem,
				CreatedAt:      now,
			})
			if _, inserted, err := a.ledger.InsertIfAbsent(txCtx, entry); err != nil {
				return err
			} else if !inserted && len(entries) == 0 {
				replay, _, err := a.loadReplay(txCtx, projectID, key)
				if err != nil {
					return err
				}
				result = OpResult{Entries: replay, IdempotentReplay: true}
				return nil
			}
			entries = append(entries, *entry)
			expect := h.Version
			if after == 0 {
				if err := a.holdings.Delete(txCtx, projectID, h.ID, expect); err != nil {
					return err
				}
			} else {
				h.Quantity = after
				h.Version++
				h.UpdatedAt = now
				if err := a.holdings.Update(txCtx, h, expect); err != nil {
					return err
				}
			}
		}
		if err := a.publish(txCtx, def, cmd.OwnerID, kind, -cmd.Quantity, remainingTotal, now); err != nil {
			return err
		}
		result = OpResult{Entries: entries}
		return nil
	})
	a.observe(kind, cmd.DefCode, err, result.IdempotentReplay)
	return &result, mapWriteError(err)
}

// Transfer 原子转让（仅 tradable 定义；entitlement 禁止）。
func (a *Assets) Transfer(ctx context.Context, cmd TransferCommand) (*OpResult, error) {
	if err := requireAssetWrite(ctx); err != nil {
		return nil, err
	}
	projectID, err := projectScope(ctx)
	if err != nil {
		return nil, err
	}
	key, err := validateIdempotency(cmd.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if cmd.FromOwnerID == "" || cmd.ToOwnerID == "" {
		return nil, status.Error(codes.InvalidArgument, "from_owner_id and to_owner_id are required")
	}
	if cmd.FromOwnerID == cmd.ToOwnerID {
		return nil, status.Error(codes.InvalidArgument, "cannot transfer to the same owner")
	}
	code, err := validateCode(cmd.DefCode)
	if err != nil {
		return nil, err
	}
	var result OpResult
	err = a.db.RunInTx(ctx, func(txCtx context.Context) error {
		if replay, ok, err := a.loadReplay(txCtx, projectID, key); err != nil {
			return err
		} else if ok {
			result = OpResult{Entries: replay, IdempotentReplay: true}
			return nil
		}
		def, err := a.requireActiveDef(txCtx, projectID, code)
		if err != nil {
			return err
		}
		if !def.Tradable || def.Class == domainassets.ClassEntitlement {
			return domainassets.ErrNotTradable
		}
		if err := domainassets.ValidateConsumeQuantity(def.Class, cmd.Quantity); err != nil {
			return err
		}
		now := a.ts()
		src, err := a.holdings.ListForUpdate(txCtx, projectID, domainassets.OwnerTypeUser, cmd.FromOwnerID, def.ID)
		if err != nil {
			return err
		}
		dst, err := a.holdings.ListForUpdate(txCtx, projectID, domainassets.OwnerTypeUser, cmd.ToOwnerID, def.ID)
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
			assetNegativeBlockedTotal.Inc()
			return fmt.Errorf("%w: have %d, want %d", domainassets.ErrInsufficient, avail, cmd.Quantity)
		}
		if err := checkMaxQuantity(def, liveDst, cmd.Quantity); err != nil {
			return err
		}
		if def.Class != domainassets.ClassCurrency && def.NaturalUniquePerOwner() && len(liveDst) > 0 {
			return domainassets.ErrUniquePerOwner
		}
		refType, refID := normalizeRef(cmd.RefType, cmd.RefID, "transfer")
		remain := cmd.Quantity
		var entries []domainassets.LedgerEntry
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
			entry := a.newEntry(txCtx, domainassets.LedgerEntry{
				ProjectID:      projectID,
				HoldingID:      h.ID,
				OwnerType:      domainassets.OwnerTypeUser,
				OwnerID:        cmd.FromOwnerID,
				DefID:          def.ID,
				Kind:           domainassets.KindTransferOut,
				Delta:          -take,
				QuantityAfter:  after,
				ExpiresAt:      h.ExpiresAt,
				BucketKey:      h.BucketKey,
				RefType:        refType,
				RefID:          refID,
				IdempotencyKey: nextKey("out"),
				CreatedAt:      now,
			})
			if _, inserted, err := a.ledger.InsertIfAbsent(txCtx, entry); err != nil {
				return err
			} else if !inserted && len(entries) == 0 {
				replay, _, err := a.loadReplay(txCtx, projectID, key)
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
				if err := a.holdings.Delete(txCtx, projectID, h.ID, expect); err != nil {
					return err
				}
			} else {
				h.Quantity = after
				h.Version++
				h.UpdatedAt = now
				if err := a.holdings.Update(txCtx, h, expect); err != nil {
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
			dh, created, err := a.applyIncoming(txCtx, def, projectID, cmd.ToOwnerID, mv, liveDst, now)
			if err != nil {
				return err
			}
			inEntry := a.newEntry(txCtx, domainassets.LedgerEntry{
				ProjectID:      projectID,
				HoldingID:      dh.ID,
				OwnerType:      domainassets.OwnerTypeUser,
				OwnerID:        cmd.ToOwnerID,
				DefID:          def.ID,
				Kind:           domainassets.KindTransferIn,
				Delta:          mv.qty,
				QuantityAfter:  dh.Quantity,
				ExpiresAt:      dh.ExpiresAt,
				BucketKey:      dh.BucketKey,
				RefType:        refType,
				RefID:          refID,
				IdempotencyKey: nextKey("in"),
				CreatedAt:      now,
			})
			if _, _, err := a.ledger.InsertIfAbsent(txCtx, inEntry); err != nil {
				return err
			}
			entries = append(entries, *inEntry)
			if created {
				if err := a.holdings.Insert(txCtx, dh); err != nil {
					return err
				}
				liveDst = append(liveDst, *dh)
			} else {
				expect := dh.Version - 1
				if err := a.holdings.Update(txCtx, dh, expect); err != nil {
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
		if err := a.publish(txCtx, def, cmd.FromOwnerID, domainassets.KindTransferOut, -cmd.Quantity, avail-cmd.Quantity, now); err != nil {
			return err
		}
		if err := a.publish(txCtx, def, cmd.ToOwnerID, domainassets.KindTransferIn, cmd.Quantity, destAfter, now); err != nil {
			return err
		}
		result = OpResult{Entries: entries}
		return nil
	})
	a.observe(domainassets.KindTransferOut, cmd.DefCode, err, result.IdempotentReplay)
	return &result, mapWriteError(err)
}

type incomingMove struct {
	qty       int64
	expiresAt *time.Time
	level     int32
	metadata  json.RawMessage
	bucketKey string
	instance  bool
}

func (a *Assets) applyIncoming(
	_ context.Context,
	def *domainassets.Def,
	projectID, toOwner string,
	mv incomingMove,
	liveDst []domainassets.Holding,
	now time.Time,
) (*domainassets.Holding, bool, error) {
	if mv.instance {
		id := newID()
		return &domainassets.Holding{
			ID: id, ProjectID: projectID, OwnerType: domainassets.OwnerTypeUser,
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
	return &domainassets.Holding{
		ID: newID(), ProjectID: projectID, OwnerType: domainassets.OwnerTypeUser,
		OwnerID: toOwner, DefID: def.ID, Quantity: mv.qty,
		ExpiresAt: mv.expiresAt, Level: mv.level, Metadata: mv.metadata,
		BucketKey: "", Version: 1, CreatedAt: now, UpdatedAt: now,
	}, true, nil
}

// Mutate 变更实例/权益属性（level / metadata / expires_at）。
func (a *Assets) Mutate(ctx context.Context, cmd MutateCommand) (*OpResult, error) {
	if err := requireAssetWrite(ctx); err != nil {
		return nil, err
	}
	projectID, err := projectScope(ctx)
	if err != nil {
		return nil, err
	}
	key, err := validateIdempotency(cmd.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if cmd.HoldingID == "" {
		return nil, status.Error(codes.InvalidArgument, "holding_id is required")
	}
	var result OpResult
	err = a.db.RunInTx(ctx, func(txCtx context.Context) error {
		if replay, ok, err := a.loadReplay(txCtx, projectID, key); err != nil {
			return err
		} else if ok {
			result = OpResult{Entries: replay, IdempotentReplay: true}
			return nil
		}
		h, err := a.holdings.GetByIDForUpdate(txCtx, projectID, cmd.HoldingID)
		if err != nil {
			return err
		}
		if h == nil {
			return domainassets.ErrHoldingNotFound
		}
		def, err := a.defs.GetByIDForShare(txCtx, projectID, h.DefID)
		if err != nil {
			return err
		}
		if def == nil {
			return domainassets.ErrDefNotFound
		}
		if err := domainassets.ValidateMutateClass(def.Class); err != nil {
			return err
		}
		now := a.ts()
		if cmd.Level != nil {
			h.Level = *cmd.Level
		}
		if cmd.Metadata != nil {
			h.Metadata = cmd.Metadata
		}
		if cmd.ExpiresAt != nil {
			exp := normalizeExpiry(cmd.ExpiresAt)
			if def.Class == domainassets.ClassEntitlement && exp == nil {
				return domainassets.ErrExpiresAtRequired
			}
			h.ExpiresAt = exp
		}
		expect := h.Version
		h.Version++
		h.UpdatedAt = now
		refType, refID := normalizeRef(cmd.RefType, cmd.RefID, "mutate")
		entry := a.newEntry(txCtx, domainassets.LedgerEntry{
			ProjectID:      projectID,
			HoldingID:      h.ID,
			OwnerType:      h.OwnerType,
			OwnerID:        h.OwnerID,
			DefID:          h.DefID,
			Kind:           domainassets.KindMutate,
			Delta:          0,
			QuantityAfter:  h.Quantity,
			ExpiresAt:      h.ExpiresAt,
			BucketKey:      h.BucketKey,
			RefType:        refType,
			RefID:          refID,
			IdempotencyKey: key,
			CreatedAt:      now,
		})
		if _, inserted, err := a.ledger.InsertIfAbsent(txCtx, entry); err != nil {
			return err
		} else if !inserted {
			replay, _, err := a.loadReplay(txCtx, projectID, key)
			if err != nil {
				return err
			}
			result = OpResult{Entries: replay, IdempotentReplay: true}
			return nil
		}
		if err := a.holdings.Update(txCtx, h, expect); err != nil {
			return err
		}
		if err := a.publish(txCtx, def, h.OwnerID, domainassets.KindMutate, 0, h.Quantity, now); err != nil {
			return err
		}
		result = OpResult{Entries: []domainassets.LedgerEntry{*entry}}
		return nil
	})
	a.observe(domainassets.KindMutate, "", err, result.IdempotentReplay)
	return &result, mapWriteError(err)
}

// Expire 强制失效一行持有（删行 + expire 流水）。
func (a *Assets) Expire(ctx context.Context, cmd ExpireCommand) (*OpResult, error) {
	if err := requireAssetWrite(ctx); err != nil {
		return nil, err
	}
	projectID, err := projectScope(ctx)
	if err != nil {
		return nil, err
	}
	key, err := validateIdempotency(cmd.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if cmd.HoldingID == "" {
		return nil, status.Error(codes.InvalidArgument, "holding_id is required")
	}
	var result OpResult
	err = a.db.RunInTx(ctx, func(txCtx context.Context) error {
		res, err := a.expireHolding(txCtx, projectID, cmd.HoldingID, key)
		if err != nil {
			return err
		}
		result = *res
		return nil
	})
	a.observe(domainassets.KindExpire, "", err, result.IdempotentReplay)
	return &result, mapWriteError(err)
}

func (a *Assets) expireHolding(ctx context.Context, projectID, holdingID, key string) (*OpResult, error) {
	if replay, ok, err := a.loadReplay(ctx, projectID, key); err != nil {
		return nil, err
	} else if ok {
		return &OpResult{Entries: replay, IdempotentReplay: true}, nil
	}
	h, err := a.holdings.GetByIDForUpdate(ctx, projectID, holdingID)
	if err != nil {
		return nil, err
	}
	if h == nil {
		return nil, domainassets.ErrHoldingNotFound
	}
	def, err := a.defs.GetByIDForShare(ctx, projectID, h.DefID)
	if err != nil {
		return nil, err
	}
	if def == nil {
		return nil, domainassets.ErrDefNotFound
	}
	now := a.ts()
	entry := a.newEntry(ctx, domainassets.LedgerEntry{
		ProjectID:      projectID,
		HoldingID:      h.ID,
		OwnerType:      h.OwnerType,
		OwnerID:        h.OwnerID,
		DefID:          h.DefID,
		Kind:           domainassets.KindExpire,
		Delta:          -h.Quantity,
		QuantityAfter:  0,
		ExpiresAt:      h.ExpiresAt,
		BucketKey:      h.BucketKey,
		RefType:        "system",
		RefID:          h.ID,
		IdempotencyKey: key,
		CreatedAt:      now,
	})
	if _, inserted, err := a.ledger.InsertIfAbsent(ctx, entry); err != nil {
		return nil, err
	} else if !inserted {
		replay, _, err := a.loadReplay(ctx, projectID, key)
		if err != nil {
			return nil, err
		}
		return &OpResult{Entries: replay, IdempotentReplay: true}, nil
	}
	if err := a.holdings.Delete(ctx, projectID, h.ID, h.Version); err != nil {
		return nil, err
	}
	if err := a.publish(ctx, def, h.OwnerID, domainassets.KindExpire, -h.Quantity, 0, now); err != nil {
		return nil, err
	}
	return &OpResult{Entries: []domainassets.LedgerEntry{*entry}}, nil
}

func (a *Assets) prepareWrite(ctx context.Context, ownerType domainassets.OwnerType, ownerID, idem string) (string, domainassets.OwnerType, string, error) {
	projectID, err := projectScope(ctx)
	if err != nil {
		return "", "", "", err
	}
	ot, err := domainassets.NormalizeOwnerType(ownerType)
	if err != nil {
		return "", "", "", err
	}
	if ownerID == "" {
		return "", "", "", status.Error(codes.InvalidArgument, "owner_id is required")
	}
	key, err := validateIdempotency(idem)
	if err != nil {
		return "", "", "", err
	}
	return projectID, ot, key, nil
}

func (a *Assets) requireActiveDef(ctx context.Context, projectID, code string) (*domainassets.Def, error) {
	def, err := a.defs.GetByCodeForShare(ctx, projectID, code)
	if err != nil {
		return nil, err
	}
	if def == nil {
		return nil, domainassets.ErrDefNotFound
	}
	if def.Status != domainassets.DefStatusActive {
		return nil, domainassets.ErrDefArchived
	}
	return def, nil
}

func (a *Assets) resolveGrantExpiry(def *domainassets.Def, explicit *time.Time) (*time.Time, error) {
	if explicit != nil {
		if !def.AllowsExpiry() {
			return nil, fmt.Errorf("%w: currency grant must not set expires_at", domainassets.ErrMatrix)
		}
		return normalizeExpiry(explicit), nil
	}
	if def.ExpiresIn != nil && *def.ExpiresIn > 0 {
		if !def.AllowsExpiry() {
			return nil, fmt.Errorf("%w: currency must not have expires_in", domainassets.ErrMatrix)
		}
		t := a.ts().Add(time.Duration(*def.ExpiresIn) * time.Second)
		return normalizeExpiry(&t), nil
	}
	if def.RequiresExpiry() {
		return nil, domainassets.ErrExpiresAtRequired
	}
	return nil, nil
}

func (a *Assets) loadReplay(ctx context.Context, projectID, key string) ([]domainassets.LedgerEntry, bool, error) {
	first, err := a.ledger.GetByIdempotencyKey(ctx, projectID, key)
	if err != nil {
		return nil, false, err
	}
	if first == nil {
		return nil, false, nil
	}
	if first.RefID != "" {
		all, err := a.ledger.ListByRef(ctx, projectID, first.RefType, first.RefID)
		if err != nil {
			return nil, false, err
		}
		if len(all) > 0 {
			return all, true, nil
		}
	}
	return []domainassets.LedgerEntry{*first}, true, nil
}

func (a *Assets) newEntry(ctx context.Context, e domainassets.LedgerEntry) *domainassets.LedgerEntry {
	if e.ID == "" {
		e.ID = newID()
	}
	if e.Operator == nil {
		e.Operator = operatorFrom(ctx)
	}
	return &e
}

func (a *Assets) publish(ctx context.Context, def *domainassets.Def, ownerID string, kind domainassets.EntryKind, delta, after int64, now time.Time) error {
	if a.events == nil {
		return nil
	}
	attrs := map[string]any{
		"def_id":         def.ID,
		"def_code":       def.Code,
		"class":          string(def.Class),
		"owner_id":       ownerID,
		"kind":           string(kind),
		"delta":          delta,
		"quantity_after": after,
	}
	return a.events.Publish(ctx, domainevents.Envelope{
		EventID:   newID(),
		Event:     domainassets.EventNameForKind(kind),
		ProjectID: def.ProjectID,
		Domain:    domainassets.EventDomain,
		Channel:   domainassets.AccountsChannel(ownerID),
		CreatedAt: now,
		Attrs:     attrs,
	})
}

func (a *Assets) observe(kind domainassets.EntryKind, class string, err error, replay bool) {
	result := "ok"
	if err != nil {
		result = "error"
	} else if replay {
		result = "idempotent"
	}
	if class == "" {
		class = "unknown"
	}
	assetOpsTotal.WithLabelValues(string(kind), class, result).Inc()
}

func liveHoldings(in []domainassets.Holding, now time.Time) []domainassets.Holding {
	out := make([]domainassets.Holding, 0, len(in))
	for i := range in {
		if in[i].Expired(now) {
			continue
		}
		out = append(out, in[i])
	}
	return out
}

func checkMaxQuantity(def *domainassets.Def, live []domainassets.Holding, add int64) error {
	if def.MaxQuantity == nil {
		return nil
	}
	var sum int64
	for i := range live {
		sum += live[i].Quantity
	}
	if sum+add > *def.MaxQuantity {
		return fmt.Errorf("%w: %d + %d > %d", domainassets.ErrMaxQuantity, sum, add, *def.MaxQuantity)
	}
	return nil
}

func normalizeRef(refType, refID, fallback string) (string, string) {
	if refType == "" {
		refType = fallback
	}
	if refID == "" {
		refID = newID()
	}
	return refType, refID
}
