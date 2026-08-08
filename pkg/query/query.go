package query

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Filter represents a single Appwrite-style query filter.
type Filter struct {
	Op        string
	Attribute string
	Values    []string
}

// Order represents an ordering clause.
type Order struct {
	Attribute string
	Desc      bool
}

// Query is the parsed representation of a list of Appwrite-style query strings.
type Query struct {
	Filters      []Filter
	Orders       []Order
	Selects      []string
	Limit        int
	Offset       int
	CursorAfter  string
	CursorBefore string
}

var queryRe = regexp.MustCompile(`^(\w+)\((.*)\)$`)

// Parse parses a single Appwrite-style query string.
// Examples:
//   equal("email","a@b.com")
//   greaterThan("age",18)
//   contains("name","john")
//   orderDesc("createdAt")
//   limit(25)
func Parse(raw string) (*Query, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return &Query{}, nil
	}

	m := queryRe.FindStringSubmatch(raw)
	if m == nil {
		return nil, fmt.Errorf("invalid query format: %s", raw)
	}
	op := m[1]
	argStr := m[2]

	args, err := splitArgs(argStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse query %q: %w", raw, err)
	}

	switch op {
	case "equal", "notEqual", "lessThan", "lessThanEqual", "greaterThan", "greaterThanEqual", "contains", "startsWith", "endsWith", "search":
		if len(args) < 2 {
			return nil, fmt.Errorf("%s requires at least 2 args", op)
		}
		attr, err := unquote(args[0])
		if err != nil {
			return nil, err
		}
		var values []string
		if strings.HasPrefix(args[1], "[") {
			values, err = parseArray(args[1])
			if err != nil {
				return nil, err
			}
		} else {
			v, err := unquoteOrLiteral(args[1])
			if err != nil {
				return nil, err
			}
			values = []string{v}
		}
		return &Query{Filters: []Filter{{Op: op, Attribute: attr, Values: values}}}, nil

	case "between":
		if len(args) != 3 {
			return nil, fmt.Errorf("between requires 3 args")
		}
		attr, err := unquote(args[0])
		if err != nil {
			return nil, err
		}
		min, err := unquoteOrLiteral(args[1])
		if err != nil {
			return nil, err
		}
		max, err := unquoteOrLiteral(args[2])
		if err != nil {
			return nil, err
		}
		return &Query{Filters: []Filter{{Op: op, Attribute: attr, Values: []string{min, max}}}}, nil

	case "isNull", "isNotNull":
		if len(args) != 1 {
			return nil, fmt.Errorf("%s requires 1 arg", op)
		}
		attr, err := unquote(args[0])
		if err != nil {
			return nil, err
		}
		return &Query{Filters: []Filter{{Op: op, Attribute: attr}}}, nil

	case "orderAsc", "orderDesc":
		if len(args) != 1 {
			return nil, fmt.Errorf("%s requires 1 arg", op)
		}
		attr, err := unquote(args[0])
		if err != nil {
			return nil, err
		}
		return &Query{Orders: []Order{{Attribute: attr, Desc: op == "orderDesc"}}}, nil

	case "limit":
		if len(args) != 1 {
			return nil, fmt.Errorf("limit requires 1 arg")
		}
		n, err := strconv.Atoi(args[0])
		if err != nil {
			return nil, fmt.Errorf("limit must be an integer")
		}
		if n < 0 {
			return nil, fmt.Errorf("limit must be non-negative")
		}
		return &Query{Limit: n}, nil

	case "offset":
		if len(args) != 1 {
			return nil, fmt.Errorf("offset requires 1 arg")
		}
		n, err := strconv.Atoi(args[0])
		if err != nil {
			return nil, fmt.Errorf("offset must be an integer")
		}
		if n < 0 {
			return nil, fmt.Errorf("offset must be non-negative")
		}
		return &Query{Offset: n}, nil

	case "cursorAfter", "cursorBefore":
		if len(args) != 1 {
			return nil, fmt.Errorf("%s requires 1 arg", op)
		}
		cursor, err := unquote(args[0])
		if err != nil {
			return nil, err
		}
		q := &Query{}
		if op == "cursorAfter" {
			q.CursorAfter = cursor
		} else {
			q.CursorBefore = cursor
		}
		return q, nil

	case "select":
		if len(args) != 1 {
			return nil, fmt.Errorf("select requires 1 arg")
		}
		fields, err := parseArray(args[0])
		if err != nil {
			return nil, err
		}
		return &Query{Selects: fields}, nil

	default:
		return nil, fmt.Errorf("unsupported query operator: %s", op)
	}
}

// ParseMany parses multiple Appwrite-style query strings and merges them into one Query.
func ParseMany(raw []string) (*Query, error) {
	merged := &Query{}
	for _, r := range raw {
		q, err := Parse(r)
		if err != nil {
			return nil, err
		}
		merged.Filters = append(merged.Filters, q.Filters...)
		merged.Orders = append(merged.Orders, q.Orders...)
		merged.Selects = append(merged.Selects, q.Selects...)
		if q.Limit != 0 {
			merged.Limit = q.Limit
		}
		if q.Offset != 0 {
			merged.Offset = q.Offset
		}
		if q.CursorAfter != "" {
			merged.CursorAfter = q.CursorAfter
		}
		if q.CursorBefore != "" {
			merged.CursorBefore = q.CursorBefore
		}
	}
	if merged.Limit == 0 {
		merged.Limit = 50 // default page size
	}
	return merged, nil
}

// splitArgs splits top-level arguments, respecting quoted strings and brackets.
func splitArgs(s string) ([]string, error) {
	var args []string
	var sb strings.Builder
	depth := 0
	inQuote := false
	var escape bool
	for i, r := range s {
		if escape {
			sb.WriteRune(r)
			escape = false
			continue
		}
		if r == '\\' {
			escape = true
			sb.WriteRune(r)
			continue
		}
		if r == '"' {
			inQuote = !inQuote
			sb.WriteRune(r)
			continue
		}
		if inQuote {
			sb.WriteRune(r)
			continue
		}
		switch r {
		case '[', '(':
			depth++
			sb.WriteRune(r)
		case ']', ')':
			depth--
			sb.WriteRune(r)
		case ',':
			if depth == 0 {
				args = append(args, strings.TrimSpace(sb.String()))
				sb.Reset()
			} else {
				sb.WriteRune(r)
			}
		default:
			sb.WriteRune(r)
		}
		_ = i
	}
	if inQuote || depth != 0 {
		return nil, fmt.Errorf("unbalanced arguments: %s", s)
	}
	if sb.Len() > 0 {
		args = append(args, strings.TrimSpace(sb.String()))
	}
	return args, nil
}

func unquote(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return "", fmt.Errorf("expected quoted string, got %s", s)
	}
	return unescapeString(s[1 : len(s)-1]), nil
}

func unquoteOrLiteral(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return unescapeString(s[1 : len(s)-1]), nil
	}
	return s, nil
}

// unescapeString reverses the escaping performed by escapeString.
func unescapeString(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			b.WriteByte(s[i+1])
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// escapeString escapes characters that would break the DSL grammar.
// Use this when composing query strings programmatically.
func escapeString(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// quoteString wraps a value in double quotes and escapes inner quotes/backslashes.
func quoteString(s string) string {
	return `"` + escapeString(s) + `"`
}

// BuildFilter constructs a single Appwrite-style query string from structured args.
// It is the safe counterpart to Sprintf-based query construction: values are
// escaped so that quotes/backslashes inside user input cannot break out of the
// quoted scope.
func BuildFilter(op, attr string, values ...string) string {
	parts := make([]string, 0, len(values)+1)
	parts = append(parts, quoteString(attr))
	for _, v := range values {
		parts = append(parts, quoteString(v))
	}
	return op + "(" + strings.Join(parts, ",") + ")"
}

// BuildEqual is a shorthand for BuildFilter("equal", attr, values...).
func BuildEqual(attr string, values ...string) string {
	return BuildFilter("equal", attr, values...)
}

// BuildLimit constructs a limit(n) query string.
func BuildLimit(n int) string {
	return fmt.Sprintf("limit(%d)", n)
}

func parseArray(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '[' || s[len(s)-1] != ']' {
		return nil, fmt.Errorf("expected array, got %s", s)
	}
	items, err := splitArgs(s[1 : len(s)-1])
	if err != nil {
		return nil, err
	}
	for i := range items {
		v, err := unquoteOrLiteral(items[i])
		if err != nil {
			return nil, err
		}
		items[i] = v
	}
	return items, nil
}
