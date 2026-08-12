package crud

import (
	"fmt"
	"regexp"
	"strings"
)

// FilterExpression represents a parsed filter expression following AIP-160
// See: https://google.aip.dev/160
type FilterExpression struct {
	// Field is the field name to filter on
	Field string `json:"field"`

	// Operator is the comparison operator
	Operator FilterOperator `json:"operator"`

	// Value is the value to compare against
	Value string `json:"value"`

	// LogicalOperator connects this expression with the next (AND/OR)
	LogicalOperator LogicalOperator `json:"logical_operator,omitempty"`
}

// FilterOperator represents comparison operators in filter expressions
type FilterOperator string

const (
	// OperatorEqual represents equality (=)
	OperatorEqual FilterOperator = "="

	// OperatorNotEqual represents inequality (!=)
	OperatorNotEqual FilterOperator = "!="

	// OperatorLessThan represents less than (<)
	OperatorLessThan FilterOperator = "<"

	// OperatorLessThanOrEqual represents less than or equal (<=)
	OperatorLessThanOrEqual FilterOperator = "<="

	// OperatorGreaterThan represents greater than (>)
	OperatorGreaterThan FilterOperator = ">"

	// OperatorGreaterThanOrEqual represents greater than or equal (>=)
	OperatorGreaterThanOrEqual FilterOperator = ">="

	// OperatorContains represents substring containment (:)
	OperatorContains FilterOperator = ":"

	// OperatorNotContains represents non-containment (!:)
	OperatorNotContains FilterOperator = "!:"

	// OperatorIn represents membership in a set
	OperatorIn FilterOperator = "in"

	// OperatorNotIn represents non-membership in a set
	OperatorNotIn FilterOperator = "not_in"

	// OperatorHas represents checking for a subfield or key presence
	OperatorHas FilterOperator = "has"
)

// LogicalOperator represents logical operators
type LogicalOperator string

const (
	// LogicalAND represents logical AND
	LogicalAND LogicalOperator = "AND"

	// LogicalOR represents logical OR
	LogicalOR LogicalOperator = "OR"
)

// Filter represents a complete filter expression tree
type Filter struct {
	Expressions []FilterExpression `json:"expressions"`
}

// ParseFilter parses a filter string following AIP-160
//
// Examples:
//
//	"name = \"John\"" -> [{Field: "name", Operator: "=", Value: "John"}]
//	"age > 18 AND age < 65" -> [{Field: "age", Operator: ">", Value: "18", LogicalOperator: "AND"}, {Field: "age", Operator: "<", Value: "65"}]
//	"status = \"active\" OR status = \"pending\"" -> [...]
func ParseFilter(filter string) (*Filter, error) {
	if filter == "" {
		return &Filter{Expressions: []FilterExpression{}}, nil
	}

	expressions, err := parseFilterExpressions(filter)
	if err != nil {
		return nil, err
	}

	return &Filter{Expressions: expressions}, nil
}

// parseFilterExpressions parses filter expressions from string.
// Supports pure AND chains, pure OR chains, and single expressions.
// Mixed AND/OR (e.g. "a=1 AND b=2 OR c=3") is not supported.
func parseFilterExpressions(filter string) ([]FilterExpression, error) {
	andParts := splitByLogicalOperator(filter, "AND")

	if len(andParts) == 1 {
		// No AND — may be a pure OR chain or a single expression.
		orParts := splitByLogicalOperator(filter, "OR")
		if len(orParts) > 1 {
			var expressions []FilterExpression
			for i, orPart := range orParts {
				expr, err := parseSingleExpression(strings.TrimSpace(orPart))
				if err != nil {
					return nil, err
				}
				if i < len(orParts)-1 {
					expr.LogicalOperator = LogicalOR
				}
				expressions = append(expressions, expr)
			}
			return expressions, nil
		}
		// Single expression
		expr, err := parseSingleExpression(strings.TrimSpace(filter))
		if err != nil {
			return nil, err
		}
		return []FilterExpression{expr}, nil
	}

	// Multiple AND parts — OR within an AND segment is not supported.
	var expressions []FilterExpression
	for i, andPart := range andParts {
		orParts := splitByLogicalOperator(andPart, "OR")
		if len(orParts) > 1 {
			return nil, fmt.Errorf("complex AND/OR combinations not yet supported")
		}
		expr, err := parseSingleExpression(strings.TrimSpace(andPart))
		if err != nil {
			return nil, err
		}
		if i < len(andParts)-1 {
			expr.LogicalOperator = LogicalAND
		}
		expressions = append(expressions, expr)
	}
	return expressions, nil
}

// splitByLogicalOperator splits string by logical operator (ignoring case)
func splitByLogicalOperator(s, op string) []string {
	// Simple implementation - split by case-insensitive operator
	// In production, use proper tokenization
	re := regexp.MustCompile(`(?i)\s+` + op + `\s+`)
	return re.Split(s, -1)
}

// parseSingleExpression parses a single filter expression
func parseSingleExpression(expr string) (FilterExpression, error) {
	// Pattern: field operator value
	// Operators can be: =, !=, <, <=, >, >=, :, !:, in, not_in, has
	re := regexp.MustCompile(`^([a-zA-Z_][a-zA-Z0-9_.]*)\s*(=|!=|<=|>=|<|>|:|!:|in|not_in|has)\s*(.+)$`)
	matches := re.FindStringSubmatch(strings.TrimSpace(expr))

	if matches == nil || len(matches) != 4 {
		return FilterExpression{}, fmt.Errorf("invalid filter expression: %s", expr)
	}

	field := matches[1]
	operator := FilterOperator(matches[2])
	value := strings.TrimSpace(matches[3])

	// Remove quotes from string values
	value = strings.Trim(value, "\"'")

	// Validate operator
	if !isValidOperator(operator) {
		return FilterExpression{}, fmt.Errorf("invalid operator: %s", operator)
	}

	// Validate field name
	if !isValidFieldName(field) {
		return FilterExpression{}, fmt.Errorf("invalid field name: %s", field)
	}

	return FilterExpression{
		Field:    field,
		Operator: operator,
		Value:    value,
	}, nil
}

// isValidOperator checks if an operator is valid
func isValidOperator(op FilterOperator) bool {
	validOperators := map[FilterOperator]bool{
		OperatorEqual:              true,
		OperatorNotEqual:           true,
		OperatorLessThan:           true,
		OperatorLessThanOrEqual:    true,
		OperatorGreaterThan:        true,
		OperatorGreaterThanOrEqual: true,
		OperatorContains:           true,
		OperatorNotContains:        true,
		OperatorIn:                 true,
		OperatorNotIn:              true,
		OperatorHas:                true,
	}
	return validOperators[op]
}

// ValidateFilterFields validates that filter fields are in the allowed list
func ValidateFilterFields(filter *Filter, allowedFields map[string]bool) error {
	if filter == nil {
		return nil
	}

	for _, expr := range filter.Expressions {
		if !allowedFields[expr.Field] {
			return fmt.Errorf("field '%s' is not allowed for filtering", expr.Field)
		}
	}
	return nil
}

// BuildSQLWhere builds a SQL WHERE clause from filter expressions
func BuildSQLWhere(filter *Filter, fieldMappings map[string]string, args *[]interface{}) string {
	if filter == nil || len(filter.Expressions) == 0 {
		return ""
	}

	var conditions []string
	for _, expr := range filter.Expressions {
		condition := buildSingleCondition(expr, fieldMappings, args)
		conditions = append(conditions, condition)
	}

	// Join with logical operators
	result := ""
	for i, condition := range conditions {
		if i > 0 {
			// Get logical operator from previous expression
			logicalOp := filter.Expressions[i-1].LogicalOperator
			if logicalOp == "" {
				logicalOp = LogicalAND // Default
			}
			result += " " + string(logicalOp) + " "
		}
		result += condition
	}

	return result
}

// safeFieldName validates that a resolved field name (possibly from fieldMappings)
// only contains safe SQL identifier characters. This is a defence-in-depth guard
// against accidental injection via developer-controlled mapping values.
var safeFieldNameRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_.]*$`)

// escapeLikePattern 转义 LIKE 模式中的通配符与转义符本身（配合 ESCAPE '\' 子句），
// 使 contains/notContains 将用户输入按字面量匹配（对齐 documentdb 的既有实现）。
func escapeLikePattern(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// IsTrustedSQLMappingFragment reports whether the resolved mapping is a full SQL
// expression built server-side (Data Platform JSON path under alias "or").
// Such fragments skip identifier-only validation; user input still flows only through bound parameters.
func IsTrustedSQLMappingFragment(s string) bool {
	if s == "" {
		return false
	}
	return strings.Contains(s, `"or".data->`)
}

// buildSingleCondition builds a single SQL condition
func buildSingleCondition(expr FilterExpression, fieldMappings map[string]string, args *[]interface{}) string {
	// Use mapped field name if provided, otherwise use original field name
	fieldSQL := expr.Field
	if mapping, ok := fieldMappings[expr.Field]; ok {
		fieldSQL = mapping
	}

	// Defence-in-depth: reject unknown expressions unless they are trusted server-built SQL.
	if !IsTrustedSQLMappingFragment(fieldSQL) && !safeFieldNameRe.MatchString(fieldSQL) {
		return "FALSE"
	}

	switch expr.Operator {
	case OperatorEqual:
		*args = append(*args, expr.Value)
		return fmt.Sprintf("%s = ?", fieldSQL)

	case OperatorNotEqual:
		*args = append(*args, expr.Value)
		return fmt.Sprintf("%s != ?", fieldSQL)

	case OperatorLessThan:
		*args = append(*args, expr.Value)
		return fmt.Sprintf("%s < ?", fieldSQL)

	case OperatorLessThanOrEqual:
		*args = append(*args, expr.Value)
		return fmt.Sprintf("%s <= ?", fieldSQL)

	case OperatorGreaterThan:
		*args = append(*args, expr.Value)
		return fmt.Sprintf("%s > ?", fieldSQL)

	case OperatorGreaterThanOrEqual:
		*args = append(*args, expr.Value)
		return fmt.Sprintf("%s >= ?", fieldSQL)

	case OperatorContains:
		*args = append(*args, "%"+escapeLikePattern(expr.Value)+"%")
		return fmt.Sprintf("%s LIKE ? ESCAPE '\\'", fieldSQL)

	case OperatorNotContains:
		*args = append(*args, "%"+escapeLikePattern(expr.Value)+"%")
		return fmt.Sprintf("%s NOT LIKE ? ESCAPE '\\'", fieldSQL)

	case OperatorIn:
		// Parse comma-separated values
		values := strings.Split(expr.Value, ",")
		placeholders := make([]string, len(values))
		for i, v := range values {
			*args = append(*args, strings.TrimSpace(v))
			placeholders[i] = "?"
		}
		return fmt.Sprintf("%s IN (%s)", fieldSQL, strings.Join(placeholders, ", "))

	case OperatorNotIn:
		values := strings.Split(expr.Value, ",")
		placeholders := make([]string, len(values))
		for i, v := range values {
			*args = append(*args, strings.TrimSpace(v))
			placeholders[i] = "?"
		}
		return fmt.Sprintf("%s NOT IN (%s)", fieldSQL, strings.Join(placeholders, ", "))

	case OperatorHas:
		// For JSON/array fields - check if key exists
		*args = append(*args, expr.Value)
		return fmt.Sprintf("JSON_CONTAINS(%s, ?)", fieldSQL)

	default:
		return ""
	}
}

// FilterValidator validates filter expressions against allowed fields
type FilterValidator struct {
	allowedFields   map[string]bool
	fieldMappings   map[string]string
	caseInsensitive bool
}

// NewFilterValidator creates a new filter validator
func NewFilterValidator() *FilterValidator {
	return &FilterValidator{
		allowedFields:   make(map[string]bool),
		fieldMappings:   make(map[string]string),
		caseInsensitive: false,
	}
}

// AddField adds an allowed field
func (v *FilterValidator) AddField(field string) *FilterValidator {
	v.allowedFields[field] = true
	return v
}

// AddFields adds multiple allowed fields
func (v *FilterValidator) AddFields(fields ...string) *FilterValidator {
	for _, field := range fields {
		v.allowedFields[field] = true
	}
	return v
}

// AddMapping adds a field mapping (proto field -> database field)
func (v *FilterValidator) AddMapping(protoField, dbField string) *FilterValidator {
	v.fieldMappings[protoField] = dbField
	v.allowedFields[protoField] = true
	return v
}

// SetCaseInsensitive enables or disables case-insensitive field matching
func (v *FilterValidator) SetCaseInsensitive(enabled bool) *FilterValidator {
	v.caseInsensitive = enabled
	return v
}

// Parse validates and parses filter string
func (v *FilterValidator) Parse(filter string) (*Filter, error) {
	if filter == "" {
		return &Filter{Expressions: []FilterExpression{}}, nil
	}

	// Parse the filter string
	f, err := ParseFilter(filter)
	if err != nil {
		return nil, err
	}

	// Validate fields are allowed
	if err := v.Validate(f); err != nil {
		return nil, err
	}

	return f, nil
}

// Validate validates filter expressions
func (v *FilterValidator) Validate(filter *Filter) error {
	return ValidateFilterFields(filter, v.allowedFields)
}

// BuildSQL builds SQL WHERE clause with field mappings
func (v *FilterValidator) BuildSQL(filter *Filter) (string, []interface{}) {
	var args []interface{}
	where := BuildSQLWhere(filter, v.fieldMappings, &args)
	return where, args
}

// FilterOptions provides configuration for filter parsing
type FilterOptions struct {
	AllowedFields   []string
	FieldMappings   map[string]string
	CaseInsensitive bool
}

// ParseFilterWithOptions parses filter with options
func ParseFilterWithOptions(filter string, opts FilterOptions) (*Filter, error) {
	// Build validator
	validator := NewFilterValidator()
	validator.AddFields(opts.AllowedFields...)

	for protoField, dbField := range opts.FieldMappings {
		validator.AddMapping(protoField, dbField)
	}

	validator.SetCaseInsensitive(opts.CaseInsensitive)

	// Parse and validate
	return validator.Parse(filter)
}

// GetFilterValue gets the value for a specific field from filter
func GetFilterValue(filter *Filter, field string) (string, bool) {
	if filter == nil {
		return "", false
	}

	for _, expr := range filter.Expressions {
		if expr.Field == field {
			return expr.Value, true
		}
	}

	return "", false
}

// HasFilterField checks if a filter has a specific field
func HasFilterField(filter *Filter, field string) bool {
	_, exists := GetFilterValue(filter, field)
	return exists
}

// GetFilterOperator gets the operator for a specific field
func GetFilterOperator(filter *Filter, field string) (FilterOperator, bool) {
	if filter == nil {
		return "", false
	}

	for _, expr := range filter.Expressions {
		if expr.Field == field {
			return expr.Operator, true
		}
	}

	return "", false
}

// MergeFilters merges multiple filters with AND logic
func MergeFilters(filters ...*Filter) *Filter {
	var allExpressions []FilterExpression

	for _, f := range filters {
		if f != nil && len(f.Expressions) > 0 {
			allExpressions = append(allExpressions, f.Expressions...)
		}
	}

	return &Filter{Expressions: allExpressions}
}

// FilterToString converts filter to string representation
func FilterToString(filter *Filter) string {
	if filter == nil || len(filter.Expressions) == 0 {
		return ""
	}

	var parts []string
	for i, expr := range filter.Expressions {
		part := fmt.Sprintf("%s %s %s", expr.Field, expr.Operator, expr.Value)
		parts = append(parts, part)

		// Add logical operator (except for last expression)
		if i < len(filter.Expressions)-1 && expr.LogicalOperator != "" {
			parts = append(parts, string(expr.LogicalOperator))
		}
	}

	return strings.Join(parts, " ")
}

// SafeFilter safely parses filter, returning empty filter on error
func SafeFilter(filter string) *Filter {
	f, err := ParseFilter(filter)
	if err != nil {
		return &Filter{Expressions: []FilterExpression{}}
	}
	return f
}

// IsEmpty checks if filter is empty (no expressions)
func IsEmpty(filter *Filter) bool {
	return filter == nil || len(filter.Expressions) == 0
}

// GetExpressionCount returns the number of expressions in filter
func GetExpressionCount(filter *Filter) int {
	if filter == nil {
		return 0
	}
	return len(filter.Expressions)
}

// FilterBuilder provides a fluent API for building Filter expressions
type FilterBuilder struct {
	filter *Filter
}

// NewFilterBuilder creates a new FilterBuilder
func NewFilterBuilder() *FilterBuilder {
	return &FilterBuilder{
		filter: &Filter{Expressions: []FilterExpression{}},
	}
}

// Equal adds an equality filter expression (=)
func (b *FilterBuilder) Equal(field, value string) *FilterBuilder {
	b.filter.Expressions = append(b.filter.Expressions, FilterExpression{
		Field:    field,
		Operator: OperatorEqual,
		Value:    value,
	})
	return b
}

// NotEqual adds an inequality filter expression (!=)
func (b *FilterBuilder) NotEqual(field, value string) *FilterBuilder {
	b.filter.Expressions = append(b.filter.Expressions, FilterExpression{
		Field:    field,
		Operator: OperatorNotEqual,
		Value:    value,
	})
	return b
}

// LessThan adds a less than filter expression (<)
func (b *FilterBuilder) LessThan(field, value string) *FilterBuilder {
	b.filter.Expressions = append(b.filter.Expressions, FilterExpression{
		Field:    field,
		Operator: OperatorLessThan,
		Value:    value,
	})
	return b
}

// LessThanOrEqual adds a less than or equal filter expression (<=)
func (b *FilterBuilder) LessThanOrEqual(field, value string) *FilterBuilder {
	b.filter.Expressions = append(b.filter.Expressions, FilterExpression{
		Field:    field,
		Operator: OperatorLessThanOrEqual,
		Value:    value,
	})
	return b
}

// GreaterThan adds a greater than filter expression (>)
func (b *FilterBuilder) GreaterThan(field, value string) *FilterBuilder {
	b.filter.Expressions = append(b.filter.Expressions, FilterExpression{
		Field:    field,
		Operator: OperatorGreaterThan,
		Value:    value,
	})
	return b
}

// GreaterThanOrEqual adds a greater than or equal filter expression (>=)
func (b *FilterBuilder) GreaterThanOrEqual(field, value string) *FilterBuilder {
	b.filter.Expressions = append(b.filter.Expressions, FilterExpression{
		Field:    field,
		Operator: OperatorGreaterThanOrEqual,
		Value:    value,
	})
	return b
}

// Contains adds a contains filter expression (:)
func (b *FilterBuilder) Contains(field, value string) *FilterBuilder {
	b.filter.Expressions = append(b.filter.Expressions, FilterExpression{
		Field:    field,
		Operator: OperatorContains,
		Value:    value,
	})
	return b
}

// NotContains adds a not contains filter expression (!:)
func (b *FilterBuilder) NotContains(field, value string) *FilterBuilder {
	b.filter.Expressions = append(b.filter.Expressions, FilterExpression{
		Field:    field,
		Operator: OperatorNotContains,
		Value:    value,
	})
	return b
}

// In adds an in filter expression (in)
func (b *FilterBuilder) In(field string, values ...string) *FilterBuilder {
	b.filter.Expressions = append(b.filter.Expressions, FilterExpression{
		Field:    field,
		Operator: OperatorIn,
		Value:    strings.Join(values, ","),
	})
	return b
}

// NotIn adds a not in filter expression (not_in)
func (b *FilterBuilder) NotIn(field string, values ...string) *FilterBuilder {
	b.filter.Expressions = append(b.filter.Expressions, FilterExpression{
		Field:    field,
		Operator: OperatorNotIn,
		Value:    strings.Join(values, ","),
	})
	return b
}

// Has adds a has filter expression (has)
func (b *FilterBuilder) Has(field, value string) *FilterBuilder {
	b.filter.Expressions = append(b.filter.Expressions, FilterExpression{
		Field:    field,
		Operator: OperatorHas,
		Value:    value,
	})
	return b
}

// And adds an AND logical operator to the last expression
func (b *FilterBuilder) And() *FilterBuilder {
	if len(b.filter.Expressions) > 0 {
		b.filter.Expressions[len(b.filter.Expressions)-1].LogicalOperator = LogicalAND
	}
	return b
}

// Or adds an OR logical operator to the last expression
func (b *FilterBuilder) Or() *FilterBuilder {
	if len(b.filter.Expressions) > 0 {
		b.filter.Expressions[len(b.filter.Expressions)-1].LogicalOperator = LogicalOR
	}
	return b
}

// Expr adds a custom filter expression
func (b *FilterBuilder) Expr(field string, operator FilterOperator, value string) *FilterBuilder {
	b.filter.Expressions = append(b.filter.Expressions, FilterExpression{
		Field:    field,
		Operator: operator,
		Value:    value,
	})
	return b
}

// ExprWithLogical adds a custom filter expression with logical operator
func (b *FilterBuilder) ExprWithLogical(field string, operator FilterOperator, value string, logicalOp LogicalOperator) *FilterBuilder {
	b.filter.Expressions = append(b.filter.Expressions, FilterExpression{
		Field:           field,
		Operator:        operator,
		Value:           value,
		LogicalOperator: logicalOp,
	})
	return b
}

// Build returns the constructed Filter
func (b *FilterBuilder) Build() *Filter {
	return b.filter
}

// String returns the string representation of the filter
func (b *FilterBuilder) String() string {
	return FilterToString(b.filter)
}

// FilterContainsField parses filterStr and reports whether any expression
// targets the given field name. Returns false on parse error.
func FilterContainsField(filterStr, field string) bool {
	if filterStr == "" {
		return false
	}
	f, err := ParseFilter(filterStr)
	if err != nil {
		return false
	}
	return HasFilterField(f, field)
}

// MustBuild returns the Filter or panics if validation fails
func (b *FilterBuilder) MustBuild(allowedFields ...string) *Filter {
	if len(allowedFields) > 0 {
		allowed := make(map[string]bool)
		for _, f := range allowedFields {
			allowed[f] = true
		}
		if err := ValidateFilterFields(b.filter, allowed); err != nil {
			panic(err)
		}
	}
	return b.filter
}
