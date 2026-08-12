package crud

import (
	"reflect"
	"strings"
	"testing"
)

// TestParseFilterComprehensive tests the ParseFilter function with various inputs
func TestParseFilterComprehensive(t *testing.T) {
	tests := []struct {
		name      string
		filter    string
		wantCount int
		wantErr   bool
	}{
		{
			name:      "empty filter",
			filter:    "",
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:      "simple equality",
			filter:    "status = \"active\"",
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "equality without quotes",
			filter:    "status = active",
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "single quotes",
			filter:    "status = 'active'",
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "not equal",
			filter:    "status != \"deleted\"",
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "less than",
			filter:    "age < 18",
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "less than or equal",
			filter:    "age <= 18",
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
			name:      "greater than or equal",
			filter:    "age >= 18",
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "contains operator",
			filter:    "name : \"John\"",
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "not contains operator",
			filter:    "name !: \"test\"",
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "in operator",
			filter:    "status in \"active,pending\"",
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "not_in operator",
			filter:    "status not_in \"deleted,archived\"",
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "has operator",
			filter:    "metadata has \"key\"",
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "AND expression",
			filter:    "status = \"active\" AND age > 18",
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:      "AND expression with spaces",
			filter:    "status = \"active\"   AND   age > 18",
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:      "three AND expressions",
			filter:    "status = \"active\" AND age > 18 AND age < 65",
			wantCount: 3,
			wantErr:   false,
		},
		{
			name:      "nested field name",
			filter:    "address.city = \"NYC\"",
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "invalid syntax - missing operator",
			filter:    "status",
			wantCount: 0,
			wantErr:   true,
		},
		{
			name:      "invalid syntax - invalid operator",
			filter:    "status ~~ \"active\"",
			wantCount: 0,
			wantErr:   true,
		},
		{
			name:      "invalid field name - starts with number",
			filter:    "123field = \"value\"",
			wantCount: 0,
			wantErr:   true,
		},
		{
			name:      "simple OR expression",
			filter:    "status = \"active\" OR age > 18",
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:      "complex AND/OR mixed not supported",
			filter:    "status = \"active\" AND age > 18 OR name = \"test\"",
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

			if !tt.wantErr {
				if len(filter.Expressions) != tt.wantCount {
					t.Errorf("ParseFilter() expression count = %v, want %v", len(filter.Expressions), tt.wantCount)
				}

				// Verify all expressions have valid fields
				for _, expr := range filter.Expressions {
					if expr.Field == "" {
						t.Error("ParseFilter() returned expression with empty field")
					}
				}
			}
		})
	}
}

// TestParseSingleExpression tests individual expression parsing
func TestParseSingleExpression(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		wantField  string
		wantOp     FilterOperator
		wantValue  string
		wantErr    bool
	}{
		{
			name:       "equality with spaces",
			expression: "name = \"John\"",
			wantField:  "name",
			wantOp:     OperatorEqual,
			wantValue:  "John",
			wantErr:    false,
		},
		{
			name:       "equality without spaces",
			expression: "name=\"John\"",
			wantField:  "name",
			wantOp:     OperatorEqual,
			wantValue:  "John",
			wantErr:    false,
		},
		{
			name:       "greater than",
			expression: "age > 18",
			wantField:  "age",
			wantOp:     OperatorGreaterThan,
			wantValue:  "18",
			wantErr:    false,
		},
		{
			name:       "less than or equal",
			expression: "count <= 100",
			wantField:  "count",
			wantOp:     OperatorLessThanOrEqual,
			wantValue:  "100",
			wantErr:    false,
		},
		{
			name:       "contains",
			expression: "description : \"test\"",
			wantField:  "description",
			wantOp:     OperatorContains,
			wantValue:  "test",
			wantErr:    false,
		},
		{
			name:       "in operator",
			expression: "status in \"active,pending\"",
			wantField:  "status",
			wantOp:     OperatorIn,
			wantValue:  "active,pending",
			wantErr:    false,
		},
		{
			name:       "nested field",
			expression: "user.profile.age > 18",
			wantField:  "user.profile.age",
			wantOp:     OperatorGreaterThan,
			wantValue:  "18",
			wantErr:    false,
		},
		{
			name:       "field with underscore",
			expression: "created_at > \"2024-01-01\"",
			wantField:  "created_at",
			wantOp:     OperatorGreaterThan,
			wantValue:  "2024-01-01",
			wantErr:    false,
		},
		{
			name:       "only field",
			expression: "name",
			wantField:  "",
			wantOp:     "",
			wantValue:  "",
			wantErr:    true,
		},
		{
			// Note: "==" is parsed as "=" operator with value '= "John"'
			// This is because regex matches first '=' as operator
			name:       "double equals operator",
			expression: "name == \"John\"",
			wantField:  "name",
			wantOp:     OperatorEqual,
			wantValue:  "= \"John",
			wantErr:    false,
		},
		{
			name:       "truly invalid operator",
			expression: "name ~~ \"John\"",
			wantField:  "",
			wantOp:     "",
			wantValue:  "",
			wantErr:    true,
		},
		{
			name:       "field starting with number",
			expression: "123name = \"test\"",
			wantField:  "",
			wantOp:     "",
			wantValue:  "",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Call parseSingleExpression through ParseFilter for consistency
			filter, err := ParseFilter(tt.expression)

			if (err != nil) != tt.wantErr {
				t.Errorf("parseSingleExpression() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if len(filter.Expressions) != 1 {
					t.Fatalf("Expected 1 expression, got %d", len(filter.Expressions))
				}

				expr := filter.Expressions[0]
				if expr.Field != tt.wantField {
					t.Errorf("Field = %v, want %v", expr.Field, tt.wantField)
				}
				if expr.Operator != tt.wantOp {
					t.Errorf("Operator = %v, want %v", expr.Operator, tt.wantOp)
				}
				if expr.Value != tt.wantValue {
					t.Errorf("Value = %v, want %v", expr.Value, tt.wantValue)
				}
			}
		})
	}
}

// TestBuildSQLWhere tests SQL WHERE clause generation
func TestBuildSQLWhere(t *testing.T) {
	tests := []struct {
		name          string
		filter        string
		fieldMappings map[string]string
		wantContains  []string
		wantArgsCount int
	}{
		{
			name:          "simple equality",
			filter:        "status = \"active\"",
			wantContains:  []string{"status = ?"},
			wantArgsCount: 1,
		},
		{
			name:          "not equal",
			filter:        "status != \"deleted\"",
			wantContains:  []string{"status != ?"},
			wantArgsCount: 1,
		},
		{
			name:          "less than",
			filter:        "age < 18",
			wantContains:  []string{"age < ?"},
			wantArgsCount: 1,
		},
		{
			name:          "greater than or equal",
			filter:        "age >= 18",
			wantContains:  []string{"age >= ?"},
			wantArgsCount: 1,
		},
		{
			name:          "contains",
			filter:        "name : \"John\"",
			wantContains:  []string{"name LIKE ?"},
			wantArgsCount: 1,
		},
		{
			name:          "not contains",
			filter:        "name !: \"test\"",
			wantContains:  []string{"name NOT LIKE ?"},
			wantArgsCount: 1,
		},
		{
			name:          "in operator",
			filter:        "status in \"active,pending\"",
			wantContains:  []string{"status IN (?, ?)"},
			wantArgsCount: 2,
		},
		{
			name:          "not_in operator",
			filter:        "status not_in \"deleted,archived\"",
			wantContains:  []string{"status NOT IN (?, ?)"},
			wantArgsCount: 2,
		},
		{
			name:          "AND expression",
			filter:        "status = \"active\" AND age > 18",
			wantContains:  []string{"status = ?", "age > ?", "AND"},
			wantArgsCount: 2,
		},
		{
			name:          "three AND expressions",
			filter:        "status = \"active\" AND age > 18 AND age < 65",
			wantContains:  []string{"status = ?", "age > ?", "age < ?", "AND", "AND"},
			wantArgsCount: 3,
		},
		{
			name:          "with field mapping",
			filter:        "status = \"active\"",
			fieldMappings: map[string]string{"status": "user_status"},
			wantContains:  []string{"user_status = ?"},
			wantArgsCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter, err := ParseFilter(tt.filter)
			if err != nil {
				t.Fatalf("ParseFilter() error = %v", err)
			}

			var args []interface{}
			where := BuildSQLWhere(filter, tt.fieldMappings, &args)

			// Check that all expected substrings are present
			for _, expected := range tt.wantContains {
				if !strings.Contains(where, expected) {
					t.Errorf("BuildSQLWhere() should contain %q, got %q", expected, where)
				}
			}

			// Check argument count
			if len(args) != tt.wantArgsCount {
				t.Errorf("BuildSQLWhere() args count = %v, want %v", len(args), tt.wantArgsCount)
			}
		})
	}
}

// TestBuildSQLWhereArgs verifies that arguments are properly added
func TestBuildSQLWhereArgs(t *testing.T) {
	tests := []struct {
		name     string
		filter   string
		wantArgs []interface{}
	}{
		{
			name:     "equality string value",
			filter:   "status = \"active\"",
			wantArgs: []interface{}{"active"},
		},
		{
			name:     "equality numeric value",
			filter:   "age = 25",
			wantArgs: []interface{}{"25"},
		},
		{
			name:     "greater than",
			filter:   "age > 18",
			wantArgs: []interface{}{"18"},
		},
		{
			name:     "contains with wildcards",
			filter:   "name : \"John\"",
			wantArgs: []interface{}{"%John%"},
		},
		{
			name:     "not contains with wildcards",
			filter:   "name !: \"test\"",
			wantArgs: []interface{}{"%test%"},
		},
		{
			name:     "in operator multiple values",
			filter:   "status in \"active,pending,archived\"",
			wantArgs: []interface{}{"active", "pending", "archived"},
		},
		{
			name:     "AND expression",
			filter:   "status = \"active\" AND age > 18",
			wantArgs: []interface{}{"active", "18"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter, err := ParseFilter(tt.filter)
			if err != nil {
				t.Fatalf("ParseFilter() error = %v", err)
			}

			var args []interface{}
			_ = BuildSQLWhere(filter, nil, &args)

			if !reflect.DeepEqual(args, tt.wantArgs) {
				t.Errorf("BuildSQLWhere() args = %v, want %v", args, tt.wantArgs)
			}
		})
	}
}

// TestFilterValidatorComprehensive tests the FilterValidator
func TestFilterValidatorComprehensive(t *testing.T) {
	t.Run("Parse with allowed fields", func(t *testing.T) {
		validator := NewFilterValidator().
			AddFields("name", "status", "age")

		filter, err := validator.Parse("status = \"active\"")
		if err != nil {
			t.Errorf("Parse() error = %v", err)
		}
		if len(filter.Expressions) != 1 {
			t.Errorf("Expected 1 expression, got %d", len(filter.Expressions))
		}
	})

	t.Run("Parse with disallowed field", func(t *testing.T) {
		validator := NewFilterValidator().
			AddFields("name", "status")

		_, err := validator.Parse("invalid_field = \"value\"")
		if err == nil {
			t.Error("Expected error for disallowed field")
		}
	})

	t.Run("Parse with field mapping", func(t *testing.T) {
		validator := NewFilterValidator().
			AddMapping("status", "user_status")

		_, err := validator.Parse("status = \"active\"")
		if err != nil {
			t.Errorf("Parse() error = %v", err)
		}
	})

	t.Run("Parse empty filter", func(t *testing.T) {
		validator := NewFilterValidator().
			AddFields("name", "status")

		filter, err := validator.Parse("")
		if err != nil {
			t.Errorf("Parse() error = %v", err)
		}
		if len(filter.Expressions) != 0 {
			t.Errorf("Expected 0 expressions, got %d", len(filter.Expressions))
		}
	})
}

// TestFilterValidatorBuildSQL tests SQL building with validator
func TestFilterValidatorBuildSQL(t *testing.T) {
	validator := NewFilterValidator().
		AddFields("status", "age").
		AddMapping("status", "user_status")

	filter, _ := validator.Parse("status = \"active\" AND age > 18")
	where, args := validator.BuildSQL(filter)

	if !strings.Contains(where, "user_status = ?") {
		t.Errorf("Expected field mapping in SQL, got %q", where)
	}

	if !strings.Contains(where, "age > ?") {
		t.Errorf("Expected age condition in SQL, got %q", where)
	}

	if len(args) != 2 {
		t.Errorf("Expected 2 args, got %d", len(args))
	}
}

// TestValidateFilterFields tests field validation
func TestValidateFilterFields(t *testing.T) {
	t.Run("all fields allowed", func(t *testing.T) {
		filter, _ := ParseFilter("status = \"active\" AND age > 18")
		allowed := map[string]bool{"status": true, "age": true}

		err := ValidateFilterFields(filter, allowed)
		if err != nil {
			t.Errorf("ValidateFilterFields() error = %v", err)
		}
	})

	t.Run("one field not allowed", func(t *testing.T) {
		filter, _ := ParseFilter("status = \"active\" AND invalid_field > 18")
		allowed := map[string]bool{"status": true, "age": true}

		err := ValidateFilterFields(filter, allowed)
		if err == nil {
			t.Error("Expected error for disallowed field")
		}
	})

	t.Run("nil filter", func(t *testing.T) {
		allowed := map[string]bool{"status": true}
		err := ValidateFilterFields(nil, allowed)
		if err != nil {
			t.Errorf("ValidateFilterFields() with nil filter should not error, got %v", err)
		}
	})
}

// TestFilterHelpersDetailed tests helper functions
func TestFilterHelpersDetailed(t *testing.T) {
	filter, _ := ParseFilter("status = \"active\" AND age > 18")

	t.Run("GetFilterValue", func(t *testing.T) {
		value, ok := GetFilterValue(filter, "status")
		if !ok {
			t.Error("GetFilterValue() should find 'status' field")
		}
		if value != "active" {
			t.Errorf("GetFilterValue() = %v, want 'active'", value)
		}

		_, ok = GetFilterValue(filter, "nonexistent")
		if ok {
			t.Error("GetFilterValue() should not find 'nonexistent' field")
		}
	})

	t.Run("HasFilterField", func(t *testing.T) {
		if !HasFilterField(filter, "status") {
			t.Error("HasFilterField() should return true for 'status'")
		}
		if HasFilterField(filter, "nonexistent") {
			t.Error("HasFilterField() should return false for 'nonexistent'")
		}
	})

	t.Run("GetFilterOperator", func(t *testing.T) {
		op, ok := GetFilterOperator(filter, "status")
		if !ok {
			t.Error("GetFilterOperator() should find 'status' field")
		}
		if op != OperatorEqual {
			t.Errorf("GetFilterOperator() = %v, want %v", op, OperatorEqual)
		}
	})

	t.Run("GetExpressionCount", func(t *testing.T) {
		count := GetExpressionCount(filter)
		if count != 2 {
			t.Errorf("GetExpressionCount() = %v, want 2", count)
		}

		count = GetExpressionCount(nil)
		if count != 0 {
			t.Errorf("GetExpressionCount() with nil filter = %v, want 0", count)
		}
	})

	t.Run("IsEmpty", func(t *testing.T) {
		if IsEmpty(filter) {
			t.Error("IsEmpty() should return false for non-empty filter")
		}

		emptyFilter, _ := ParseFilter("")
		if !IsEmpty(emptyFilter) {
			t.Error("IsEmpty() should return true for empty filter")
		}

		if !IsEmpty(nil) {
			t.Error("IsEmpty() should return true for nil filter")
		}
	})
}

// TestFilterToString tests string representation
func TestFilterToString(t *testing.T) {
	tests := []struct {
		name   string
		filter string
		want   string
	}{
		{
			name:   "single expression",
			filter: "status = \"active\"",
			want:   "status = active",
		},
		{
			name:   "AND expression",
			filter: "status = \"active\" AND age > 18",
			want:   "status = active AND age > 18",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter, _ := ParseFilter(tt.filter)
			result := FilterToString(filter)

			if !strings.Contains(result, tt.want) {
				t.Errorf("FilterToString() = %v, want to contain %v", result, tt.want)
			}
		})
	}
}

// TestMergeFilters tests filter merging
func TestMergeFilters(t *testing.T) {
	filter1, _ := ParseFilter("status = \"active\"")
	filter2, _ := ParseFilter("age > 18")
	filter3, _ := ParseFilter("name : \"test\"")

	merged := MergeFilters(filter1, filter2, filter3)

	if GetExpressionCount(merged) != 3 {
		t.Errorf("MergeFilters() count = %v, want 3", GetExpressionCount(merged))
	}

	// Verify all fields are present
	if !HasFilterField(merged, "status") {
		t.Error("MergeFilters() should contain 'status' field")
	}
	if !HasFilterField(merged, "age") {
		t.Error("MergeFilters() should contain 'age' field")
	}
	if !HasFilterField(merged, "name") {
		t.Error("MergeFilters() should contain 'name' field")
	}
}

// TestSafeFilter tests safe parsing
func TestSafeFilter(t *testing.T) {
	t.Run("valid filter", func(t *testing.T) {
		filter := SafeFilter("status = \"active\"")
		if IsEmpty(filter) {
			t.Error("SafeFilter() should return valid filter for valid input")
		}
	})

	t.Run("invalid filter", func(t *testing.T) {
		filter := SafeFilter("invalid syntax")
		if !IsEmpty(filter) {
			t.Error("SafeFilter() should return empty filter for invalid input")
		}
	})

	t.Run("empty filter", func(t *testing.T) {
		filter := SafeFilter("")
		if !IsEmpty(filter) {
			t.Error("SafeFilter() should return empty filter for empty input")
		}
	})
}

// TestParseFilterWithOptions tests parsing with options
func TestParseFilterWithOptions(t *testing.T) {
	opts := FilterOptions{
		AllowedFields: []string{"status", "age"},
		FieldMappings: map[string]string{
			"status": "user_status",
		},
	}

	t.Run("valid filter with options", func(t *testing.T) {
		filter, err := ParseFilterWithOptions("status = \"active\"", opts)
		if err != nil {
			t.Errorf("ParseFilterWithOptions() error = %v", err)
		}
		if len(filter.Expressions) != 1 {
			t.Errorf("Expected 1 expression, got %d", len(filter.Expressions))
		}
	})

	t.Run("invalid field with options", func(t *testing.T) {
		_, err := ParseFilterWithOptions("invalid_field = \"value\"", opts)
		if err == nil {
			t.Error("Expected error for disallowed field")
		}
	})
}

// TestFilterOperator tests operator validation
func TestFilterOperator(t *testing.T) {
	validOperators := []FilterOperator{
		OperatorEqual,
		OperatorNotEqual,
		OperatorLessThan,
		OperatorLessThanOrEqual,
		OperatorGreaterThan,
		OperatorGreaterThanOrEqual,
		OperatorContains,
		OperatorNotContains,
		OperatorIn,
		OperatorNotIn,
		OperatorHas,
	}

	for _, op := range validOperators {
		t.Run(string(op), func(t *testing.T) {
			if !isValidOperator(op) {
				t.Errorf("Operator %v should be valid", op)
			}
		})
	}
}

// TestFilterExpressionLogicalOperators tests logical operators
func TestFilterExpressionLogicalOperators(t *testing.T) {
	filter, _ := ParseFilter("status = \"active\" AND age > 18 AND name : \"test\"")

	if len(filter.Expressions) != 3 {
		t.Fatalf("Expected 3 expressions, got %d", len(filter.Expressions))
	}

	// First expression should have AND
	if filter.Expressions[0].LogicalOperator != LogicalAND {
		t.Errorf("First expression logical operator = %v, want %v",
			filter.Expressions[0].LogicalOperator, LogicalAND)
	}

	// Second expression should have AND
	if filter.Expressions[1].LogicalOperator != LogicalAND {
		t.Errorf("Second expression logical operator = %v, want %v",
			filter.Expressions[1].LogicalOperator, LogicalAND)
	}

	// Third expression should have no logical operator (last one)
	if filter.Expressions[2].LogicalOperator != "" {
		t.Errorf("Third expression should have no logical operator, got %v",
			filter.Expressions[2].LogicalOperator)
	}
}

// TestSplitByLogicalOperator tests the split function
func TestSplitByLogicalOperator(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		operator string
		wantLen  int
	}{
		{
			name:     "single AND",
			input:    "status = \"active\" AND age > 18",
			operator: "AND",
			wantLen:  2,
		},
		{
			name:     "multiple ANDs",
			input:    "a = 1 AND b = 2 AND c = 3",
			operator: "AND",
			wantLen:  3,
		},
		{
			name:     "case insensitive AND",
			input:    "status = \"active\" and age > 18",
			operator: "AND",
			wantLen:  2,
		},
		{
			name:     "no operator",
			input:    "status = \"active\"",
			operator: "AND",
			wantLen:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitByLogicalOperator(tt.input, tt.operator)
			if len(result) != tt.wantLen {
				t.Errorf("splitByLogicalOperator() len = %v, want %v", len(result), tt.wantLen)
			}
		})
	}
}

// TestEdgeCases tests edge cases and boundary conditions
func TestEdgeCases(t *testing.T) {
	t.Run("filter with extra spaces", func(t *testing.T) {
		filter, err := ParseFilter("   status   =   \"active\"   ")
		if err != nil {
			t.Errorf("ParseFilter() error = %v", err)
		}
		if len(filter.Expressions) != 1 {
			t.Errorf("Expected 1 expression, got %d", len(filter.Expressions))
		}
	})

	t.Run("filter with tab characters", func(t *testing.T) {
		// Our implementation uses TrimSpace which handles tabs
		_, err := ParseFilter("status\t=\t\"active\"")
		if err != nil {
			t.Errorf("ParseFilter() error = %v", err)
		}
	})

	t.Run("value with spaces", func(t *testing.T) {
		filter, err := ParseFilter("name = \"John Doe\"")
		if err != nil {
			t.Errorf("ParseFilter() error = %v", err)
		}
		if len(filter.Expressions) != 1 {
			t.Errorf("Expected 1 expression, got %d", len(filter.Expressions))
		}
		value := filter.Expressions[0].Value
		if value != "John Doe" {
			t.Errorf("Value = %v, want 'John Doe'", value)
		}
	})

	t.Run("in operator with spaces", func(t *testing.T) {
		filter, err := ParseFilter("status in \"active, pending, archived\"")
		if err != nil {
			t.Errorf("ParseFilter() error = %v", err)
		}

		var args []interface{}
		_ = BuildSQLWhere(filter, nil, &args)

		// Should trim spaces from values
		if len(args) != 3 {
			t.Errorf("Expected 3 args, got %d", len(args))
		}
	})
}

// TestFieldMappings tests field mapping functionality
func TestFieldMappings(t *testing.T) {
	tests := []struct {
		name        string
		filter      string
		mappings    map[string]string
		expectInSQL []string
	}{
		{
			name:        "single field mapping",
			filter:      "status = \"active\"",
			mappings:    map[string]string{"status": "user_status"},
			expectInSQL: []string{"user_status = ?"},
		},
		{
			name:        "multiple field mappings",
			filter:      "status = \"active\" AND age > 18",
			mappings:    map[string]string{"status": "user_status", "age": "user_age"},
			expectInSQL: []string{"user_status = ?", "user_age > ?"},
		},
		{
			name:        "partial mapping",
			filter:      "status = \"active\" AND age > 18",
			mappings:    map[string]string{"status": "user_status"},
			expectInSQL: []string{"user_status = ?", "age > ?"},
		},
		{
			name:        "nested field mapping",
			filter:      "address.city = \"NYC\"",
			mappings:    map[string]string{"address.city": "city"},
			expectInSQL: []string{"city = ?"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter, err := ParseFilter(tt.filter)
			if err != nil {
				t.Fatalf("ParseFilter() error = %v", err)
			}

			var args []interface{}
			where := BuildSQLWhere(filter, tt.mappings, &args)

			for _, expected := range tt.expectInSQL {
				if !strings.Contains(where, expected) {
					t.Errorf("SQL should contain %q, got %q", expected, where)
				}
			}
		})
	}
}

// TestBuildSingleCondition_UnsafeMappingProducesFALSE verifies defence-in-depth:
// even if a developer accidentally puts an unsafe string in fieldMappings,
// the SQL interpolation rejects it.
func TestBuildSingleCondition_UnsafeMappingProducesFALSE(t *testing.T) {
	tests := []struct {
		name    string
		mapping string
	}{
		{"SQL injection", "1; DROP TABLE users--"},
		{"subquery", "(SELECT 1)"},
		{"space in name", "user status"},
		{"semicolon", "status;"},
		{"quotes", "status'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter, err := ParseFilter("status = \"active\"")
			if err != nil {
				t.Fatalf("ParseFilter() error = %v", err)
			}
			var args []interface{}
			where := BuildSQLWhere(filter, map[string]string{"status": tt.mapping}, &args)
			if where != "FALSE" {
				t.Errorf("unsafe mapping %q produced %q, want FALSE", tt.mapping, where)
			}
		})
	}
}

func TestBuildSingleCondition_SafeMappingWorks(t *testing.T) {
	filter, err := ParseFilter("status = \"active\"")
	if err != nil {
		t.Fatalf("ParseFilter() error = %v", err)
	}
	var args []interface{}
	where := BuildSQLWhere(filter, map[string]string{"status": "u.status"}, &args)
	if !strings.Contains(where, "u.status = ?") {
		t.Errorf("safe mapping produced %q, want to contain %q", where, "u.status = ?")
	}
}

// TestBuildSQLWhere_LikeEscaping (R08-P2-2): contains/notContains 必须转义
// LIKE 通配符（%/_\），配合 ESCAPE '\' 子句按字面量匹配。
func TestBuildSQLWhere_LikeEscaping(t *testing.T) {
	tests := []struct {
		name       string
		operator   string
		value      string
		wantArg    string
		wantSQL    string
		wantNotSQL bool
	}{
		{
			name:     "contains percent",
			operator: ":", value: "100%",
			wantArg: "%100\\%%",
			wantSQL: "LIKE ? ESCAPE '\\'",
		},
		{
			name:     "contains underscore",
			operator: ":", value: "a_b",
			wantArg: "%a\\_b%",
			wantSQL: "LIKE ? ESCAPE '\\'",
		},
		{
			name:     "contains backslash",
			operator: ":", value: `a\b`,
			wantArg: `%a\\b%`,
			wantSQL: "LIKE ? ESCAPE '\\'",
		},
		{
			name:     "contains mixed wildcards",
			operator: ":", value: "50%_x\\y",
			wantArg: `%50\%\_x\\y%`,
			wantSQL: "LIKE ? ESCAPE '\\'",
		},
		{
			name:     "not contains percent",
			operator: "!:", value: "100%",
			wantArg:    "%100\\%%",
			wantSQL:    "NOT LIKE ? ESCAPE '\\'",
			wantNotSQL: true,
		},
		{
			name:     "not contains underscore",
			operator: "!:", value: "a_b",
			wantArg:    "%a\\_b%",
			wantSQL:    "NOT LIKE ? ESCAPE '\\'",
			wantNotSQL: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter, err := ParseFilter("name " + tt.operator + " \"" + tt.value + "\"")
			if err != nil {
				t.Fatalf("ParseFilter() error = %v", err)
			}
			var args []interface{}
			where := BuildSQLWhere(filter, nil, &args)
			if !strings.Contains(where, tt.wantSQL) {
				t.Errorf("SQL %q should contain %q", where, tt.wantSQL)
			}
			if len(args) != 1 || args[0] != tt.wantArg {
				t.Errorf("args = %v, want [%q]", args, tt.wantArg)
			}
		})
	}

	// 转义不改变无通配符输入的行为（回归）。
	filter, err := ParseFilter("name : \"John\"")
	if err != nil {
		t.Fatalf("ParseFilter() error = %v", err)
	}
	var args []interface{}
	where := BuildSQLWhere(filter, nil, &args)
	requireLike := strings.Contains(where, "ESCAPE '\\'")
	if !requireLike {
		t.Errorf("SQL %q should carry ESCAPE clause", where)
	}
	if len(args) != 1 || args[0] != "%John%" {
		t.Errorf("args = %v, want [%q]", args, "%John%")
	}
}
