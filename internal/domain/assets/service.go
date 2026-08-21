package assets

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	domainevents "github.com/torchwooddev/torchwood/internal/domain/events"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
)

// TxRunner 在同一 ctx 事务内执行 fn（S-4：落地仍用 ctx 传连接，不升 uow.Run）。
type TxRunner interface {
	RunInTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// Service 是资产五动词的领域服务：跨 Def + Holding + Ledger 的不变式引擎。
// 锁策略留在本类型内部；仓储只持久化。
type Service struct {
	db       TxRunner
	defs     DefRepo
	holdings HoldingRepo
	ledger   LedgerRepo
	events   shared.EventPublisher
	now      func() time.Time
	newID    func() string
}

// NewService 构造领域服务。now / newID 由 app 注入（测试可冻结时钟与 ID）。
func NewService(
	db TxRunner,
	defs DefRepo,
	holdings HoldingRepo,
	ledger LedgerRepo,
	events shared.EventPublisher,
	now func() time.Time,
	newID func() string,
) *Service {
	s := &Service{
		db:       db,
		defs:     defs,
		holdings: holdings,
		ledger:   ledger,
		events:   events,
		now:      now,
		newID:    newID,
	}
	if s.now == nil {
		s.now = func() time.Time { return time.Now().UTC() }
	}
	if s.newID == nil {
		s.newID = func() string { panic("assets: newID is required") }
	}
	return s
}

func (s *Service) ts() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now().UTC()
}

func (s *Service) requireScope(scope Scope) error {
	if scope.ProjectID == "" {
		return ErrProjectRequired
	}
	return nil
}

func (s *Service) prepareWrite(ownerType OwnerType, ownerID, idem string) (OwnerType, string, error) {
	ot, err := NormalizeOwnerType(ownerType)
	if err != nil {
		return "", "", err
	}
	if ownerID == "" {
		return "", "", ErrOwnerRequired
	}
	key, err := ValidateIdempotencyKey(idem)
	if err != nil {
		return "", "", err
	}
	return ot, key, nil
}

func (s *Service) requireActiveDef(ctx context.Context, projectID, code string) (*Def, error) {
	def, err := s.defs.GetByCodeForShare(ctx, projectID, code)
	if err != nil {
		return nil, err
	}
	if def == nil {
		return nil, ErrDefNotFound
	}
	if def.Status != DefStatusActive {
		return nil, ErrDefArchived
	}
	return def, nil
}

func (s *Service) resolveGrantExpiry(def *Def, explicit *time.Time) (*time.Time, error) {
	if explicit != nil {
		if !def.AllowsExpiry() {
			return nil, fmt.Errorf("%w: currency grant must not set expires_at", ErrMatrix)
		}
		return normalizeExpiry(explicit), nil
	}
	if def.ExpiresIn != nil && *def.ExpiresIn > 0 {
		if !def.AllowsExpiry() {
			return nil, fmt.Errorf("%w: currency must not have expires_in", ErrMatrix)
		}
		t := s.ts().Add(time.Duration(*def.ExpiresIn) * time.Second)
		return normalizeExpiry(&t), nil
	}
	if def.RequiresExpiry() {
		return nil, ErrExpiresAtRequired
	}
	return nil, nil
}

func (s *Service) loadReplay(ctx context.Context, projectID, key string) ([]LedgerEntry, bool, error) {
	first, err := s.ledger.GetByIdempotencyKey(ctx, projectID, key)
	if err != nil {
		return nil, false, err
	}
	if first == nil {
		return nil, false, nil
	}
	if first.RefID != "" {
		all, err := s.ledger.ListByRef(ctx, projectID, first.RefType, first.RefID)
		if err != nil {
			return nil, false, err
		}
		if len(all) > 0 {
			return all, true, nil
		}
	}
	return []LedgerEntry{*first}, true, nil
}

func (s *Service) newEntry(e LedgerEntry, op json.RawMessage) *LedgerEntry {
	if e.ID == "" {
		e.ID = s.newID()
	}
	if e.Operator == nil {
		if len(op) == 0 {
			e.Operator = MarshalOperator(OperatorSnapshot{IsSystem: true})
		} else {
			e.Operator = op
		}
	}
	return &e
}

func (s *Service) publish(ctx context.Context, def *Def, ownerID string, kind EntryKind, delta, after int64, now time.Time) error {
	if s.events == nil {
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
	return s.events.Publish(ctx, domainevents.Envelope{
		EventID:   s.newID(),
		Event:     EventNameForKind(kind),
		ProjectID: def.ProjectID,
		Domain:    EventDomain,
		Channel:   AccountsChannel(ownerID),
		CreatedAt: now,
		Attrs:     attrs,
	})
}

func (s *Service) normalizeRef(refType, refID, fallback string) (string, string) {
	if refType == "" {
		refType = fallback
	}
	if refID == "" {
		refID = s.newID()
	}
	return refType, refID
}

// LiveHolding 返回业主某定义下未过期的第一行（事务内加锁）。
// 供订阅 entitlement 续期：已有持有则 Mutate，否则 Grant。须在外层事务内调用。
func (s *Service) LiveHolding(ctx context.Context, scope Scope, ownerID, defCode string) (*Holding, error) {
	if err := s.requireScope(scope); err != nil {
		return nil, err
	}
	if ownerID == "" {
		return nil, ErrOwnerRequired
	}
	code, err := ValidateCode(defCode)
	if err != nil {
		return nil, err
	}
	def, err := s.requireActiveDef(ctx, scope.ProjectID, code)
	if err != nil {
		return nil, err
	}
	holdings, err := s.holdings.ListForUpdate(ctx, scope.ProjectID, OwnerTypeUser, ownerID, def.ID)
	if err != nil {
		return nil, err
	}
	live := liveHoldings(holdings, s.ts())
	if len(live) == 0 {
		return nil, nil
	}
	h := live[0]
	return &h, nil
}

func normalizeExpiry(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC().Truncate(time.Microsecond)
	return &u
}

func sameExpiry(a, b *time.Time) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Equal(*b)
}

func liveHoldings(in []Holding, now time.Time) []Holding {
	out := make([]Holding, 0, len(in))
	for i := range in {
		if in[i].Expired(now) {
			continue
		}
		out = append(out, in[i])
	}
	return out
}

func checkMaxQuantity(def *Def, live []Holding, add int64) error {
	if def.MaxQuantity == nil {
		return nil
	}
	var sum int64
	for i := range live {
		sum += live[i].Quantity
	}
	if sum+add > *def.MaxQuantity {
		return fmt.Errorf("%w: %d + %d > %d", ErrMaxQuantity, sum, add, *def.MaxQuantity)
	}
	return nil
}
