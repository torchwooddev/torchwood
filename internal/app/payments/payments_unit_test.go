package payments

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	domainevents "github.com/torchwooddev/torchwood/internal/domain/events"
	domainpayments "github.com/torchwooddev/torchwood/internal/domain/payments"
	domainshared "github.com/torchwooddev/torchwood/internal/domain/shared"
	infrapayments "github.com/torchwooddev/torchwood/internal/infra/payments"
	"github.com/torchwooddev/torchwood/internal/infra/payments/stripe"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const unitWebhookSecret = "whsec_unit_test"

// memStore 是订单 / 回调 / 履约 / outbox 的内存库，RunInTx 失败整体回滚。
type memStore struct {
	orders       map[string]*domainpayments.Order
	byIdem       map[string]string
	callbacks    map[string]struct{}
	fulfillments map[string]*domainpayments.Fulfillment
	byFulfillID  map[string]string
	outbox       []domainevents.Envelope
}

func newMemStore() *memStore {
	return &memStore{
		orders:       map[string]*domainpayments.Order{},
		byIdem:       map[string]string{},
		callbacks:    map[string]struct{}{},
		fulfillments: map[string]*domainpayments.Fulfillment{},
		byFulfillID:  map[string]string{},
	}
}

func (s *memStore) RunInTx(_ context.Context, fn func(context.Context) error) error {
	snap := s.snapshot()
	if err := fn(context.Background()); err != nil {
		s.restore(snap)
		return err
	}
	return nil
}

func (s *memStore) snapshot() memStore {
	out := memStore{
		orders:       make(map[string]*domainpayments.Order, len(s.orders)),
		byIdem:       make(map[string]string, len(s.byIdem)),
		callbacks:    make(map[string]struct{}, len(s.callbacks)),
		fulfillments: make(map[string]*domainpayments.Fulfillment, len(s.fulfillments)),
		byFulfillID:  make(map[string]string, len(s.byFulfillID)),
		outbox:       append([]domainevents.Envelope(nil), s.outbox...),
	}
	for k, v := range s.orders {
		out.orders[k] = cloneOrder(v)
	}
	for k, v := range s.byIdem {
		out.byIdem[k] = v
	}
	for k := range s.callbacks {
		out.callbacks[k] = struct{}{}
	}
	for k, v := range s.fulfillments {
		out.fulfillments[k] = cloneFulfillment(v)
	}
	for k, v := range s.byFulfillID {
		out.byFulfillID[k] = v
	}
	return out
}

func (s *memStore) restore(snap memStore) {
	s.orders = snap.orders
	s.byIdem = snap.byIdem
	s.callbacks = snap.callbacks
	s.fulfillments = snap.fulfillments
	s.byFulfillID = snap.byFulfillID
	s.outbox = snap.outbox
}

func cloneOrder(o *domainpayments.Order) *domainpayments.Order {
	if o == nil {
		return nil
	}
	cp := *o
	if o.PaidAt != nil {
		t := *o.PaidAt
		cp.PaidAt = &t
	}
	if o.Purpose != nil {
		cp.Purpose = append(json.RawMessage(nil), o.Purpose...)
	}
	return &cp
}

func cloneFulfillment(f *domainpayments.Fulfillment) *domainpayments.Fulfillment {
	if f == nil {
		return nil
	}
	cp := *f
	if f.Detail != nil {
		cp.Detail = make(map[string]any, len(f.Detail))
		for k, v := range f.Detail {
			cp.Detail[k] = v
		}
	}
	return &cp
}

func idemKey(projectID, key string) string  { return projectID + "\x00" + key }
func cbKey(provider, eventID string) string { return provider + "\x00" + eventID }

type memOrders struct{ s *memStore }

func (r memOrders) Insert(_ context.Context, order *domainpayments.Order) (*domainpayments.Order, bool, error) {
	k := idemKey(order.ProjectID, order.IdempotencyKey)
	if id, ok := r.s.byIdem[k]; ok {
		return cloneOrder(r.s.orders[id]), false, nil
	}
	r.s.orders[order.ID] = cloneOrder(order)
	r.s.byIdem[k] = order.ID
	return nil, true, nil
}

func (r memOrders) GetByID(_ context.Context, projectID, orderID string) (*domainpayments.Order, error) {
	return r.get(projectID, orderID)
}

func (r memOrders) GetByIDForUpdate(_ context.Context, projectID, orderID string) (*domainpayments.Order, error) {
	return r.get(projectID, orderID)
}

func (r memOrders) get(projectID, orderID string) (*domainpayments.Order, error) {
	o := r.s.orders[orderID]
	if o == nil {
		return nil, nil
	}
	if projectID != "" && o.ProjectID != projectID {
		return nil, nil
	}
	return cloneOrder(o), nil
}

func (r memOrders) GetByProviderRef(_ context.Context, projectID, provider, sessionID, orderID string) (*domainpayments.Order, error) {
	for _, o := range r.s.orders {
		if o.Provider != provider {
			continue
		}
		if projectID != "" && o.ProjectID != projectID {
			continue
		}
		if (sessionID != "" && o.ProviderSessionID == sessionID) || (orderID != "" && o.ProviderOrderID == orderID) {
			return cloneOrder(o), nil
		}
	}
	return nil, nil
}

func (r memOrders) Update(_ context.Context, order *domainpayments.Order, expect domainpayments.OrderStatus) error {
	cur := r.s.orders[order.ID]
	if cur == nil || cur.Status != expect {
		return status.Error(codes.Aborted, "payment order concurrently modified")
	}
	r.s.orders[order.ID] = cloneOrder(order)
	return nil
}

func (r memOrders) ListByUser(context.Context, string, string, int, time.Time) ([]domainpayments.Order, error) {
	return nil, nil
}
func (r memOrders) ListByProject(context.Context, string, int, time.Time) ([]domainpayments.Order, error) {
	return nil, nil
}
func (r memOrders) CloseExpired(context.Context, time.Time, int) (int64, error) { return 0, nil }

type memCallbacks struct{ s *memStore }

func (r memCallbacks) InsertIfAbsent(_ context.Context, event *domainpayments.CallbackEvent, _, _ string) (bool, error) {
	k := cbKey(event.Provider, event.ProviderEventID)
	if _, ok := r.s.callbacks[k]; ok {
		return false, nil
	}
	r.s.callbacks[k] = struct{}{}
	return true, nil
}

type memFulfillments struct{ s *memStore }

func (r memFulfillments) InsertPending(_ context.Context, f *domainpayments.Fulfillment) (*domainpayments.Fulfillment, bool, error) {
	if existing, ok := r.s.fulfillments[f.OrderID]; ok {
		return cloneFulfillment(existing), false, nil
	}
	cp := cloneFulfillment(f)
	cp.Status = domainpayments.FulfillmentPending
	r.s.fulfillments[f.OrderID] = cp
	r.s.byFulfillID[f.ID] = f.OrderID
	return nil, true, nil
}

func (r memFulfillments) MarkDone(_ context.Context, fulfillmentID, ref string, detail map[string]any) error {
	oid, ok := r.s.byFulfillID[fulfillmentID]
	if !ok {
		return errors.New("fulfillment not found")
	}
	f := r.s.fulfillments[oid]
	f.Status = domainpayments.FulfillmentDone
	f.Ref = ref
	if detail != nil {
		f.Detail = detail
	}
	return nil
}

func (r memFulfillments) MarkFailed(_ context.Context, fulfillmentID, reason string) error {
	oid, ok := r.s.byFulfillID[fulfillmentID]
	if !ok {
		return errors.New("fulfillment not found")
	}
	f := r.s.fulfillments[oid]
	f.Status = domainpayments.FulfillmentFailed
	f.Detail = map[string]any{"reason": reason}
	return nil
}

func (r memFulfillments) GetByOrder(_ context.Context, _, orderID string) (*domainpayments.Fulfillment, error) {
	return cloneFulfillment(r.s.fulfillments[orderID]), nil
}

type memPublisher struct{ s *memStore }

func (p memPublisher) Publish(_ context.Context, ev domainevents.Envelope) error {
	p.s.outbox = append(p.s.outbox, ev)
	return nil
}

type fakeProvider struct {
	createCalls int
	createErr   error
	session     *domainpayments.PaymentSession
	verify      func(http.Header, []byte) (*domainpayments.CallbackEvent, error)
	verifyErr   error
}

func (f *fakeProvider) Name() string { return domainpayments.ProviderStripe }

func (f *fakeProvider) CreatePayment(_ context.Context, _ domainpayments.CreatePaymentInput) (*domainpayments.PaymentSession, error) {
	f.createCalls++
	if f.createErr != nil {
		return nil, f.createErr
	}
	if f.session != nil {
		return f.session, nil
	}
	return &domainpayments.PaymentSession{SessionID: "cs_fake", PaymentURL: "https://pay.example/cs_fake"}, nil
}

func (f *fakeProvider) VerifyCallback(_ context.Context, h http.Header, raw []byte) (*domainpayments.CallbackEvent, error) {
	if f.verifyErr != nil {
		return nil, f.verifyErr
	}
	if f.verify != nil {
		return f.verify(h, raw)
	}
	var ev domainpayments.CallbackEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil, err
	}
	if ev.Provider == "" {
		ev.Provider = f.Name()
	}
	return &ev, nil
}

func (f *fakeProvider) Refund(context.Context, domainpayments.RefundInput) (*domainpayments.RefundResult, error) {
	return nil, domainpayments.ErrUnsupported
}

type fakeRegistry struct {
	p domainpayments.PaymentProvider
}

func (r fakeRegistry) Get(name string) (domainpayments.PaymentProvider, error) {
	if r.p == nil || r.p.Name() != name {
		return nil, fmt.Errorf("unknown provider %q", name)
	}
	return r.p, nil
}

type countingFulfiller struct {
	calls int
	err   error
}

func (f *countingFulfiller) Fulfill(_ context.Context, order *domainpayments.Order) (string, error) {
	f.calls++
	if f.err != nil {
		return "", f.err
	}
	return "order:" + order.ID, nil
}

type unitEnv struct {
	payments *Payments
	store    *memStore
	provider *fakeProvider
}

func setupUnit(t *testing.T, provider domainpayments.PaymentProvider, fulfiller domainpayments.Fulfiller) *unitEnv {
	t.Helper()
	store := newMemStore()
	fp, _ := provider.(*fakeProvider)
	uc := newPayments(
		nil,
		store,
		memOrders{store},
		memCallbacks{store},
		memFulfillments{store},
		fulfiller,
		fakeRegistry{p: provider},
		memPublisher{store},
		nil,
		nil,
	)
	return &unitEnv{payments: uc, store: store, provider: fp}
}

func unitUserCtx(projectID, userID string) context.Context {
	return contexts.WithPrincipal(context.Background(), &domainshared.Principal{
		ActorKind:      domainshared.ActorKindEndUser,
		ProjectID:      projectID,
		UserID:         userID,
		CredentialType: domainshared.CredentialTypeToken,
	})
}

func seedPayingOrder(t *testing.T, store *memStore, orderID string, amount int64) *domainpayments.Order {
	t.Helper()
	now := time.Now()
	order := &domainpayments.Order{
		ID:                orderID,
		ProjectID:         "proj-1",
		UserID:            "u1",
		Provider:          domainpayments.ProviderStripe,
		IdempotencyKey:    "idem-" + orderID,
		ProviderSessionID: "cs_" + orderID,
		Amount:            amount,
		Currency:          "USD",
		PurposeKind:       domainpayments.PurposeTopup,
		Purpose:           json.RawMessage(`{"currency_code":"gold"}`),
		Status:            domainpayments.OrderStatusPaying,
		CreatedAt:         now,
		UpdatedAt:         now,
		ExpiresAt:         now.Add(time.Hour),
	}
	store.orders[order.ID] = cloneOrder(order)
	store.byIdem[idemKey(order.ProjectID, order.IdempotencyKey)] = order.ID
	return order
}

func paidCallbackJSON(t *testing.T, eventID, orderID string, amount int64) []byte {
	t.Helper()
	body, err := json.Marshal(domainpayments.CallbackEvent{
		Provider:          domainpayments.ProviderStripe,
		ProviderEventID:   eventID,
		ProviderSessionID: "cs_" + orderID,
		ProviderOrderID:   "pi_" + orderID,
		Type:              domainpayments.CallbackPaid,
		Amount:            amount,
		Currency:          "USD",
		OrderID:           orderID,
	})
	require.NoError(t, err)
	return body
}

func signStripe(t *testing.T, secret string, body []byte) http.Header {
	t.Helper()
	ts := time.Now().Unix()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("%d.", ts)))
	mac.Write(body)
	h := http.Header{}
	h.Set("Stripe-Signature", fmt.Sprintf("t=%d,v1=%s", ts, hex.EncodeToString(mac.Sum(nil))))
	return h
}

func TestCreateOrder_IdempotencyKeySkipsSecondCreatePayment(t *testing.T) {
	provider := &fakeProvider{}
	env := setupUnit(t, provider, NewRecordOnlyFulfiller())
	ctx := unitUserCtx("proj-1", "u1")
	cmd := CreateOrderCommand{
		Provider:       domainpayments.ProviderStripe,
		Amount:         1999,
		Currency:       "USD",
		PurposeKind:    domainpayments.PurposeTopup,
		Purpose:        map[string]any{"currency_code": "gold"},
		IdempotencyKey: "idem-dup",
	}

	first, err := env.payments.CreateOrder(ctx, cmd)
	require.NoError(t, err)
	require.False(t, first.IdempotentReplay)
	require.Equal(t, domainpayments.OrderStatusPaying, first.Order.Status)
	require.Equal(t, 1, provider.createCalls)
	require.Equal(t, 1, len(env.store.orders))

	second, err := env.payments.CreateOrder(ctx, cmd)
	require.NoError(t, err)
	require.True(t, second.IdempotentReplay)
	require.Equal(t, first.Order.ID, second.Order.ID)
	require.Equal(t, 1, provider.createCalls, "幂等重放不得向渠道二次 CreatePayment")
	require.Equal(t, 1, len(env.store.orders))
}

func TestCreateOrder_RejectsNonPositiveAmount(t *testing.T) {
	provider := &fakeProvider{}
	env := setupUnit(t, provider, NewRecordOnlyFulfiller())
	ctx := unitUserCtx("proj-1", "u1")
	for _, amount := range []int64{0, -100} {
		_, err := env.payments.CreateOrder(ctx, CreateOrderCommand{
			Provider: domainpayments.ProviderStripe, Amount: amount, Currency: "USD",
			PurposeKind: domainpayments.PurposeTopup, IdempotencyKey: fmt.Sprintf("bad-%d", amount),
		})
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	}
	require.Equal(t, 0, provider.createCalls)
	require.Empty(t, env.store.orders)
}

func TestHandleCallback_ReplaySameEventIDNoReentry(t *testing.T) {
	fulfiller := &countingFulfiller{}
	env := setupUnit(t, &fakeProvider{}, fulfiller)
	order := seedPayingOrder(t, env.store, "ord-replay", 1999)
	body := paidCallbackJSON(t, "evt_replay", order.ID, 1999)

	require.NoError(t, env.payments.HandleCallback(context.Background(), domainpayments.ProviderStripe, nil, body))
	require.Equal(t, domainpayments.OrderStatusPaid, env.store.orders[order.ID].Status)
	require.NotNil(t, env.store.orders[order.ID].PaidAt)
	require.Equal(t, 1, fulfiller.calls)
	require.Equal(t, 1, len(env.store.fulfillments))
	require.Equal(t, domainpayments.FulfillmentDone, env.store.fulfillments[order.ID].Status)
	require.Equal(t, 1, len(env.store.outbox))
	require.Equal(t, domainpayments.EventOrderPaid, env.store.outbox[0].Event)
	require.Equal(t, "accounts.u1", env.store.outbox[0].Channel)
	paidAt := *env.store.orders[order.ID].PaidAt

	require.NoError(t, env.payments.HandleCallback(context.Background(), domainpayments.ProviderStripe, nil, body))
	require.Equal(t, 1, fulfiller.calls, "同 event.id 重放不得二次履约")
	require.Equal(t, 1, len(env.store.fulfillments))
	require.Equal(t, 1, len(env.store.outbox), "同 event.id 重放不得二次 Publish")
	require.Equal(t, 1, len(env.store.callbacks))
	require.Equal(t, paidAt.Unix(), env.store.orders[order.ID].PaidAt.Unix(), "paid_at 不被重放覆盖")
}

func TestHandleCallback_PaidSameTxFulfillmentAndOutbox(t *testing.T) {
	fulfiller := &countingFulfiller{}
	env := setupUnit(t, &fakeProvider{}, fulfiller)
	order := seedPayingOrder(t, env.store, "ord-paid", 500)
	body := paidCallbackJSON(t, "evt_paid", order.ID, 500)

	require.NoError(t, env.payments.HandleCallback(context.Background(), domainpayments.ProviderStripe, nil, body))

	got := env.store.orders[order.ID]
	require.Equal(t, domainpayments.OrderStatusPaid, got.Status)
	require.NotNil(t, got.PaidAt)
	require.Equal(t, "pi_"+order.ID, got.ProviderOrderID)
	require.Equal(t, 1, fulfiller.calls)

	f := env.store.fulfillments[order.ID]
	require.NotNil(t, f)
	require.Equal(t, domainpayments.FulfillmentDone, f.Status)
	require.Equal(t, "order:"+order.ID, f.Ref)

	require.Equal(t, 1, len(env.store.outbox))
	require.Equal(t, domainpayments.EventOrderPaid, env.store.outbox[0].Event)
	require.Equal(t, domainpayments.EventDomain, env.store.outbox[0].Domain)
	require.Equal(t, "accounts.u1", env.store.outbox[0].Channel)
	require.Equal(t, int64(500), env.store.outbox[0].Attrs["amount"])
	require.Contains(t, env.store.callbacks, cbKey(domainpayments.ProviderStripe, "evt_paid"))
}

func TestHandleCallback_FulfillerErrorRollsBack(t *testing.T) {
	fulfiller := &countingFulfiller{err: errors.New("assets not ready (PR2)")}
	env := setupUnit(t, &fakeProvider{}, fulfiller)
	order := seedPayingOrder(t, env.store, "ord-ff", 300)
	body := paidCallbackJSON(t, "evt_ff", order.ID, 300)

	err := env.payments.HandleCallback(context.Background(), domainpayments.ProviderStripe, nil, body)
	require.Error(t, err)
	require.Equal(t, 1, fulfiller.calls, "必须真正走进 Fulfill 才算同事务回滚")

	got := env.store.orders[order.ID]
	require.Equal(t, domainpayments.OrderStatusPaying, got.Status, "履约失败订单保持 paying")
	require.Nil(t, got.PaidAt)
	require.Empty(t, env.store.fulfillments, "不得留下 pending/done 履约行")
	require.Empty(t, env.store.outbox, "不得 Publish")
	require.Empty(t, env.store.callbacks, "回调行与翻转同事务，失败一并回滚")
}

func TestHandleCallback_ForgedSignatureWritesNothing(t *testing.T) {
	adapter := stripe.New(stripe.Config{SecretKey: "sk_test_x", WebhookSecret: unitWebhookSecret})
	env := setupUnit(t, adapter, NewRecordOnlyFulfiller())
	order := seedPayingOrder(t, env.store, "ord-401", 500)

	body := []byte(`{"id":"evt_fake","type":"checkout.session.completed","data":{"object":{"id":"cs_x","client_reference_id":"ord-401","payment_status":"paid","amount_total":500,"currency":"usd"}}}`)
	h := http.Header{}
	h.Set("Stripe-Signature", "t=1700000000,v1=0000000000000000000000000000000000000000000000000000000000000000")

	err := env.payments.HandleCallback(context.Background(), domainpayments.ProviderStripe, h, body)
	require.ErrorIs(t, err, domainpayments.ErrSignatureInvalid)
	require.Equal(t, domainpayments.OrderStatusPaying, env.store.orders[order.ID].Status)
	require.Empty(t, env.store.callbacks)
	require.Empty(t, env.store.fulfillments)
	require.Empty(t, env.store.outbox)
}

func TestHandleCallback_TamperedBodyWritesNothing(t *testing.T) {
	adapter := stripe.New(stripe.Config{SecretKey: "sk_test_x", WebhookSecret: unitWebhookSecret})
	env := setupUnit(t, adapter, NewRecordOnlyFulfiller())
	order := seedPayingOrder(t, env.store, "ord-tamper", 500)

	good := []byte(`{"id":"evt_tamper","type":"checkout.session.completed","data":{"object":{"id":"cs_x","client_reference_id":"ord-tamper","payment_status":"paid","amount_total":500,"currency":"usd"}}}`)
	h := signStripe(t, unitWebhookSecret, good)
	tampered := []byte(strings.Replace(string(good), `"amount_total":500`, `"amount_total":9999`, 1))

	err := env.payments.HandleCallback(context.Background(), domainpayments.ProviderStripe, h, tampered)
	require.ErrorIs(t, err, domainpayments.ErrSignatureInvalid)
	require.Equal(t, domainpayments.OrderStatusPaying, env.store.orders[order.ID].Status)
	require.Empty(t, env.store.callbacks)
	require.Empty(t, env.store.fulfillments)
	require.Empty(t, env.store.outbox)
}

func TestHandleCallback_UnknownProviderWritesNothing(t *testing.T) {
	env := setupUnit(t, &fakeProvider{}, NewRecordOnlyFulfiller())
	_ = seedPayingOrder(t, env.store, "ord-wx", 100)
	err := env.payments.HandleCallback(context.Background(), "wechat", nil, []byte(`{}`))
	require.ErrorIs(t, err, domainpayments.ErrSignatureInvalid)
	require.Empty(t, env.store.callbacks)
	require.Empty(t, env.store.outbox)
}

func TestNewPayments_WiresDatabaseAsTxRunner(t *testing.T) {
	// 生产构造器签名不变：nil *clients.Database 仍可装配（handler 验签失败路径）。
	adapter := stripe.New(stripe.Config{SecretKey: "sk", WebhookSecret: unitWebhookSecret})
	uc := NewPayments(nil, nil, nil, nil, nil, NewRecordOnlyFulfiller(), infrapayments.NewRegistry(adapter), nil, nil, nil)
	require.NotNil(t, uc)
}
