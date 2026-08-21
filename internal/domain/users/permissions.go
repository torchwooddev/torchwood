package users

import "fmt"

// Permission 是 users 文档 ACE（与文档层 type/role 对齐，避免领域依赖 databases）。
type Permission struct {
	Type string
	Role string
}

// DocumentPermissions 是 users 文档的唯一权限切片：owner 读写删，keys 只读，admin 读写删。
func DocumentPermissions(userID string) []Permission {
	owner := fmt.Sprintf("user:%s", userID)
	return []Permission{
		{Type: "read", Role: owner},
		{Type: "read", Role: "keys"},
		{Type: "read", Role: "admin"},
		{Type: "update", Role: owner},
		{Type: "update", Role: "admin"},
		{Type: "delete", Role: owner},
		{Type: "delete", Role: "admin"},
	}
}
