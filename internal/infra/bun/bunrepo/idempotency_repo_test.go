package bunrepo_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwooddev/torchwood/internal/testutil"
)

// TryClaim/Complete/Release 语义（redesign §4.1/§10.1）：认领-完成-重放-冲突。
func TestIdempotencyStore_ClaimCompleteReplayConflict(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer func() { _ = db.Close() }()

	s := bunrepo.NewIdempotencyStore(db)
	key := databases.IdempotencyKey{ProjectID: "p1", ActorID: "a1", RequestID: "r1"}

	c1, err := s.TryClaim(ctx, key, "fp1")
	require.NoError(t, err)
	require.Equal(t, databases.IdempotencyClaimAcquired, c1.State)
	require.NotEmpty(t, c1.Token)

	// 认领后同指纹重试 → InFlight。
	c1b, err := s.TryClaim(ctx, key, "fp1")
	require.NoError(t, err)
	require.Equal(t, databases.IdempotencyClaimInFlight, c1b.State)

	// 完成后同指纹 → Done + payload 重放。
	require.NoError(t, s.Complete(ctx, key, c1.Token, []byte(`{"ok":1}`)))
	c2, err := s.TryClaim(ctx, key, "fp1")
	require.NoError(t, err)
	require.Equal(t, databases.IdempotencyClaimDone, c2.State)
	require.JSONEq(t, `{"ok":1}`, string(c2.Payload))

	// 同 key 不同指纹 → KEY_CONFLICT。
	_, err = s.TryClaim(ctx, key, "fp2")
	require.ErrorIs(t, err, databases.ErrIdempotencyKeyConflict)

	// Release 只作用于 in_flight 行（done 行不受影响）。
	c3, err := s.TryClaim(ctx, databases.IdempotencyKey{ProjectID: "p1", ActorID: "a1", RequestID: "r2"}, "fp1")
	require.NoError(t, err)
	require.NoError(t, s.Release(ctx, databases.IdempotencyKey{ProjectID: "p1", ActorID: "a1", RequestID: "r2"}, c3.Token))
	c4, err := s.TryClaim(ctx, databases.IdempotencyKey{ProjectID: "p1", ActorID: "a1", RequestID: "r2"}, "fp1")
	require.NoError(t, err)
	require.Equal(t, databases.IdempotencyClaimAcquired, c4.State, "Release 后同键可重新认领")
}
