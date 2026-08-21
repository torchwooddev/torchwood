package bunrepo

import (
	"encoding/json"
	"time"
)

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

func unmarshalStringSlice(raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func unmarshalAnyMap(raw json.RawMessage) map[string]any {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}

func unmarshalStringMap(raw json.RawMessage) map[string]string {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]string{}
	}
	var out map[string]string
	if err := json.Unmarshal(raw, &out); err == nil && out != nil {
		return out
	}
	var anyMap map[string]any
	if err := json.Unmarshal(raw, &anyMap); err != nil || anyMap == nil {
		return map[string]string{}
	}
	out = make(map[string]string, len(anyMap))
	for k, v := range anyMap {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}
