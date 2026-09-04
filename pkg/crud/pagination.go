package crud

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"
)

const (
	// PageTokenVersion is the current version of page token format
	PageTokenVersion = "v1"

	// PageTokenSeparator separates parts of the page token
	PageTokenSeparator = ":"

	// DefaultTokenTTL is the default time-to-live for page tokens
	DefaultTokenTTL = 24 * time.Hour

	// TokenModeLegacy represents offset-based token mode.
	TokenModeLegacy = "legacy"
	// TokenModeCursor represents cursor-based token mode.
	TokenModeCursor = "cursor"

	// MaxQueryOffset 上限：拒绝客户端伪造超深分页 token 打爆数据库。
	// 仅服务静态表/控制面 offset token（文档面已 keyset-only，原 documentdb
	// 侧同名上限随 C2 阶段①收敛删除——R4-J2-4 的对齐关系就此解除）。
	MaxQueryOffset = 10000

	// pageTokenKeyPurpose 是密钥派生的 purpose 域，与 pkg/jwtparser
	// DeriveKey(master, purpose) 同构（HMAC-SHA256(master, purpose)），
	// 本地实现以避免 pkg 内反向依赖。
	pageTokenKeyPurpose = "torchwood-page-token-v1"
)

// pageTokenSecret 进程级签名密钥（HMAC-SHA256，空串=未启用）。由组合根在
// 启动期通过 InitPageTokenSigning 注入（server 与 worker 都必须调用）；未注入
// 时编码退化为未签名 token、带签名的 token 解码被拒——保证灰度发布期间旧
// token 兼容，同时新签发 token 在任何配置正确的进程中都可验证。
var pageTokenSecret atomic.Value // string

// InitPageTokenSigning 启用页 token 的 HMAC 签名与验签。master 为部署主密钥
// （security.jwt.secret）；实际签名密钥经 HMAC-SHA256(master, purpose) 派生，
// 与 JWT/OAuth 等域隔离。
func InitPageTokenSigning(master string) error {
	if strings.TrimSpace(master) == "" {
		return fmt.Errorf("page token signing requires a non-empty master secret")
	}
	mac := hmac.New(sha256.New, []byte(master))
	_, _ = mac.Write([]byte(pageTokenKeyPurpose))
	pageTokenSecret.Store(hex.EncodeToString(mac.Sum(nil)))
	return nil
}

// PageTokenSigningEnabled 报告当前进程是否已启用签名（测试与启动断言用）。
func PageTokenSigningEnabled() bool {
	return pageTokenKey() != ""
}

func pageTokenKey() string {
	if v, ok := pageTokenSecret.Load().(string); ok {
		return v
	}
	return ""
}

// seal 为 token 附加 HMAC 签名（若已启用签名）。失败仅可能因内部状态异常，
// 此时退化为未签名并在解码侧被拒（fail-closed），不影响可用性。
func seal(data *PageTokenData) {
	key := pageTokenKey()
	if key == "" {
		return
	}
	if sig, err := SignPageToken(data, key); err == nil {
		data.Sig = sig
	}
}

// PageTokenData represents the internal structure of a page token
// Following AIP-158: https://google.aip.dev/158
type PageTokenData struct {
	Version      string            `json:"v"`                       // Token format version
	Mode         string            `json:"mode,omitempty"`          // legacy | cursor
	Offset       int               `json:"offset"`                  // Current offset
	Created      time.Time         `json:"created"`                 // Token creation time
	PageSize     int32             `json:"page_size"`               // Original page size
	OrderBy      string            `json:"order_by,omitempty"`      // OrderBy of original request
	FilterDigest string            `json:"filter_digest,omitempty"` // Canonical filter digest
	Keys         map[string]string `json:"keys,omitempty"`          // Cursor seek keys
	TTL          int64             `json:"ttl,omitempty"`           // Token TTL in seconds
	Checksum     string            `json:"checksum,omitempty"`      // Compatibility checksum
	Sig          string            `json:"sig,omitempty"`           // HMAC-SHA256 signature
}

// EncodePageToken creates a page token from offset
// This is a simplified version following AIP-158.
// 已启用签名时（InitPageTokenSigning）token 附带 HMAC，客户端无法伪造偏移。
func EncodePageToken(offset int) string {
	data := PageTokenData{
		Version: PageTokenVersion,
		Mode:    TokenModeLegacy,
		Offset:  offset,
		Created: time.Now().UTC(),
	}
	seal(&data)

	return encodeTokenData(data)
}

// EncodePageTokenWithSize creates a page token with page size info
func EncodePageTokenWithSize(offset int, pageSize int32) string {
	data := PageTokenData{
		Version:  PageTokenVersion,
		Mode:     TokenModeLegacy,
		Offset:   offset,
		Created:  time.Now().UTC(),
		PageSize: pageSize,
	}
	seal(&data)

	return encodeTokenData(data)
}

// EncodePageTokenFull creates a page token with full metadata
func EncodePageTokenFull(offset int, pageSize int32, checksum string) string {
	data := PageTokenData{
		Version:  PageTokenVersion,
		Mode:     TokenModeLegacy,
		Offset:   offset,
		Created:  time.Now().UTC(),
		PageSize: pageSize,
		Checksum: checksum,
	}
	seal(&data)

	return encodeTokenData(data)
}

// EncodeCursorPageToken creates a cursor-mode page token with metadata.
func EncodeCursorPageToken(offset int, pageSize int32, orderBy, filterDigest string, keys map[string]string, checksum string) string {
	data := PageTokenData{
		Version:      PageTokenVersion,
		Mode:         TokenModeCursor,
		Offset:       offset,
		Created:      time.Now().UTC(),
		PageSize:     pageSize,
		OrderBy:      strings.TrimSpace(orderBy),
		FilterDigest: strings.TrimSpace(filterDigest),
		Keys:         keys,
		TTL:          int64(DefaultTokenTTL / time.Second),
		Checksum:     strings.TrimSpace(checksum),
	}
	return encodeTokenData(data)
}

// EncodeSignedCursorPageToken creates a signed cursor-mode token.
func EncodeSignedCursorPageToken(offset int, pageSize int32, orderBy, filterDigest string, keys map[string]string, checksum, secret string) (string, error) {
	data := PageTokenData{
		Version:      PageTokenVersion,
		Mode:         TokenModeCursor,
		Offset:       offset,
		Created:      time.Now().UTC(),
		PageSize:     pageSize,
		OrderBy:      strings.TrimSpace(orderBy),
		FilterDigest: strings.TrimSpace(filterDigest),
		Keys:         keys,
		TTL:          int64(DefaultTokenTTL / time.Second),
		Checksum:     strings.TrimSpace(checksum),
	}
	sig, err := SignPageToken(&data, secret)
	if err != nil {
		return "", err
	}
	data.Sig = sig
	return encodeTokenData(data), nil
}

// SignPageToken computes HMAC-SHA256 signature for token payload.
func SignPageToken(data *PageTokenData, secret string) (string, error) {
	if data == nil {
		return "", fmt.Errorf("page token data is nil")
	}
	if strings.TrimSpace(secret) == "" {
		return "", fmt.Errorf("page token secret is required")
	}
	canonical := *data
	canonical.Sig = ""
	raw, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(raw)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// VerifyPageTokenSignature validates page token signature.
func VerifyPageTokenSignature(data *PageTokenData, secret string) error {
	if data == nil {
		return fmt.Errorf("page token data is nil")
	}
	if data.Sig == "" {
		return fmt.Errorf("page token signature missing")
	}
	expected, err := SignPageToken(data, secret)
	if err != nil {
		return err
	}
	if !hmac.Equal([]byte(expected), []byte(data.Sig)) {
		return fmt.Errorf("invalid page token signature")
	}
	return nil
}

// encodeTokenData encodes token data to base64 string
func encodeTokenData(data PageTokenData) string {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		// Fallback to simple format
		return fmt.Sprintf("%s%s%d", PageTokenVersion, PageTokenSeparator, data.Offset)
	}

	// Base64 encode the JSON
	return base64.URLEncoding.EncodeToString(jsonBytes)
}

// DecodePageToken decodes a page token and returns the offset
// Following AIP-158 standard.
// 带 Sig 的 token 必须能被当前进程密钥验证（篡改/跨环境重放即拒绝）；
// 未签名 token（灰度期存量或未启用签名的测试进程）按原语义接受。
func DecodePageToken(token string) (int, error) {
	if token == "" {
		return 0, nil
	}

	// Try to decode as base64 JSON first (new format)
	data, err := decodeTokenData(token)
	if err == nil {
		if err := verifyTokenAuthenticity(&data); err != nil {
			return 0, err
		}
		// Validate token version
		if data.Version != PageTokenVersion {
			return 0, fmt.Errorf("invalid page token version: %s", data.Version)
		}

		// Check token expiration
		ttl := DefaultTokenTTL
		if data.TTL > 0 {
			ttl = time.Duration(data.TTL) * time.Second
		}
		if time.Since(data.Created) > ttl {
			return 0, fmt.Errorf("page token has expired")
		}

		return data.Offset, nil
	}

	// Fallback: try old simple format "version:offset"
	// （历史实现用 Sscanf("%s:%d") 因 %s 贪婪匹配从未成功过，这里按
	// 分隔符真实解析，保持文档语义可用。该格式无法携带签名，仅在未启用
	// 签名的进程中被接受。）
	if pageTokenKey() == "" {
		parts := strings.SplitN(token, PageTokenSeparator, 2)
		if len(parts) == 2 && parts[0] == PageTokenVersion {
			var offset int
			if _, err := fmt.Sscanf(parts[1], "%d", &offset); err == nil {
				slog.Warn("deprecated unsigned page_token format; clients should adopt the refreshed token from the previous response")
				return offset, nil
			}
		}
	}

	return 0, fmt.Errorf("invalid page token format")
}

// verifyTokenAuthenticity 校验 token 来源可信：
//   - 未启用签名（key 为空）：跳过校验，保持历史语义——显式密钥的
//     DecodeSignedPageTokenFull 与既有测试不受影响；
//   - 已启用签名：token 必须携带且通过当前进程密钥的 HMAC 验证，
//     无签名 / 篡改 / 跨环境 token 一律拒绝（fail-closed）。
func verifyTokenAuthenticity(data *PageTokenData) error {
	key := pageTokenKey()
	if key == "" {
		return nil
	}
	if data.Sig == "" {
		return fmt.Errorf("page token signature missing: token signing is enforced on this deployment")
	}
	return VerifyPageTokenSignature(data, key)
}

// decodeTokenData decodes token data from base64 string
func decodeTokenData(token string) (PageTokenData, error) {
	var data PageTokenData

	// Decode base64
	jsonBytes, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return data, fmt.Errorf("failed to decode page token: %w", err)
	}

	// Unmarshal JSON
	err = json.Unmarshal(jsonBytes, &data)
	if err != nil {
		return data, fmt.Errorf("failed to parse page token data: %w", err)
	}

	return data, nil
}

// DecodePageTokenFull decodes a page token and returns full metadata
func DecodePageTokenFull(token string) (*PageTokenData, error) {
	if token == "" {
		return nil, fmt.Errorf("empty page token")
	}

	data, err := decodeTokenData(token)
	if err != nil {
		return nil, err
	}

	if err := verifyTokenAuthenticity(&data); err != nil {
		return nil, err
	}

	// Validate token version
	if data.Version != PageTokenVersion {
		return nil, fmt.Errorf("invalid page token version: %s", data.Version)
	}

	// Check token expiration
	ttl := DefaultTokenTTL
	if data.TTL > 0 {
		ttl = time.Duration(data.TTL) * time.Second
	}
	if time.Since(data.Created) > ttl {
		return nil, fmt.Errorf("page token has expired")
	}
	if data.Mode == "" {
		data.Mode = TokenModeLegacy
	}

	return &data, nil
}

// DecodeSignedPageTokenFull decodes and validates token signature.
func DecodeSignedPageTokenFull(token, secret string) (*PageTokenData, error) {
	data, err := DecodePageTokenFull(token)
	if err != nil {
		return nil, err
	}
	if err := VerifyPageTokenSignature(data, secret); err != nil {
		return nil, err
	}
	return data, nil
}

// DecodeAndValidatePageToken decodes and validates a page token
func DecodeAndValidatePageToken(token string) (*PageTokenInfo, error) {
	data, err := DecodePageTokenFull(token)
	if err != nil {
		return nil, err
	}

	return &PageTokenInfo{
		Offset: data.Offset,
		Token:  data,
	}, nil
}

// ValidatePageToken checks if a page token is valid without decoding it
func ValidatePageToken(token string) error {
	if token == "" {
		return nil
	}

	_, err := DecodePageToken(token)
	return err
}

// IsExpired checks if a page token has expired
func IsExpired(token string) bool {
	data, err := decodeTokenData(token)
	if err != nil {
		return true
	}
	ttl := DefaultTokenTTL
	if data.TTL > 0 {
		ttl = time.Duration(data.TTL) * time.Second
	}
	return time.Since(data.Created) > ttl
}

// GetTokenAge returns the age of a page token
func GetTokenAge(token string) (time.Duration, error) {
	data, err := decodeTokenData(token)
	if err != nil {
		return 0, err
	}

	return time.Since(data.Created), nil
}

// CalculateTotalPages calculates the total number of pages
func CalculateTotalPages(totalCount int, pageSize int32) int {
	if pageSize <= 0 {
		return 0
	}
	if totalCount <= 0 {
		return 0
	}

	pages := totalCount / int(pageSize)
	if totalCount%int(pageSize) > 0 {
		pages++
	}

	return pages
}

// CalculateCurrentPage calculates the current page number (1-indexed)
func CalculateCurrentPage(offset int, pageSize int32) int {
	if pageSize <= 0 {
		return 1
	}
	return (offset / int(pageSize)) + 1
}

// PaginationInfo contains detailed pagination information
type PaginationInfo struct {
	CurrentPage    int   `json:"current_page"`
	PageSize       int32 `json:"page_size"`
	TotalPages     int   `json:"total_pages,omitempty"`
	TotalCount     int   `json:"total_count,omitempty"`
	HasPrevious    bool  `json:"has_previous"`
	HasNext        bool  `json:"has_next"`
	NextOffset     int   `json:"next_offset,omitempty"`
	PreviousOffset int   `json:"previous_offset,omitempty"`
}

// BuildPaginationInfo builds complete pagination info
func BuildPaginationInfo(params ListParams, totalCount int, hasMore bool) PaginationInfo {
	info := PaginationInfo{
		CurrentPage: CalculateCurrentPage(params.Offset, params.PageSize),
		PageSize:    params.PageSize,
		HasPrevious: params.Offset > 0,
		HasNext:     hasMore,
	}

	if totalCount >= 0 {
		info.TotalCount = totalCount
		info.TotalPages = CalculateTotalPages(totalCount, params.PageSize)
	}

	if hasMore {
		info.NextOffset = params.Offset + int(params.PageSize)
	}

	if params.Offset > 0 {
		info.PreviousOffset = max(0, params.Offset-int(params.PageSize))
	}

	return info
}

// BuildPaginationInfoSimple builds simple pagination info without total count
func BuildPaginationInfoSimple(params ListParams, resultCount int, hasMore bool) PaginationInfo {
	info := PaginationInfo{
		CurrentPage: CalculateCurrentPage(params.Offset, params.PageSize),
		PageSize:    params.PageSize,
		HasPrevious: params.Offset > 0,
		HasNext:     hasMore,
	}

	if hasMore {
		info.NextOffset = params.Offset + resultCount
	}

	if params.Offset > 0 {
		info.PreviousOffset = max(0, params.Offset-int(params.PageSize))
	}

	return info
}

// GeneratePreviousPageToken generates a page token for the previous page.
// token 内记录 order_by/filter digest，供解码侧做跨页一致性校验（R4-J2-4）。
func GeneratePreviousPageToken(params ListParams) string {
	if params.Offset <= 0 {
		return ""
	}

	previousOffset := max(0, params.Offset-int(params.PageSize))
	data := PageTokenData{
		Version:      PageTokenVersion,
		Mode:         TokenModeLegacy,
		Offset:       previousOffset,
		Created:      time.Now().UTC(),
		PageSize:     params.PageSize,
		OrderBy:      strings.TrimSpace(params.OrderBy),
		FilterDigest: FilterDigest(params.Filter),
	}
	seal(&data)
	return encodeTokenData(data)
}

// GeneratePageTokens generates both next and previous page tokens.
// token 内记录 order_by/filter digest，供解码侧做跨页一致性校验（R4-J2-4）。
func GeneratePageTokens(params ListParams, resultCount int, totalCount int) (nextToken, previousToken string) {
	// Next page token
	if params.Offset+resultCount < totalCount {
		data := PageTokenData{
			Version:      PageTokenVersion,
			Mode:         TokenModeLegacy,
			Offset:       params.Offset + resultCount,
			Created:      time.Now().UTC(),
			PageSize:     params.PageSize,
			OrderBy:      strings.TrimSpace(params.OrderBy),
			FilterDigest: FilterDigest(params.Filter),
		}
		seal(&data)
		nextToken = encodeTokenData(data)
	}

	// Previous page token
	if params.Offset > 0 {
		data := PageTokenData{
			Version:      PageTokenVersion,
			Mode:         TokenModeLegacy,
			Offset:       max(0, params.Offset-int(params.PageSize)),
			Created:      time.Now().UTC(),
			PageSize:     params.PageSize,
			OrderBy:      strings.TrimSpace(params.OrderBy),
			FilterDigest: FilterDigest(params.Filter),
		}
		seal(&data)
		previousToken = encodeTokenData(data)
	}

	return nextToken, previousToken
}

// TokenChecksum calculates a checksum for page token validation
func TokenChecksum(data string) string {
	// Simple checksum implementation
	// In production, use a proper hash function like SHA256
	const maxLen = 8
	checksum := 0
	for i, c := range data {
		checksum += int(c) * (i + 1)
	}

	result := ""
	for checksum > 0 {
		result = string(rune('A'+(checksum%26))) + result
		checksum = checksum / 26
	}

	for len(result) < maxLen {
		result = "A" + result
	}

	if len(result) > maxLen {
		result = result[:maxLen]
	}

	return result
}

// ValidatePageTokenChecksum validates the checksum in a page token
func ValidatePageTokenChecksum(token string) bool {
	data, err := decodeTokenData(token)
	if err != nil {
		return false
	}

	if data.Checksum == "" {
		// No checksum to validate
		return true
	}

	// In a real implementation, you would calculate the expected checksum
	// based on the original request parameters and compare it
	// For now, just check that it's not empty
	return data.Checksum != ""
}
