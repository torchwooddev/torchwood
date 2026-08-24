// Package restapi provides common utilities for implementing RESTful/gRPC APIs
// following Google AIP standards (https://google.aip.dev)
//
// This package implements:
// - AIP-132: List methods (sorting)
// - AIP-158: Pagination (page_size, page_token, next_page_token)
// - AIP-160: Filtering
package crud

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const (
	// DefaultPageSize is the default page size for list operations
	DefaultPageSize = 50

	// MaxPageSize is the maximum page size allowed
	MaxPageSize = 1000

	// DefaultPageSizeNoToken is the default page size when no page_token is provided
	DefaultPageSizeNoToken = 50
)

// ListParams represents the parsed parameters from a List request
// following Google AIP standards (AIP-132, AIP-158, AIP-160)
type ListParams struct {
	// PageSize is the maximum number of results to return
	PageSize int32

	// PageToken is the token for requesting the next page
	PageToken string

	// Filter is the filter expression (AIP-160)
	Filter string

	// OrderBy specifies the sort order (AIP-132)
	OrderBy string

	// Offset is the decoded offset from the page token (internal use)
	Offset int
}

// ParseListParams parses and validates list parameters from a List request
// following AIP-132 and AIP-158 standards.
//
// It enforces:
// - page_size between 1 and MaxPageSize (default: DefaultPageSize)
// - Validates page_token format if provided
// - page_token 签名验证（启用签名后伪造/跨环境 token 被拒）
// - offset 上限 MaxQueryOffset（防伪造超深分页拖垮数据库）
// - order_by/filter 与签发该 token 的请求一致（token 内记录 digest）
func ParseListParams(pageSize int32, pageToken, filter, orderBy string) (ListParams, error) {
	params := ListParams{
		PageSize:  pageSize,
		PageToken: pageToken,
		Filter:    filter,
		OrderBy:   orderBy,
	}

	// Validate and set page_size
	if params.PageSize <= 0 {
		if params.PageToken == "" {
			params.PageSize = DefaultPageSizeNoToken
		} else {
			params.PageSize = DefaultPageSize
		}
	}

	// Enforce maximum page size (AIP-158)
	if params.PageSize > MaxPageSize {
		params.PageSize = MaxPageSize
	}

	// Decode page token to get offset
	if params.PageToken != "" {
		data, err := DecodePageTokenFull(params.PageToken)
		if err != nil {
			return ListParams{}, fmt.Errorf("invalid page_token: %w", err)
		}
		// R4-J2-4：order_by/filter 跨页一致性。仅当 token 记录了对应约束时校验，
		// 兼容未记录约束的历史 token。
		canonicalOrderBy := strings.TrimSpace(strings.ToLower(orderBy))
		if data.OrderBy != "" && strings.TrimSpace(strings.ToLower(data.OrderBy)) != canonicalOrderBy {
			return ListParams{}, fmt.Errorf("order_by must match the original request when using page_token")
		}
		if data.FilterDigest != "" && data.FilterDigest != FilterDigest(filter) {
			return ListParams{}, fmt.Errorf("filter must match the original request when using page_token")
		}
		if data.Offset > MaxQueryOffset {
			return ListParams{}, fmt.Errorf("page_token offset exceeds the maximum of %d", MaxQueryOffset)
		}
		params.Offset = data.Offset
	}

	return params, nil
}

// OffsetLimit returns offset and limit values for database queries
func (p ListParams) OffsetLimit() (offset int, limit int) {
	return p.Offset, int(p.PageSize)
}

// String returns a string representation of ListParams (for debugging)
func (p ListParams) String() string {
	return fmt.Sprintf(
		"ListParams{PageSize: %d, PageToken: %q, Filter: %q, OrderBy: %q, Offset: %d}",
		p.PageSize, p.PageToken, p.Filter, p.OrderBy, p.Offset,
	)
}

// ValidatePageTokenIntegrity validates that the page token is consistent
// with the current request parameters (AIP-158 requirement)
func ValidatePageTokenIntegrity(
	params ListParams,
	originalPageSize int32,
	originalFilter string,
	originalOrderBy string,
) error {
	// Note: In a real implementation, you would encode additional parameters
	// in the page token to validate consistency across pages.
	// For now, this is a placeholder for future enhancement.

	if params.PageToken != "" && originalPageSize > 0 {
		// Page size should be consistent when using page tokens
		if params.PageSize != originalPageSize && originalPageSize <= MaxPageSize {
			return fmt.Errorf("page_size must be consistent across pages when using page_token")
		}
	}

	return nil
}

// ContextWithListParams returns a context with list params attached
func ContextWithListParams(ctx context.Context, params ListParams) context.Context {
	return context.WithValue(ctx, listParamsKey{}, params)
}

// ListParamsFromContext extracts list params from context
func ListParamsFromContext(ctx context.Context) (ListParams, bool) {
	params, ok := ctx.Value(listParamsKey{}).(ListParams)
	return params, ok
}

type listParamsKey struct{}

// CalculateHasMore determines if there are more results based on
// current offset, result count, and total count
func CalculateHasMore(offset int, resultCount int, totalCount int) bool {
	if totalCount < 0 {
		// Unknown total count - assume no more if result count < page size
		return false
	}
	return offset+resultCount < totalCount
}

// CalculateHasMoreWithResult determines if there are more results
// when total count is unknown but we can check if we got a full page
func CalculateHasMoreWithResult(resultCount int, pageSize int32) bool {
	return resultCount >= int(pageSize)
}

// PageTokenInfo represents decoded page token information
type PageTokenInfo struct {
	Offset int            `json:"offset"`
	Token  *PageTokenData `json:"token,omitempty"`
}

// ValidatePageTokenForRequest validates that a page token is appropriate
// for the current request context
func ValidatePageTokenForRequest(
	ctx context.Context,
	pageToken string,
	filter string,
	orderBy string,
) error {
	if pageToken == "" {
		return nil
	}

	// Decode and validate the token structure
	info, err := DecodeAndValidatePageToken(pageToken)
	if err != nil {
		return err
	}
	if info.Token != nil {
		canonicalOrderBy := strings.TrimSpace(strings.ToLower(orderBy))
		if info.Token.OrderBy != "" && strings.TrimSpace(strings.ToLower(info.Token.OrderBy)) != canonicalOrderBy {
			return fmt.Errorf("order_by must match the original request when using page_token")
		}
		filterDigest := FilterDigest(filter)
		if info.Token.FilterDigest != "" && info.Token.FilterDigest != filterDigest {
			return fmt.Errorf("filter must match the original request when using page_token")
		}
	}

	// If we have stored params in context, validate consistency
	if storedParams, ok := ListParamsFromContext(ctx); ok {
		if filter != storedParams.Filter {
			return fmt.Errorf("filter must match the original request when using page_token")
		}
		if orderBy != storedParams.OrderBy {
			return fmt.Errorf("order_by must match the original request when using page_token")
		}
	}

	_ = info // Reserved for future use
	return nil
}

// ParseListRequest is a convenience function that parses all list parameters
// and performs comprehensive validation
func ParseListRequest(
	ctx context.Context,
	pageSize int32,
	pageToken string,
	filter string,
	orderBy string,
) (context.Context, ListParams, error) {
	// Validate page token against request if provided
	if err := ValidatePageTokenForRequest(ctx, pageToken, filter, orderBy); err != nil {
		return nil, ListParams{}, err
	}

	// Parse the parameters
	params, err := ParseListParams(pageSize, pageToken, filter, orderBy)
	if err != nil {
		return nil, ListParams{}, err
	}

	// Store params in context for later use
	ctx = ContextWithListParams(ctx, params)

	return ctx, params, nil
}

// FilterDigest returns a deterministic digest for filter expression compatibility checks.
func FilterDigest(filter string) string {
	normalized := strings.TrimSpace(strings.ToLower(filter))
	if normalized == "" {
		return ""
	}
	if parsed, err := ParseFilter(normalized); err == nil {
		chunks := make([]string, 0, len(parsed.Expressions))
		for _, exp := range parsed.Expressions {
			chunks = append(chunks, strings.TrimSpace(strings.ToLower(fmt.Sprintf("%s|%s|%v", exp.Field, exp.Operator, exp.Value))))
		}
		sort.Strings(chunks)
		normalized = strings.Join(chunks, "&&")
	}
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

// GetPageSizeOrDefault returns the page size or default value
func GetPageSizeOrDefault(pageSize int32) int32 {
	if pageSize <= 0 {
		return DefaultPageSize
	}
	if pageSize > MaxPageSize {
		return MaxPageSize
	}
	return pageSize
}

// NormalizePageSize ensures page size is within valid bounds
func NormalizePageSize(pageSize int32) int32 {
	if pageSize <= 0 {
		return DefaultPageSize
	}
	if pageSize > MaxPageSize {
		return MaxPageSize
	}
	return pageSize
}

// FormatTotalCount formats total count for response (-1 means unknown)
func FormatTotalCount(totalCount int) *int32 {
	if totalCount < 0 {
		return nil
	}
	count := int32(totalCount)
	return &count
}

// CalculateOffset calculates the offset for a given page index and size
func CalculateOffset(pageIndex int, pageSize int32) int {
	return pageIndex * int(pageSize)
}

// CalculatePageIndex calculates the page index from an offset
func CalculatePageIndex(offset int, pageSize int32) int {
	if pageSize <= 0 {
		return 0
	}
	return offset / int(pageSize)
}

// StringToInt32 safely converts a string to int32
func StringToInt32(s string, defaultValue int32) int32 {
	if s == "" {
		return defaultValue
	}
	val, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return defaultValue
	}
	return int32(val)
}
