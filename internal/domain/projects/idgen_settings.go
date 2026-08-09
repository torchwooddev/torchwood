package projects

import "strings"

const (
	SettingsKeyIDGenDefault   = "idgen.default"
	SettingsKeyIDGenUsers     = "idgen.users"
	SettingsKeyIDGenSessions  = "idgen.sessions"
	SettingsKeyIDGenDocuments = "idgen.documents"
)

// IDGenStrategyForResource resolves the configured strategy for a resource from project settings.
func IDGenStrategyForResource(settings map[string]any, resource string, platformDefault string) string {
	if settings != nil {
		switch resource {
		case "users":
			if s := readIDGenSetting(settings, SettingsKeyIDGenUsers); s != "" {
				return s
			}
		case "sessions":
			if s := readIDGenSetting(settings, SettingsKeyIDGenSessions); s != "" {
				return s
			}
		case "documents":
			if s := readIDGenSetting(settings, SettingsKeyIDGenDocuments); s != "" {
				return s
			}
		}
		if s := readIDGenSetting(settings, SettingsKeyIDGenDefault); s != "" {
			return s
		}
	}
	return strings.TrimSpace(platformDefault)
}

func readIDGenSetting(settings map[string]any, dottedKey string) string {
	if s, ok := settings[dottedKey].(string); ok && strings.TrimSpace(s) != "" {
		return strings.ToLower(strings.TrimSpace(s))
	}
	if nested, ok := settings["idgen"].(map[string]any); ok {
		parts := strings.SplitN(dottedKey, ".", 2)
		if len(parts) == 2 {
			if s, ok := nested[parts[1]].(string); ok && strings.TrimSpace(s) != "" {
				return strings.ToLower(strings.TrimSpace(s))
			}
		}
	}
	return ""
}
