package payments

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOrderTransitionStateMachine(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name string
		path []OrderStatus
		ok   bool
	}{
		{"created→paying→paid", []OrderStatus{OrderStatusCreated, OrderStatusPaying, OrderStatusPaid}, true},
		{"created→paid（iOS 直接验票）", []OrderStatus{OrderStatusCreated, OrderStatusPaid}, true},
		{"created→paying→failed", []OrderStatus{OrderStatusCreated, OrderStatusPaying, OrderStatusFailed}, true},
		{"created→closed（超时）", []OrderStatus{OrderStatusCreated, OrderStatusClosed}, true},
		{"created→paying→closed", []OrderStatus{OrderStatusCreated, OrderStatusPaying, OrderStatusClosed}, true},
		{"paid→refunding→refunded", []OrderStatus{OrderStatusCreated, OrderStatusPaid, OrderStatusRefunding, OrderStatusRefunded}, true},
		{"paid→refunded（同步退款）", []OrderStatus{OrderStatusCreated, OrderStatusPaid, OrderStatusRefunded}, true},
		{"paid→paying 非法", []OrderStatus{OrderStatusCreated, OrderStatusPaid, OrderStatusPaying}, false},
		{"closed→paid 非法（迟到支付不重入）", []OrderStatus{OrderStatusCreated, OrderStatusClosed, OrderStatusPaid}, false},
		{"failed→paid 非法", []OrderStatus{OrderStatusCreated, OrderStatusFailed, OrderStatusPaid}, false},
		{"refunded→paid 非法", []OrderStatus{OrderStatusCreated, OrderStatusPaid, OrderStatusRefunded, OrderStatusPaid}, false},
		{"created→refunded 非法", []OrderStatus{OrderStatusCreated, OrderStatusRefunded}, false},
		{"paid→created 非法", []OrderStatus{OrderStatusCreated, OrderStatusPaid, OrderStatusCreated}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			order := &Order{ID: "o1", Status: tc.path[0]}
			var err error
			for _, next := range tc.path[1:] {
				err = order.Transition(next, now)
				if err != nil {
					break
				}
			}
			if tc.ok {
				require.NoError(t, err)
				require.Equal(t, tc.path[len(tc.path)-1], order.Status)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestOrderTransitionSetsPaidAtOnce(t *testing.T) {
	now := time.Now()
	order := &Order{ID: "o1", Status: OrderStatusPaying}
	require.NoError(t, order.Transition(OrderStatusPaid, now))
	require.NotNil(t, order.PaidAt)
	require.Equal(t, now.Unix(), order.PaidAt.Unix())
	// 再次翻转不重置 paid_at（防御）。
	later := now.Add(time.Hour)
	require.Error(t, order.Transition(OrderStatusPaid, later))
	require.Equal(t, now.Unix(), order.PaidAt.Unix())
}

func TestOrderExpired(t *testing.T) {
	now := time.Now()
	order := &Order{
		ID:        "o1",
		Status:    OrderStatusPaying,
		ExpiresAt: now.Add(-time.Minute),
	}
	require.True(t, order.Expired(now))
	order.Status = OrderStatusPaid
	require.False(t, order.Expired(now), "paid 订单不再超时关单")
	order.Status = OrderStatusCreated
	require.True(t, order.Expired(now))
}

func TestOrderStatusIsTerminal(t *testing.T) {
	require.True(t, OrderStatusFailed.IsTerminal())
	require.True(t, OrderStatusClosed.IsTerminal())
	require.True(t, OrderStatusRefunded.IsTerminal())
	require.False(t, OrderStatusPaid.IsTerminal(), "paid 后仍有退款迁移")
	require.False(t, OrderStatusCreated.IsTerminal())
	require.False(t, OrderStatusPaying.IsTerminal())
	require.False(t, OrderStatusRefunding.IsTerminal())
}

func TestPurposeKindIsValid(t *testing.T) {
	require.True(t, PurposeTopup.IsValid())
	require.True(t, PurposeItemPurchase.IsValid())
	require.True(t, PurposeSubscription.IsValid())
	require.False(t, PurposeKind("magic").IsValid())
	require.False(t, PurposeKind("").IsValid())
}

func TestAccountsChannel(t *testing.T) {
	require.Equal(t, "accounts.u1", AccountsChannel("u1"))
}
