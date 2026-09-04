package crud

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// base64URLEncode 与 encodeTokenData 的编码保持一致。
func base64URLEncode(t *testing.T, raw []byte) string {
	t.Helper()
	return base64.URLEncoding.EncodeToString(raw)
}

// resetSigning 统一管理进程级签名状态，避免用例间串扰（空串=未启用）。
func resetSigning(t *testing.T, master string) {
	t.Helper()
	if master == "" {
		pageTokenSecret.Store("")
	} else {
		require.NoError(t, InitPageTokenSigning(master))
	}
	t.Cleanup(func() { pageTokenSecret.Store("") })
}

func TestInitPageTokenSigningRejectsEmptyMaster(t *testing.T) {
	resetSigning(t, "")
	err := InitPageTokenSigning("   ")
	require.Error(t, err)
	require.False(t, PageTokenSigningEnabled())
}

func TestSignedTokenRoundTrip(t *testing.T) {
	resetSigning(t, "master-secret-for-tests-0123456789")

	token := EncodePageToken(250)
	offset, err := DecodePageToken(token)
	require.NoError(t, err)
	require.Equal(t, 250, offset)

	data, err := DecodePageTokenFull(token)
	require.NoError(t, err)
	require.NotEmpty(t, data.Sig, "signed process must mint signed tokens")
}

func TestTamperedOffsetRejected(t *testing.T) {
	resetSigning(t, "master-secret-for-tests-0123456789")

	token := EncodePageToken(20)
	data, err := decodeTokenData(token)
	require.NoError(t, err)
	data.Offset = 999999 // 攻击者篡改深翻页
	raw, err := json.Marshal(data)
	require.NoError(t, err)
	tampered := base64URLEncode(t, raw)

	_, err = DecodePageToken(tampered)
	require.ErrorContains(t, err, "signature")

	_, err = ParseListParams(10, tampered, "", "")
	require.ErrorContains(t, err, "signature")
}

func TestCrossEnvironmentTokenRejectedWhenSigningEnabled(t *testing.T) {
	// 未启用签名的进程签发的 token，带到已启用签名的进程必须被拒。
	resetSigning(t, "")
	unsigned := EncodePageToken(30)
	resetSigning(t, "another-master-secret-0123456789abcdef")

	_, err := DecodePageToken(unsigned)
	require.ErrorContains(t, err, "signature missing")
}

func TestUnsignedTokenAcceptedWithoutSigning(t *testing.T) {
	// 灰度兼容：未启用签名的进程仍接受历史未签名 token。
	resetSigning(t, "")
	token := EncodePageToken(40)
	offset, err := DecodePageToken(token)
	require.NoError(t, err)
	require.Equal(t, 40, offset)
}

func TestParseListParamsRejectsOffsetBeyondMax(t *testing.T) {
	resetSigning(t, "master-secret-for-tests-0123456789")

	token := EncodePageToken(MaxQueryOffset + 1)
	_, err := ParseListParams(10, token, "", "")
	require.ErrorContains(t, err, "exceeds the maximum")

	ok, err := ParseListParams(10, EncodePageToken(MaxQueryOffset), "", "")
	require.NoError(t, err)
	require.Equal(t, MaxQueryOffset, ok.Offset)
}

func TestParseListParamsBindsOrderByAndFilter(t *testing.T) {
	resetSigning(t, "master-secret-for-tests-0123456789")

	filter := `name equal "a"`
	orderBy := "created_at DESC"
	next := mintBoundToken(10, 10, filter, orderBy)
	prev := mintBoundToken(0, 10, filter, orderBy)

	_, err := ParseListParams(10, next, filter, orderBy)
	require.NoError(t, err)

	// 换 filter 复用旧 token 必须被拒（09-api-guide 声称的防护由此变为真实）。
	_, err = ParseListParams(10, next, `name equal "b"`, orderBy)
	require.ErrorContains(t, err, "filter must match")

	// 换 order_by 同理。
	_, err = ParseListParams(10, next, filter, "name ASC")
	require.ErrorContains(t, err, "order_by must match")

	_, err = ParseListParams(10, prev, filter, orderBy)
	require.NoError(t, err)
}

// mintBoundToken 手工铸造带 order_by/filter 绑定的 token，覆盖
// ParseListParams 的跨页一致性校验路径（生产侧 EncodePageToken 仅签发
// offset-only token，不携带绑定字段）。
func mintBoundToken(offset int, pageSize int32, filter, orderBy string) string {
	data := PageTokenData{
		Version:      PageTokenVersion,
		Mode:         TokenModeLegacy,
		Offset:       offset,
		Created:      time.Now().UTC(),
		PageSize:     pageSize,
		OrderBy:      strings.TrimSpace(orderBy),
		FilterDigest: FilterDigest(filter),
	}
	seal(&data)
	return encodeTokenData(data)
}

func TestLegacyColonTokenRejected(t *testing.T) {
	// 历史 "v1:offset" 简单格式已退役：无论进程是否启用签名都不可解析。
	resetSigning(t, "")
	_, err := DecodePageToken("v1:77")
	require.Error(t, err)

	resetSigning(t, "master-secret-for-tests-0123456789")
	_, err = DecodePageToken("v1:77")
	require.Error(t, err)
}
