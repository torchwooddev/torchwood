package crud

import (
	"context"
	"testing"
)

func TestParseListParams(t *testing.T) {
	tests := []struct {
		name       string
		pageSize   int32
		pageToken  string
		filter     string
		orderBy    string
		wantSize   int32
		wantOffset int
		wantErr    bool
	}{
		{
			name:       "valid params with defaults",
			pageSize:   0,
			pageToken:  "",
			wantSize:   DefaultPageSizeNoToken,
			wantOffset: 0,
			wantErr:    false,
		},
		{
			name:       "custom page size",
			pageSize:   100,
			wantSize:   100,
			wantOffset: 0,
			wantErr:    false,
		},
		{
			name:       "page size exceeds max",
			pageSize:   2000,
			wantSize:   MaxPageSize,
			wantOffset: 0,
			wantErr:    false,
		},
		{
			name:       "with page token",
			pageSize:   50,
			pageToken:  EncodePageToken(100),
			wantSize:   50,
			wantOffset: 100,
			wantErr:    false,
		},
		{
			name:       "invalid page token",
			pageSize:   50,
			pageToken:  "invalid-token",
			wantSize:   50,
			wantOffset: 0,
			wantErr:    true,
		},
		{
			name:       "negative page size",
			pageSize:   -10,
			wantSize:   DefaultPageSizeNoToken,
			wantOffset: 0,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params, err := ParseListParams(tt.pageSize, tt.pageToken, tt.filter, tt.orderBy)

			if (err != nil) != tt.wantErr {
				t.Errorf("ParseListParams() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if params.PageSize != tt.wantSize {
					t.Errorf("PageSize = %v, want %v", params.PageSize, tt.wantSize)
				}
				if params.Offset != tt.wantOffset {
					t.Errorf("Offset = %v, want %v", params.Offset, tt.wantOffset)
				}
			}
		})
	}
}

func TestEncodeDecodePageToken(t *testing.T) {
	offsets := []int{0, 10, 100, 1000, 9999}

	for _, offset := range offsets {
		t.Run("", func(t *testing.T) {
			token := EncodePageToken(offset)
			decoded, err := DecodePageToken(token)

			if err != nil {
				t.Errorf("DecodePageToken() error = %v", err)
				return
			}

			if decoded != offset {
				t.Errorf("DecodePageToken() = %v, want %v", decoded, offset)
			}
		})
	}
}

func TestParseOrderBy(t *testing.T) {
	tests := []struct {
		name      string
		orderBy   string
		wantCount int
		wantErr   bool
	}{
		{
			name:      "single field",
			orderBy:   "name",
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "single field with direction",
			orderBy:   "name desc",
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "multiple fields",
			orderBy:   "created_at desc, name asc",
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:      "empty string",
			orderBy:   "",
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:      "invalid direction",
			orderBy:   "name invalid",
			wantCount: 0,
			wantErr:   true,
		},
		{
			name:      "invalid field name",
			orderBy:   "123name desc",
			wantCount: 0,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clauses, err := ParseOrderBy(tt.orderBy)

			if (err != nil) != tt.wantErr {
				t.Errorf("ParseOrderBy() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && len(clauses) != tt.wantCount {
				t.Errorf("ParseOrderBy() len = %v, want %v", len(clauses), tt.wantCount)
			}
		})
	}
}

func TestParseFilter(t *testing.T) {
	tests := []struct {
		name      string
		filter    string
		wantCount int
		wantErr   bool
	}{
		{
			name:      "simple equality",
			filter:    "status = \"active\"",
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "greater than",
			filter:    "age > 18",
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "contains",
			filter:    "name : \"John\"",
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "empty string",
			filter:    "",
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:      "invalid syntax",
			filter:    "invalid",
			wantCount: 0,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter, err := ParseFilter(tt.filter)

			if (err != nil) != tt.wantErr {
				t.Errorf("ParseFilter() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && len(filter.Expressions) != tt.wantCount {
				t.Errorf("ParseFilter() len = %v, want %v", len(filter.Expressions), tt.wantCount)
			}
		})
	}
}

// Temporarily disabled - BuildListMetadata not implemented
/*
func TestBuildListMetadata(t *testing.T) {
	tests := []struct {
		name        string
		params      ListParams
		resultCount int
		totalCount  int
		hasMore     bool
	}{
		{
			name: "first page with more",
			params: ListParams{
				PageSize: 50,
				Offset:   0,
			},
			resultCount: 50,
			totalCount:  100,
			hasMore:     true,
		},
		{
			name: "last page",
			params: ListParams{
				PageSize: 50,
				Offset:   50,
			},
			resultCount: 50,
			totalCount:  100,
			hasMore:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metadata := BuildListMetadata(tt.params, tt.resultCount, tt.totalCount, tt.hasMore)

			if metadata.TotalCount != tt.totalCount {
				t.Errorf("TotalCount = %v, want %v", metadata.TotalCount, tt.totalCount)
			}

			if metadata.CurrentOffset != tt.params.Offset {
				t.Errorf("CurrentOffset = %v, want %v", metadata.CurrentOffset, tt.params.Offset)
			}

			if metadata.HasMore != tt.hasMore {
				t.Errorf("HasMore = %v, want %v", metadata.HasMore, tt.hasMore)
			}

			// If has more, should have next page token
			if tt.hasMore && metadata.NextPageToken == "" {
				t.Error("Expected NextPageToken when HasMore is true")
			}
		})
	}
}
*/

func TestContextWithListParams(t *testing.T) {
	params := ListParams{
		PageSize: 100,
		Offset:   50,
	}

	ctx := context.Background()
	ctx = ContextWithListParams(ctx, params)

	retrieved, ok := ListParamsFromContext(ctx)
	if !ok {
		t.Fatal("ListParamsFromContext() returned ok = false")
	}

	if retrieved.PageSize != params.PageSize {
		t.Errorf("PageSize = %v, want %v", retrieved.PageSize, params.PageSize)
	}

	if retrieved.Offset != params.Offset {
		t.Errorf("Offset = %v, want %v", retrieved.Offset, params.Offset)
	}
}

func TestParseListRequest(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		pageSize  int32
		pageToken string
		filter    string
		orderBy   string
		wantErr   bool
	}{
		{
			name:     "valid request",
			pageSize: 50,
			wantErr:  false,
		},
		{
			name:      "valid request with page token",
			pageSize:  50,
			pageToken: EncodePageToken(100),
			wantErr:   false,
		},
		{
			name:      "invalid page token",
			pageSize:  50,
			pageToken: "invalid",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newCtx, params, err := ParseListRequest(ctx, tt.pageSize, tt.pageToken, tt.filter, tt.orderBy)

			if (err != nil) != tt.wantErr {
				t.Errorf("ParseListRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if newCtx == nil {
					t.Error("ParseListRequest() returned nil context")
				}

				if params.PageSize <= 0 {
					t.Error("PageSize should be positive")
				}
			}
		})
	}
}

func TestFilterDigest_Deterministic(t *testing.T) {
	d1 := FilterDigest(`status = "active" AND name : "foo"`)
	d2 := FilterDigest(`name : "foo" and status = "active"`)
	if d1 != d2 {
		t.Fatalf("expected deterministic digest, got %s vs %s", d1, d2)
	}
}

func TestOrderByValidator(t *testing.T) {
	validator := NewOrderByValidator().
		AddFields("name", "created_at", "updated_at").
		SetDefault(OrderByClause{Field: "created_at", Direction: "desc"})

	tests := []struct {
		name      string
		orderBy   string
		wantErr   bool
		wantCount int
	}{
		{
			name:      "valid field",
			orderBy:   "name asc",
			wantErr:   false,
			wantCount: 1,
		},
		{
			name:      "use default",
			orderBy:   "",
			wantErr:   false,
			wantCount: 1,
		},
		{
			name:      "invalid field",
			orderBy:   "invalid_field",
			wantErr:   true,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clauses, err := validator.Parse(tt.orderBy)

			if (err != nil) != tt.wantErr {
				t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && len(clauses) != tt.wantCount {
				t.Errorf("Parse() len = %v, want %v", len(clauses), tt.wantCount)
			}
		})
	}
}

func TestFilterValidator(t *testing.T) {
	validator := NewFilterValidator().
		AddFields("name", "status", "group_id")

	tests := []struct {
		name    string
		filter  string
		wantErr bool
	}{
		{
			name:    "valid field",
			filter:  "status = \"active\"",
			wantErr: false,
		},
		{
			name:    "empty filter",
			filter:  "",
			wantErr: false,
		},
		{
			name:    "invalid field",
			filter:  "invalid_field = \"value\"",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter, err := validator.Parse(tt.filter)

			if (err != nil) != tt.wantErr {
				t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && filter == nil {
				t.Error("Parse() returned nil filter")
			}
		})
	}
}

func TestCalculateHasMore(t *testing.T) {
	tests := []struct {
		name        string
		offset      int
		resultCount int
		totalCount  int
		wantHasMore bool
	}{
		{
			name:        "more results available",
			offset:      0,
			resultCount: 50,
			totalCount:  100,
			wantHasMore: true,
		},
		{
			name:        "last page",
			offset:      50,
			resultCount: 50,
			totalCount:  100,
			wantHasMore: false,
		},
		{
			name:        "unknown total count",
			offset:      0,
			resultCount: 50,
			totalCount:  -1,
			wantHasMore: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasMore := CalculateHasMore(tt.offset, tt.resultCount, tt.totalCount)
			if hasMore != tt.wantHasMore {
				t.Errorf("CalculateHasMore() = %v, want %v", hasMore, tt.wantHasMore)
			}
		})
	}
}

func TestSafeParsing(t *testing.T) {
	t.Run("SafeOrderBy", func(t *testing.T) {
		clauses := SafeOrderBy("invalid field asc", "created_at")
		if len(clauses) == 0 {
			t.Error("SafeOrderBy() should return default clauses")
		}
		if PrimaryField(clauses) != "created_at" {
			t.Errorf("Expected default field 'created_at', got '%s'", PrimaryField(clauses))
		}
	})

	t.Run("SafeFilter", func(t *testing.T) {
		filter := SafeFilter("invalid syntax")
		if !IsEmpty(filter) {
			t.Error("SafeFilter() should return empty filter for invalid input")
		}
	})

	t.Run("GetPageSizeOrDefault", func(t *testing.T) {
		size := GetPageSizeOrDefault(0)
		if size != DefaultPageSize {
			t.Errorf("GetPageSizeOrDefault() = %v, want %v", size, DefaultPageSize)
		}
	})
}

// Temporarily disabled - NewListResponseBuilder not implemented
/*
func TestListResponseBuilder(t *testing.T) {
	params := ListParams{PageSize: 50, Offset: 0}

	t.Run("BuildNextPageTokenWithTotal", func(t *testing.T) {
		builder := NewListResponseBuilder(params).
			SetTotalCount(100).
			SetHasMore(true)

		token := builder.BuildNextPageTokenWithTotal(0)
		if token == "" {
			t.Error("Expected non-empty next page token")
		}

		// Verify token can be decoded
		offset, err := DecodePageToken(token)
		if err != nil {
			t.Errorf("Failed to decode token: %v", err)
		}

		if offset != 50 {
			t.Errorf("Expected offset 50, got %d", offset)
		}
	})

	t.Run("No more results", func(t *testing.T) {
		builder := NewListResponseBuilder(params).
			SetTotalCount(50).
			SetHasMore(false)

		token := builder.BuildNextPageTokenWithTotal(0)
		if token != "" {
			t.Error("Expected empty next page token when no more results")
		}
	})
}
*/

func TestOrderByHelpers(t *testing.T) {
	clauses := []OrderByClause{
		{Field: "created_at", Direction: "desc"},
		{Field: "name", Direction: "asc"},
	}

	t.Run("PrimaryField", func(t *testing.T) {
		field := PrimaryField(clauses)
		if field != "created_at" {
			t.Errorf("PrimaryField() = %v, want %v", field, "created_at")
		}
	})

	t.Run("PrimaryDirection", func(t *testing.T) {
		direction := PrimaryDirection(clauses)
		if direction != "desc" {
			t.Errorf("PrimaryDirection() = %v, want %v", direction, "desc")
		}
	})

	t.Run("IsFieldOrdered", func(t *testing.T) {
		if !IsFieldOrdered(clauses, "name") {
			t.Error("IsFieldOrdered() should return true for 'name'")
		}
		if IsFieldOrdered(clauses, "invalid") {
			t.Error("IsFieldOrdered() should return false for invalid field")
		}
	})

	t.Run("ReverseDirection", func(t *testing.T) {
		reversed := ReverseDirection(clauses)
		if PrimaryDirection(reversed) != "asc" {
			t.Error("Reversed direction should be 'asc'")
		}
	})

	t.Run("OrderByToString", func(t *testing.T) {
		str := OrderByToString(clauses)
		if str == "" {
			t.Error("OrderByToString() should not return empty string")
		}
	})
}

func TestFilterHelpers(t *testing.T) {
	filter, _ := ParseFilter("status = \"active\" AND age > 18")

	t.Run("GetFilterValue", func(t *testing.T) {
		value, ok := GetFilterValue(filter, "status")
		if !ok {
			t.Error("GetFilterValue() should find 'status' field")
		}
		if value != "active" {
			t.Errorf("GetFilterValue() = %v, want %v", value, "active")
		}
	})

	t.Run("HasFilterField", func(t *testing.T) {
		if !HasFilterField(filter, "status") {
			t.Error("HasFilterField() should return true for 'status'")
		}
		if HasFilterField(filter, "invalid") {
			t.Error("HasFilterField() should return false for invalid field")
		}
	})

	t.Run("GetExpressionCount", func(t *testing.T) {
		count := GetExpressionCount(filter)
		if count != 2 {
			t.Errorf("GetExpressionCount() = %v, want %v", count, 2)
		}
	})

	t.Run("FilterToString", func(t *testing.T) {
		str := FilterToString(filter)
		if str == "" {
			t.Error("FilterToString() should not return empty string")
		}
	})
}

func TestBuildPaginationInfo(t *testing.T) {
	params := ListParams{PageSize: 50, Offset: 50}

	t.Run("With total count", func(t *testing.T) {
		info := BuildPaginationInfo(params, 150, true)

		if info.CurrentPage != 2 {
			t.Errorf("CurrentPage = %v, want %v", info.CurrentPage, 2)
		}

		if info.TotalPages != 3 {
			t.Errorf("TotalPages = %v, want %v", info.TotalPages, 3)
		}

		if !info.HasNext {
			t.Error("HasNext should be true")
		}

		if !info.HasPrevious {
			t.Error("HasPrevious should be true")
		}
	})

	t.Run("First page", func(t *testing.T) {
		params := ListParams{PageSize: 50, Offset: 0}
		info := BuildPaginationInfo(params, 100, true)

		if info.CurrentPage != 1 {
			t.Errorf("CurrentPage = %v, want %v", info.CurrentPage, 1)
		}

		if info.HasPrevious {
			t.Error("HasPrevious should be false for first page")
		}
	})
}

func TestNormalizePageSize(t *testing.T) {
	tests := []struct {
		input    int32
		expected int32
	}{
		{0, DefaultPageSize},
		{-10, DefaultPageSize},
		{50, 50},
		{2000, MaxPageSize},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := NormalizePageSize(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizePageSize(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestPaginationCalculations(t *testing.T) {
	t.Run("CalculateTotalPages", func(t *testing.T) {
		tests := []struct {
			totalCount int
			pageSize   int32
			expected   int
		}{
			{0, 50, 0},
			{50, 50, 1},
			{51, 50, 2},
			{100, 50, 2},
			{101, 50, 3},
		}

		for _, tt := range tests {
			result := CalculateTotalPages(tt.totalCount, tt.pageSize)
			if result != tt.expected {
				t.Errorf("CalculateTotalPages(%d, %d) = %d, want %d",
					tt.totalCount, tt.pageSize, result, tt.expected)
			}
		}
	})

	t.Run("CalculateCurrentPage", func(t *testing.T) {
		tests := []struct {
			offset   int
			pageSize int32
			expected int
		}{
			{0, 50, 1},
			{50, 50, 2},
			{100, 50, 3},
		}

		for _, tt := range tests {
			result := CalculateCurrentPage(tt.offset, tt.pageSize)
			if result != tt.expected {
				t.Errorf("CalculateCurrentPage(%d, %d) = %d, want %d",
					tt.offset, tt.pageSize, result, tt.expected)
			}
		}
	})
}
