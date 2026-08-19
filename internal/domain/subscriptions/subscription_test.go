package subscriptions

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCanTransition(t *testing.T) {
	t.Parallel()
	allow := [][2]Status{
		{StatusTrialing, StatusActive},
		{StatusTrialing, StatusPastDue},
		{StatusTrialing, StatusCanceled},
		{StatusTrialing, StatusExpired},
		{StatusActive, StatusPastDue},
		{StatusActive, StatusCanceled},
		{StatusActive, StatusExpired},
		{StatusPastDue, StatusActive},
		{StatusPastDue, StatusCanceled},
		{StatusPastDue, StatusExpired},
		{StatusActive, StatusActive}, // 幂等同态
	}
	for _, pair := range allow {
		require.Truef(t, CanTransition(pair[0], pair[1]), "%s -> %s", pair[0], pair[1])
	}
	deny := [][2]Status{
		{StatusCanceled, StatusActive},
		{StatusExpired, StatusActive},
		{StatusCanceled, StatusPastDue},
		{StatusExpired, StatusCanceled},
		{StatusActive, StatusTrialing},
		{StatusPastDue, StatusTrialing},
	}
	for _, pair := range deny {
		require.Falsef(t, CanTransition(pair[0], pair[1]), "%s -> %s", pair[0], pair[1])
	}
}

func TestTransitionPastDueClearsOnRecover(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	grace := now.Add(3 * 24 * time.Hour)
	sub := &Subscription{ID: "s1", Status: StatusActive, GraceUntil: &grace}
	require.NoError(t, sub.Transition(StatusPastDue, now))
	require.Equal(t, StatusPastDue, sub.Status)
	require.NoError(t, sub.Transition(StatusActive, now))
	require.Equal(t, StatusActive, sub.Status)
	require.Nil(t, sub.GraceUntil)
}

func TestNextPeriodEnd(t *testing.T) {
	t.Parallel()
	from := time.Date(2026, 1, 15, 15, 4, 5, 0, time.UTC)
	month, err := NextPeriodEnd(from, IntervalMonth, 0)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 2, 15, 15, 4, 5, 0, time.UTC), month)

	year, err := NextPeriodEnd(from, IntervalYear, 0)
	require.NoError(t, err)
	require.Equal(t, time.Date(2027, 1, 15, 15, 4, 5, 0, time.UTC), year)

	days, err := NextPeriodEnd(from, IntervalCustomDays, 10)
	require.NoError(t, err)
	require.Equal(t, from.Add(10*24*time.Hour), days)

	_, err = NextPeriodEnd(from, IntervalCustomDays, 0)
	require.Error(t, err)
}

func TestComputeGraceUntil(t *testing.T) {
	t.Parallel()
	from := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	require.Equal(t, from, ComputeGraceUntil(from, 0))
	require.Equal(t, from.Add(3*24*time.Hour), ComputeGraceUntil(from, 3))
}

func TestBenefitsValidateRejectsNonPositiveQuantity(t *testing.T) {
	t.Parallel()
	err := (Benefits{Grants: []BenefitGrant{{AssetCode: "gold", Quantity: 0}}}).Validate()
	require.Error(t, err)
	err = (Benefits{Grants: []BenefitGrant{{AssetCode: "gold", Quantity: 10}}}).Validate()
	require.NoError(t, err)
}
