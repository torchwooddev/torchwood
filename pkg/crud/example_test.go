package crud

import (
	"context"
	"fmt"
)

// This file contains usage examples for the restapi package
// following Google AIP standards (AIP-132, AIP-158, AIP-160)

// Example 1: Basic List parameter parsing
func Example_parseListParams() {
	// Parse basic list parameters
	params, err := ParseListParams(50, "", "status = \"active\"", "created_at desc")
	if err != nil {
		panic(err)
	}

	fmt.Printf("PageSize: %d\n", params.PageSize)
	fmt.Printf("Filter: %s\n", params.Filter)
	fmt.Printf("OrderBy: %s\n", params.OrderBy)
}

// Example 2: Pagination with page tokens
func Example_pagination() {
	// First page request
	params, err := ParseListParams(50, "", "", "")
	if err != nil {
		panic(err)
	}

	offset, limit := params.OffsetLimit()
	fmt.Printf("First page: OFFSET %d LIMIT %d\n", offset, limit)

	// After getting results, generate next page token
	resultCount := 50
	nextToken := EncodePageToken(offset + resultCount)
	fmt.Printf("Next page token: %s\n", nextToken)

	// Second page request using token
	params2, err := ParseListParams(50, nextToken, "", "")
	if err != nil {
		panic(err)
	}

	offset2, limit2 := params2.OffsetLimit()
	fmt.Printf("Second page: OFFSET %d LIMIT %d\n", offset2, limit2)
}

// Example 3: Order by parsing
func Example_parseOrderBy() {
	// Parse order by clause
	clauses, err := ParseOrderBy("created_at desc, name asc")
	if err != nil {
		panic(err)
	}

	for _, clause := range clauses {
		fmt.Printf("Field: %s, Direction: %s\n", clause.Field, clause.Direction)
	}

	// Build SQL order by clause
	fieldMappings := map[string]string{
		"created_at": "created_at",
		"name":       "display_name",
	}
	sql := BuildOrderByClause(clauses, fieldMappings)
	fmt.Printf("SQL: ORDER BY %s\n", sql)
}

// Example 4: Filter parsing
func Example_parseFilter() {
	// Parse filter expression
	filter, err := ParseFilter("status = \"active\" AND age > 18")
	if err != nil {
		panic(err)
	}

	for _, expr := range filter.Expressions {
		fmt.Printf("Field: %s, Operator: %s, Value: %s\n",
			expr.Field, expr.Operator, expr.Value)
	}

	// Build SQL WHERE clause
	fieldMappings := map[string]string{
		"status": "status",
		"age":    "user_age",
	}
	var args []interface{}
	where := BuildSQLWhere(filter, fieldMappings, &args)
	fmt.Printf("SQL WHERE: %s\n", where)
	fmt.Printf("Args: %v\n", args)
}

// Example 5: Complete List request handling
func Example_completeListRequest() {
	ctx := context.Background()

	// Parse complete list request
	_, params, err := ParseListRequest(
		ctx,
		50,                    // page_size
		"",                    // page_token (first page)
		"status = \"active\"", // filter
		"created_at desc",     // order_by
	)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Parsed params: %s\n", params.String())

	// Use parameters for database query
	offset, limit := params.OffsetLimit()
	fmt.Printf("Query: OFFSET %d LIMIT %d\n", offset, limit)
}

// Example 6: Using validators for field validation
func Example_fieldValidation() {
	// Create order by validator with allowed fields
	orderValidator := NewOrderByValidator().
		AddField("name").
		AddField("created_at").
		AddField("updated_at").
		SetDefault(
			OrderByClause{Field: "created_at", Direction: "desc"},
		)

	// Parse and validate order by
	clauses, err := orderValidator.Parse("name asc")
	if err != nil {
		panic(err)
	}
	fmt.Printf("Validated order by: %+v\n", clauses)

	// Create filter validator with allowed fields
	filterValidator := NewFilterValidator().
		AddField("name").
		AddField("status").
		AddMapping("created_at", "created_at_ms")

	// Parse and validate filter
	filter, err := filterValidator.Parse("status = \"active\"")
	if err != nil {
		panic(err)
	}
	fmt.Printf("Validated filter: %s\n", FilterToString(filter))
}

// Example 7: Building pagination info
func Example_buildPaginationInfo() {
	params, _ := ParseListParams(50, "", "", "")

	// With known total count
	totalCount := 250
	hasMore := true

	info := BuildPaginationInfo(params, totalCount, hasMore)
	fmt.Printf("Current page: %d\n", info.CurrentPage)
	fmt.Printf("Total pages: %d\n", info.TotalPages)
	fmt.Printf("Has next: %v\n", info.HasNext)
	fmt.Printf("Has previous: %v\n", info.HasPrevious)
}

// Example 8: gRPC service integration
func Example_gRPCIntegration() {
	// In your gRPC service handler:
	//
	// func (s *Service) ListItems(ctx context.Context, req *pb.ListItemsRequest) (*pb.ListItemsResponse, error) {
	//     // Parse list parameters
	//     _, params, err := restapi.ParseListRequest(
	//         ctx,
	//         req.PageSize,
	//         req.PageToken,
	//         req.Filter,
	//         req.OrderBy,
	//     )
	//     if err != nil {
	//         return nil, status.Error(codes.InvalidArgument, err.Error())
	//     }
	//
	//     // Parse order by with validation
	//     orderValidator := restapi.NewOrderByValidator().
	//         AddFields("name", "created_at", "updated_at").
	//         SetDefault(restapi.OrderByClause{Field: "created_at", Direction: "desc"})
	//
	//     clauses, err := orderValidator.Parse(params.OrderBy)
	//     if err != nil {
	//         return nil, status.Error(codes.InvalidArgument, err.Error())
	//     }
	//
	//     // Parse filter with validation
	//     filterValidator := restapi.NewFilterValidator().
	//         AddFields("name", "status", "group_id")
	//
	//     filter, err := filterValidator.Parse(params.Filter)
	//     if err != nil {
	//         return nil, status.Error(codes.InvalidArgument, err.Error())
	//     }
	//
	//     // Build repository filter
	//     repoFilter := buildRepoFilter(params, clauses, filter)
	//
	//     // Query repository
	//     items, total, err := s.repo.List(ctx, repoFilter)
	//     if err != nil {
	//         return nil, err
	//     }
	//
	//     // Calculate has more
	//     hasMore := restapi.CalculateHasMore(params.Offset, len(items), total)
	//
	//     // Build response
	//     return &pb.ListItemsResponse{
	//         Items:          items,
	//         NextPageToken:  restapi.EncodePageToken(params.Offset + len(items)),
	//         TotalSize:      restapi.FormatTotalCount(total),
	//     }, nil
	// }

	_ = "example code shown in comments"
}

// Example 9: Safe parsing with defaults
func Example_safeParsing() {
	// Safe parsing with fallbacks
	clauses := SafeOrderBy("invalid_field asc", "created_at")
	fmt.Printf("Safe order by: %s\n", OrderByToString(clauses))

	// Safe filter parsing
	filter := SafeFilter("invalid syntax")
	fmt.Printf("Safe filter is empty: %v\n", IsEmpty(filter))

	// Get normalized page size
	pageSize := GetPageSizeOrDefault(100)
	fmt.Printf("Page size: %d\n", pageSize)
}

// Example 10: Using FilterBuilder for programmatic filter creation
func Example_filterBuilder() {
	// Create a filter using fluent API
	filter := NewFilterBuilder().
		Equal("status", "active").
		And().
		GreaterThan("age", "18").
		Build()

	fmt.Printf("Filter: %s\n", FilterToString(filter))

	// Build complex filter with OR
	filter2 := NewFilterBuilder().
		Equal("status", "active").
		Or().
		Equal("status", "pending").
		Build()

	fmt.Printf("Filter with OR: %s\n", FilterToString(filter2))

	// Use In operator
	filter3 := NewFilterBuilder().
		In("status", "active", "pending", "suspended").
		Build()

	fmt.Printf("Filter with IN: %s\n", FilterToString(filter3))

	// Use Contains for partial matching
	filter4 := NewFilterBuilder().
		Contains("name", "John").
		And().
		NotEqual("deleted_at", "").
		Build()

	fmt.Printf("Filter with contains: %s\n", FilterToString(filter4))
}

// Example 11: FilterBuilder with validation
func Example_filterBuilderWithValidation() {
	// Build filter with field validation
	filter := NewFilterBuilder().
		Equal("status", "active").
		And().
		GreaterThan("age", "18").
		MustBuild("status", "age", "name") // allowed fields

	fmt.Printf("Validated filter: %s\n", FilterToString(filter))

	// This will panic if field is not allowed
	// Uncomment to test:
	// crud.NewFilterBuilder().
	//     Equal("invalid_field", "value").
	//     MustBuild("status", "age")
}

// Example 12: Dynamic filter building
func Example_dynamicFilterBuilding() {
	// Simulated request parameters
	type ListRequest struct {
		Status     string
		MinAge     int
		MaxAge     int
		Search     string
		Categories []string
	}

	req := ListRequest{
		Status:     "active",
		MinAge:     18,
		MaxAge:     65,
		Search:     "John",
		Categories: []string{"premium", "vip"},
	}

	// Build filter dynamically based on request
	builder := NewFilterBuilder()

	if req.Status != "" {
		builder.Equal("status", req.Status)
	}

	if req.MinAge > 0 {
		if len(builder.Build().Expressions) > 0 {
			builder.And()
		}
		builder.GreaterThanOrEqual("age", fmt.Sprintf("%d", req.MinAge))
	}

	if req.MaxAge > 0 {
		builder.And().LessThanOrEqual("age", fmt.Sprintf("%d", req.MaxAge))
	}

	if req.Search != "" {
		builder.And().Contains("name", req.Search)
	}

	if len(req.Categories) > 0 {
		builder.And().In("category", req.Categories...)
	}

	filter := builder.Build()
	fmt.Printf("Dynamic filter: %s\n", FilterToString(filter))
}

// Example 13: Convert FilterBuilder to SQL
func Example_filterBuilderToSQL() {
	// Build filter
	filter := NewFilterBuilder().
		Equal("status", "active").
		And().
		GreaterThan("created_at", "2024-01-01").
		Build()

	// Build SQL WHERE clause
	fieldMappings := map[string]string{
		"status":     "status",
		"created_at": "created_at_ms",
	}
	var args []interface{}
	where := BuildSQLWhere(filter, fieldMappings, &args)

	fmt.Printf("SQL WHERE: %s\n", where)
	fmt.Printf("Args: %v\n", args)
}

// Example 14: Chained conditions with FilterBuilder
func Example_chainedConditions() {
	// Build: (status = "active" AND age > 18) OR (status = "vip" AND age >= 21)
	// Note: Current implementation uses simple chaining
	// For complex nested conditions, use ExprWithLogical

	filter := NewFilterBuilder().
		ExprWithLogical("status", OperatorEqual, "active", LogicalAND).
		ExprWithLogical("age", OperatorGreaterThan, "18", LogicalOR).
		ExprWithLogical("status", OperatorEqual, "vip", LogicalAND).
		ExprWithLogical("age", OperatorGreaterThanOrEqual, "21", "").
		Build()

	fmt.Printf("Complex filter: %s\n", FilterToString(filter))
}
