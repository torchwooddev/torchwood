package idgen

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/samber/lo"
)

// ID is a string-based domain identifier.
type ID string

func (id ID) String() string {
	return strings.TrimSpace(string(id))
}

// IsValid 仅校验 ID 非空（R08-P3-6）。字符集/长度/格式等语义校验归各 use-case
// 负责（例如 functions 的 functionIDPattern、server 的 ID 格式校验），
// 本方法不做全局限定，避免误导调用方以为任意 ID 类型都被完整校验。
func (id ID) IsValid() bool {
	return id.String() != ""
}

func UUID() ID {
	return ID(uuid.NewString())
}

func IDsToStrings(ids []ID) []string {
	return lo.Map(ids, func(id ID, _ int) string {
		return id.String()
	})
}

func (id ID) MarshalJSON() ([]byte, error) {
	if id == "" {
		return []byte("null"), nil
	}
	return json.Marshal(string(id))
}

func (id *ID) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" {
		*id = ""
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	*id = ID(s)
	return nil
}

func (id ID) Value() (driver.Value, error) {
	if id == "" {
		return nil, nil
	}
	return string(id), nil
}

func (id *ID) Scan(value any) error {
	if value == nil {
		*id = ""
		return nil
	}
	switch v := value.(type) {
	case []byte:
		*id = ID(v)
	case string:
		*id = ID(v)
	default:
		return errors.New("cannot scan non-string into ID")
	}
	return nil
}
