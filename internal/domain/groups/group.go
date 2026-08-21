package groups

import "time"

// Group 是项目内用户组聚合。Total 只允许 SQL 增量 / 重数，禁止经 Update 回写。
type Group struct {
	ID          string
	Name        string
	Permissions []string
	Total       int64
	Prefs       map[string]any
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
