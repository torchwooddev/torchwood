package storage

import (
	"fmt"
	"strings"
)

// BucketUpdateColumns 是仓储 Update 允许 SET 的列。
var BucketUpdateColumns = map[string]struct{}{
	"name":        {},
	"public":      {},
	"permissions": {},
	"updated_at":  {},
}

// FileUpdateColumns 是仓储 Update 允许 SET 的列（不含 size / owner_user_id）。
var FileUpdateColumns = map[string]struct{}{
	"name":       {},
	"mime_type":  {},
	"metadata":   {},
	"updated_at": {},
}

// NormalizeBucketUpdateColumns 拒绝未知列；空 map → ErrInvalidUpdate。
func NormalizeBucketUpdateColumns(cols map[string]any) (map[string]any, error) {
	if len(cols) == 0 {
		return nil, fmt.Errorf("%w: no columns to update", ErrInvalidUpdate)
	}
	out := make(map[string]any, len(cols))
	for k, v := range cols {
		col := strings.TrimSpace(k)
		if _, ok := BucketUpdateColumns[col]; !ok {
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
		if col == "public" {
			if _, ok := v.(bool); !ok {
				return nil, fmt.Errorf("%w: public must be a bool", ErrInvalidUpdate)
			}
		}
		out[col] = v
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: no columns to update", ErrInvalidUpdate)
	}
	return out, nil
}

// NormalizeFileUpdateColumns 拒绝未知列与 size；空 map → ErrInvalidUpdate。
func NormalizeFileUpdateColumns(cols map[string]any) (map[string]any, error) {
	if len(cols) == 0 {
		return nil, fmt.Errorf("%w: no columns to update", ErrInvalidUpdate)
	}
	out := make(map[string]any, len(cols))
	for k, v := range cols {
		col := strings.TrimSpace(k)
		if _, ok := FileUpdateColumns[col]; !ok {
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
