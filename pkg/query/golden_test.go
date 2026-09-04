package query

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// R11 共享 golden 语料：testdata/dsl_ast_golden.json 是 pkg/query.Parse/
// ParseMany 与 sdk/go/query.FromDSL 的**共同仲裁语料**——两侧测试消费同一
// 文件，改语料必须两侧同红同绿；两侧行为分歧时修落后的一侧，改语料本身
// 须在 commit message 给出理由。
//
// 条目格式：{"dsl": [串...], "ast": <中立 JSON> | null, "error": "<子串>"}；
// 可选 "root"/"sdk" 覆盖块 {"ast":…, "error":…} 声明单侧契约差异（如
// offset：根侧通用解析器支持、SDK 糖按文档面 keyset-only 拒绝）。
//
// 中立 JSON 是语言中立的 AST 描述（镜像 query.Query 的文法语义字段）：
// filter 树 {op, attribute, values[], children[]}、orders[{attribute,desc}]、
// selects、offset/cursorAfter/cursorBefore/pageSize/pageToken；limit 不进
// 中立形态（DSL limit(n) 的通道两侧分别是 Limit+PageSize 与 PageSize，
// 统一以 pageSize 表达）。空对象 = 解析成功且无任何子句。
type goldenEntry struct {
	DSL   []string        `json:"dsl"`
	AST   json.RawMessage `json:"ast"`
	Error string          `json:"error"`
	Root  *goldenOverride `json:"root"`
	SDK   *goldenOverride `json:"sdk"`
}

type goldenOverride struct {
	AST   json.RawMessage `json:"ast"`
	Error string          `json:"error"`
}

func (e goldenEntry) expectation(sdkSide bool) (json.RawMessage, string) {
	ov := e.Root
	if sdkSide {
		ov = e.SDK
	}
	if ov != nil {
		return ov.AST, ov.Error
	}
	return e.AST, e.Error
}

func loadGolden(t *testing.T) []goldenEntry {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "dsl_ast_golden.json"))
	require.NoError(t, err)
	var entries []goldenEntry
	require.NoError(t, json.Unmarshal(b, &entries))
	require.NotEmpty(t, entries)
	return entries
}

// TestParseGolden：根侧消费共享语料（ParseMany → 中立 JSON / 错误子串）。
func TestParseGolden(t *testing.T) {
	for i, e := range loadGolden(t) {
		name := fmt.Sprintf("%02d_%s", i, truncate(strings.Join(e.DSL, " ")))
		t.Run(name, func(t *testing.T) {
			expAST, expErr := e.expectation(false)
			got, err := ParseMany(e.DSL)
			if expErr != "" {
				require.Error(t, err, "golden 期望错误 %q", expErr)
				require.Contains(t, err.Error(), expErr)
				return
			}
			require.NoError(t, err)
			assertNeutralEqual(t, expAST, rootNeutral(got))
		})
	}
}

func truncate(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '"' || r == '\n' || r == '\t' {
			return '_'
		}
		return r
	}, s)
	if len(s) > 60 {
		return s[:60]
	}
	return s
}

func assertNeutralEqual(t *testing.T, expected json.RawMessage, actual map[string]any) {
	t.Helper()
	exp := "{}"
	if len(expected) > 0 {
		exp = string(expected)
	}
	act, err := json.Marshal(actual)
	require.NoError(t, err)
	require.JSONEq(t, exp, string(act), "中立形态与 golden 不符：got %s", string(act))
}

// rootNeutral：*Query → 中立 JSON（字段语义见 goldenEntry 注释）。
func rootNeutral(q *Query) map[string]any {
	if q == nil {
		return map[string]any{}
	}
	m := map[string]any{}
	if q.Filter != nil {
		m["filter"] = rootNeutralFilter(q.Filter)
	}
	if len(q.Orders) > 0 {
		orders := make([]map[string]any, 0, len(q.Orders))
		for _, o := range q.Orders {
			om := map[string]any{"attribute": o.Attribute}
			if o.Desc {
				om["desc"] = true
			}
			orders = append(orders, om)
		}
		m["orders"] = orders
	}
	if len(q.Selects) > 0 {
		m["selects"] = append([]string{}, q.Selects...)
	}
	if q.Offset != 0 {
		m["offset"] = q.Offset
	}
	if q.CursorAfter != "" {
		m["cursorAfter"] = q.CursorAfter
	}
	if q.CursorBefore != "" {
		m["cursorBefore"] = q.CursorBefore
	}
	if q.PageSize != 0 {
		m["pageSize"] = q.PageSize
	}
	if q.PageToken != "" {
		m["pageToken"] = q.PageToken
	}
	return m
}

func rootNeutralFilter(f *Filter) map[string]any {
	switch f.Op {
	case OpAnd, OpOr:
		children := make([]map[string]any, 0, len(f.Children))
		for _, c := range f.Children {
			children = append(children, rootNeutralFilter(c))
		}
		return map[string]any{"op": f.Op, "children": children}
	default:
		node := map[string]any{"op": f.Op, "attribute": f.Attribute}
		if len(f.Values) > 0 {
			node["values"] = append([]string{}, f.Values...)
		}
		return node
	}
}
