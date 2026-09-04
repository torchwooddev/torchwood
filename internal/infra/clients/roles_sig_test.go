package clients

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestSignRolesSig_FormatAndVerify（阶段③-b 包 C）：签名格式 "<exp>|<hexmac>"，
// 消息 = roles + "|" + exp；与 SQL 侧 public.hmac(msg, key_hex, 'sha256') 的
// hex 输出逐字一致（键 = hex 字符串本身的字节）。
func TestSignRolesSig_FormatAndVerify(t *testing.T) {
	require.NoError(t, InitRolesSigKey("unit-test-master-secret-0123456789abcdef"))
	keyHex, ok := RolesSigKeyHex()
	require.True(t, ok)

	now := time.Unix(1750000000, 0).UTC()
	sig := SignRolesSig(keyHex, "any\x1fuser:bob", now)
	expHex := strings.SplitN(sig, "|", 2)
	require.Len(t, expHex, 2)
	require.Equal(t, "1750000060", expHex[0], "exp = now + 60s")

	// 独立重算（不依赖实现自身）验证 mac。
	mac := hmac.New(sha256.New, []byte(keyHex))
	_, _ = mac.Write([]byte("any\x1fuser:bob|1750000060"))
	require.Equal(t, hex.EncodeToString(mac.Sum(nil)), expHex[1])

	// 主密钥派生：与 page-token 同模式（HMAC-SHA256(master, purpose)）。
	master := hmac.New(sha256.New, []byte("unit-test-master-secret-0123456789abcdef"))
	_, _ = master.Write([]byte(RolesSigPurpose))
	require.Equal(t, hex.EncodeToString(master.Sum(nil)), keyHex)

	// 密钥初始化门槛：空主密钥拒绝。
	require.Error(t, InitRolesSigKey("  "))
}

// TestInjectIdentitySQL_IncludesSig：tw_app 注入语句携带 roles_sig（密钥已
// 初始化时）；tw_owner/tw_system 与密钥未初始化的 tw_app 不带（DB 侧验签
// fail-closed）。
func TestInjectIdentitySQL_IncludesSig(t *testing.T) {
	require.NoError(t, InitRolesSigKey("unit-test-master-secret-0123456789abcdef"))

	stmt, args := injectIdentitySQL(RoleApp, "any")
	require.Contains(t, stmt, `set_config('app.roles_sig', ?, true)`)
	require.Len(t, args, 2)
	require.Contains(t, args[1].(string), "|", "sig 形态 <exp>|<hexmac>")

	for _, role := range []string{RoleOwner, RoleSystem} {
		stmt, args = injectIdentitySQL(role, "")
		require.NotContains(t, stmt, "app.roles_sig", "仅 tw_app 验签")
		require.Len(t, args, 1)
	}

	// 密钥未初始化的 tw_app：不注入 sig（fail-closed 由 DB 侧承载）。
	rolesSigKeyHex.Store("")
	stmt, args = injectIdentitySQL(RoleApp, "any")
	require.NotContains(t, stmt, "app.roles_sig")
	require.Len(t, args, 1)
	// 恢复密钥，避免影响同包其他测试。
	require.NoError(t, InitRolesSigKey("unit-test-master-secret-0123456789abcdef"))
}
