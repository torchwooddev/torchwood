package groups

import (
	"fmt"
	"strings"
)

// GroupUpdateColumns 是仓储 Update 允许 SET 的列（total 必须走 AddTotal/RecountAccepted）。
var GroupUpdateColumns = map[string]struct{}{
	"name":        {},
	"permissions": {},
	"prefs":       {},
	"updated_at":  {},
}

// NormalizeGroupUpdateColumns 拒绝未知列与 total；空 map → ErrInvalidUpdate。
func NormalizeGroupUpdateColumns(cols map[string]any) (map[string]any, error) {
	if len(cols) == 0 {
		return nil, fmt.Errorf("%w: no columns to update", ErrInvalidUpdate)
	}
	out := make(map[string]any, len(cols))
	for k, v := range cols {
		col := strings.TrimSpace(k)
		if _, ok := GroupUpdateColumns[col]; !ok {
			return nil, fmt.Errorf("%w: unknown column %q", ErrInvalidUpdate, k)
		}
		if col == "name" {
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("%w: name must be a string", ErrInvalidUpdate)
			}
			s = strings.TrimSpace(s)
			if s == "" {
				return nil, fmt.Errorf("%w: name must not be empty", ErrInvalidUpdate)
			}
			out[col] = s
			continue
		}
		out[col] = v
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: no columns to update", ErrInvalidUpdate)
	}
	return out, nil
}
