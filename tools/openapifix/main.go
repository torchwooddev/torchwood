// Command openapifix 对 protoc-gen-openapiv2 的 swagger 输出做确定性后处理：
//
//  1. 所有 operation 的 default 响应统一改引用
//     #/definitions/torchwoodsharedv1ErrorResponse —— 运行时错误体即
//     shared.v1.ErrorResponse（见 internal/infra/server/errors.go），
//     而生成器默认声明的 rpcStatus 与实际响应不符；
//  2. 删除生成器注入的 rpcStatus 定义（运行时不返回该结构）；
//  3. 确保 ErrorResponse / Error / ErrorCode 定义存在于每个文件。
//     生成器只为「可达」消息输出定义，而没有任何 proto 引用 ErrorResponse
//     （它是运行时错误通道，不经 proto 声明），因此定义需在此注入。内容与
//     openapiv2(json_names_for_fields=false) 对 proto/shared/v1/error.proto
//     的原生输出一致（ErrorCode 的 title 注释除外，避免注释漂移导致静默失真）。
//     定义名取全限定名去点（torchwood.shared.v1.ErrorResponse →
//     torchwoodsharedv1ErrorResponse），与业务 swagger 中保留完整包路径时的
//     生成约定一致且当前无碰撞；若未来某 proto 直接引用了这些消息，重生成会
//     出现原生命名定义并存，届时应改为以原生定义为准。
//
// 由 Taskfile generate:proto 在 buf generate 之后调用：go run ./tools/openapifix
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	errorRef       = "#/definitions/torchwoodsharedv1ErrorResponse"
	errorDefName   = "torchwoodsharedv1Error"
	codeDefName    = "torchwoodsharedv1ErrorCode"
	respDefName    = "torchwoodsharedv1ErrorResponse"
	defaultDesc    = "An unexpected error response."
	rpcStatusIdent = "rpcStatus"
)

// 与 proto/shared/v1/error.proto 经 openapiv2(json_names_for_fields=false)
// 生成结果一致（见包注释）。修改 error.proto 后需同步更新此处。
const injectedDefinitions = `{
  "` + codeDefName + `": {
    "type": "string",
    "enum": [
      "ERROR_CODE_UNSPECIFIED",
      "ERROR_CODE_INVALID_REQUEST",
      "ERROR_CODE_RESOURCE_NOT_FOUND",
      "ERROR_CODE_INVALID_CREDENTIALS",
      "ERROR_CODE_PERMISSION_DENIED",
      "ERROR_CODE_RESOURCE_CONFLICT",
      "ERROR_CODE_QUOTA_EXCEEDED",
      "ERROR_CODE_PRECONDITION_FAILED",
      "ERROR_CODE_CONCURRENT_MODIFICATION",
      "ERROR_CODE_VALUE_OUT_OF_RANGE",
      "ERROR_CODE_OPERATION_NOT_ALLOWED",
      "ERROR_CODE_INTERNAL_ERROR",
      "ERROR_CODE_SERVICE_UNAVAILABLE",
      "ERROR_CODE_TIMEOUT"
    ],
    "default": "ERROR_CODE_UNSPECIFIED"
  },
  "` + errorDefName + `": {
    "type": "object",
    "properties": {
      "type": {"type": "string"},
      "code": {"type": "string"},
      "message": {"type": "string"},
      "error_id": {"type": "string"},
      "error_code": {"$ref": "#/definitions/` + codeDefName + `"}
    }
  },
  "` + respDefName + `": {
    "type": "object",
    "properties": {
      "error": {"$ref": "#/definitions/` + errorDefName + `"}
    }
  }
}`

func main() {
	root := "genproto"
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	if err := run(root); err != nil {
		fmt.Fprintln(os.Stderr, "openapifix:", err)
		os.Exit(1)
	}
}

func run(root string) error {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".swagger.json") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, path := range files {
		fixed, err := fixFile(path)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if fixed {
			fmt.Println("fixed", path)
		}
	}
	if len(files) == 0 {
		return fmt.Errorf("no *.swagger.json under %s", root)
	}
	return nil
}

func fixFile(path string) (bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	doc, err := decodeValue(dec)
	if err != nil {
		return false, err
	}
	docMap, ok := doc.(*oMap)
	if !ok {
		return false, fmt.Errorf("top-level JSON value is not an object")
	}

	fixed := false

	// paths: 每个 operation 的 default 响应指向 ErrorResponse；缺失则补齐。
	pathsAny, _ := docMap.get("paths")
	paths, _ := pathsAny.(*oMap)
	if paths != nil {
		for _, pathItemAny := range paths.values() {
			pathItem, ok := pathItemAny.(*oMap)
			if !ok {
				continue
			}
			for _, opAny := range pathItem.values() {
				op, ok := opAny.(*oMap)
				if !ok {
					continue // x- 扩展等非对象成员
				}
				responsesAny, _ := op.get("responses")
				responses, ok := responsesAny.(*oMap)
				if !ok {
					continue
				}
				defAny, hasDefault := responses.get("default")
				if !hasDefault {
					responses.set("default", mustParseJSON(`{
						"description": "`+defaultDesc+`",
						"schema": {"$ref": "`+errorRef+`"}
					}`))
					fixed = true
					continue
				}
				defResp, ok := defAny.(*oMap)
				if !ok {
					continue
				}
				schemaAny, _ := defResp.get("schema")
				schema, _ := schemaAny.(*oMap)
				refAny, _ := schema.get("$ref")
				ref, _ := refAny.(string)
				if ref == rpcStatusRef {
					schema.set("$ref", errorRef)
					fixed = true
				} else if ref != "" && ref != errorRef && ref != localRef(respDefName) {
					return false, fmt.Errorf("default response 引用了意外的 %q", ref)
				}
			}
		}
	}

	// definitions: 删 rpcStatus，补 ErrorResponse 三件套。
	definitionsAny, _ := docMap.get("definitions")
	definitions, _ := definitionsAny.(*oMap)
	if definitions == nil {
		return false, fmt.Errorf("missing definitions")
	}
	if _, exists := definitions.get(rpcStatusIdent); exists {
		definitions.delete(rpcStatusIdent)
		fixed = true
	}
	injectedAny := mustParseJSON(injectedDefinitions)
	injected, ok := injectedAny.(*oMap)
	if !ok {
		return false, fmt.Errorf("injected definitions JSON is not an object")
	}
	for _, name := range []string{codeDefName, errorDefName, respDefName} {
		if _, exists := definitions.get(name); !exists {
			v, _ := injected.get(name)
			definitions.set(name, v)
			fixed = true
		}
	}

	// 全文档兜底：不允许 rpcStatus 残留。
	if containsString(doc, localRef(rpcStatusIdent)) || containsKey(doc, rpcStatusIdent) {
		return false, fmt.Errorf("rpcStatus 仍被引用或定义")
	}

	if !fixed {
		return false, nil
	}
	var compact bytes.Buffer
	if err := encodeValue(&compact, doc); err != nil {
		return false, err
	}
	var indented bytes.Buffer
	if err := json.Indent(&indented, compact.Bytes(), "", "  "); err != nil {
		return false, err
	}
	indented.WriteByte('\n')
	return true, os.WriteFile(path, indented.Bytes(), 0o644)
}

const rpcStatusRef = "#/definitions/" + rpcStatusIdent

func localRef(name string) string { return "#/definitions/" + name }

func mustParseJSON(s string) any {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	v, err := decodeValue(dec)
	if err != nil {
		panic(fmt.Sprintf("openapifix 内嵌 JSON 非法: %v", err))
	}
	return v
}

// oMap 是保持插入顺序的 JSON 对象。
type oMap struct {
	keys []string
	vals map[string]any
}

func newOMap() *oMap { return &oMap{vals: map[string]any{}} }

func (m *oMap) get(k string) (any, bool) {
	v, ok := m.vals[k]
	return v, ok
}

func (m *oMap) set(k string, v any) {
	if _, exists := m.vals[k]; !exists {
		m.keys = append(m.keys, k)
	}
	m.vals[k] = v
}

func (m *oMap) delete(k string) {
	delete(m.vals, k)
	for i, key := range m.keys {
		if key == k {
			m.keys = append(m.keys[:i], m.keys[i+1:]...)
			break
		}
	}
}

func (m *oMap) values() []any {
	out := make([]any, 0, len(m.keys))
	for _, k := range m.keys {
		out = append(out, m.vals[k])
	}
	return out
}

func decodeValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	return decodeTok(dec, tok)
}

func decodeTok(dec *json.Decoder, tok json.Token) (any, error) {
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			m := newOMap()
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key := keyTok.(string)
				v, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				m.set(key, v)
			}
			if _, err := dec.Token(); err != nil { // '}'
				return nil, err
			}
			return m, nil
		case '[':
			arr := []any{}
			for dec.More() {
				v, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, v)
			}
			if _, err := dec.Token(); err != nil { // ']'
				return nil, err
			}
			return arr, nil
		}
		return nil, fmt.Errorf("unexpected delim %v", t)
	default:
		return tok, nil
	}
}

func encodeValue(buf *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case *oMap:
		buf.WriteByte('{')
		for i, k := range t.keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			kb, err := json.Marshal(k)
			if err != nil {
				return err
			}
			buf.Write(kb)
			buf.WriteByte(':')
			if err := encodeValue(buf, t.vals[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	case []any:
		buf.WriteByte('[')
		for i, item := range t {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := encodeValue(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		buf.Write(b)
	}
	return nil
}

func containsString(v any, needle string) bool {
	switch t := v.(type) {
	case *oMap:
		for _, k := range t.keys {
			if k == needle || containsString(t.vals[k], needle) {
				return true
			}
		}
	case []any:
		for _, item := range t {
			if containsString(item, needle) {
				return true
			}
		}
	case string:
		return t == needle
	}
	return false
}

func containsKey(v any, key string) bool {
	m, ok := v.(*oMap)
	if !ok {
		return false
	}
	if _, exists := m.get(key); exists {
		return true
	}
	for _, k := range m.keys {
		if containsKey(m.vals[k], key) {
			return true
		}
	}
	return false
}
