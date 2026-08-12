package query

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		expected Query
	}{
		{
			name: "equal string",
			raw:  `equal("email","a@b.com")`,
			expected: Query{
				Filters: []Filter{{Op: "equal", Attribute: "email", Values: []string{"a@b.com"}}},
			},
		},
		{
			name: "equal array",
			raw:  `equal("status",["active","pending"])`,
			expected: Query{
				Filters: []Filter{{Op: "equal", Attribute: "status", Values: []string{"active", "pending"}}},
			},
		},
		{
			name: "between",
			raw:  `between("age",18,65)`,
			expected: Query{
				Filters: []Filter{{Op: "between", Attribute: "age", Values: []string{"18", "65"}}},
			},
		},
		{
			name: "order desc",
			raw:  `orderDesc("createdAt")`,
			expected: Query{
				Orders: []Order{{Attribute: "createdAt", Desc: true}},
			},
		},
		{
			name: "limit",
			raw:  `limit(25)`,
			expected: Query{
				Limit: 25,
			},
		},
		{
			name: "select",
			raw:  `select(["name","email"])`,
			expected: Query{
				Selects: []string{"name", "email"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, err := Parse(tc.raw)
			require.NoError(t, err)
			require.Equal(t, &tc.expected, q)
		})
	}
}

func TestParseMany(t *testing.T) {
	q, err := ParseMany([]string{
		`equal("status","active")`,
		`greaterThan("age",18)`,
		`orderDesc("createdAt")`,
		`limit(10)`,
		`offset(20)`,
	})
	require.NoError(t, err)
	require.Len(t, q.Filters, 2)
	require.Len(t, q.Orders, 1)
	require.Equal(t, 10, q.Limit)
	require.Equal(t, 20, q.Offset)
}

// TestParseMany_NoDefaultLimit (F3-2)：未显式指定 limit 时 Limit 保持 0，
// 默认页大小交由 adapter 决定（ListDocuments 用 PageSize 回退）。
func TestParseMany_NoDefaultLimit(t *testing.T) {
	q, err := ParseMany([]string{`equal("status","active")`})
	require.NoError(t, err)
	require.Equal(t, 0, q.Limit)

	q, err = ParseMany(nil)
	require.NoError(t, err)
	require.Equal(t, 0, q.Limit)

	q, err = ParseMany([]string{`limit(0)`})
	require.NoError(t, err)
	require.Equal(t, 0, q.Limit)
}

// TestParse_NegativeLimitOffset (A1): 解析期 fail-fast，负数 limit/offset 直接报错，
// 防止透传到 PG 变成 LIMIT -1（全表返回）。
func TestParse_NegativeLimitOffset(t *testing.T) {
	for _, raw := range []string{`limit(-1)`, `limit(-100)`, `offset(-1)`, `offset(-999)`} {
		t.Run(raw, func(t *testing.T) {
			_, err := Parse(raw)
			require.Error(t, err)
		})
	}
	// 正值不受影响。
	q, err := Parse(`limit(0)`)
	require.NoError(t, err)
	require.Equal(t, 0, q.Limit)
	q, err = Parse(`offset(0)`)
	require.NoError(t, err)
	require.Equal(t, 0, q.Offset)
}
