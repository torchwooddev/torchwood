package shared

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
)

// v2 设计 §5.1/§5.2：created_by 标识与操作者规则。
func TestTransactionActor(t *testing.T) {
	t.Parallel()
	require.Equal(t, "user:u1", TransactionActor(&shared.Principal{ActorKind: shared.ActorKindEndUser, UserID: "u1"}))
	require.Equal(t, "key:k1", TransactionActor(&shared.Principal{ActorKind: shared.ActorKindService, APIKeyID: "k1"}))
	require.Equal(t, "admin:a1", TransactionActor(&shared.Principal{ActorKind: shared.ActorKindAdmin, ActorID: "a1"}))
	require.Equal(t, "", TransactionActor(&shared.Principal{ActorKind: shared.ActorKindEndUser}))
}

func TestCanOperateTransaction(t *testing.T) {
	t.Parallel()
	// 创建者本人。
	require.True(t, CanOperateTransaction(&shared.Principal{ActorKind: shared.ActorKindEndUser, UserID: "u1"}, "user:u1"))
	// 非创建者端用户拒绝。
	require.False(t, CanOperateTransaction(&shared.Principal{ActorKind: shared.ActorKindEndUser, UserID: "u2"}, "user:u1"))
	// platform admin 可干预任意 pending。
	require.True(t, CanOperateTransaction(&shared.Principal{ActorKind: shared.ActorKindAdmin, ActorID: "a1", IsPlatformAdmin: true}, "user:u1"))
	// 非 platform admin 的受限管理员不可干预他人事务。
	require.False(t, CanOperateTransaction(&shared.Principal{ActorKind: shared.ActorKindAdmin, ActorID: "a2"}, "user:u1"))
	// databases 写 scope 的 API Key 可干预任意 pending。
	for _, scopes := range [][]string{{"databases.write"}, {"databases"}, {"*"}, {"all"}} {
		require.True(t, CanOperateTransaction(&shared.Principal{ActorKind: shared.ActorKindService, APIKeyID: "k2", Permissions: scopes}, "key:k1"), "scopes=%v", scopes)
	}
	// 只读 scope 的 API Key 不可干预。
	require.False(t, CanOperateTransaction(&shared.Principal{ActorKind: shared.ActorKindService, APIKeyID: "k3", Permissions: []string{"databases.read"}}, "key:k1"))
}
