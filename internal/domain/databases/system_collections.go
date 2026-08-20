package databases

// SystemCollectionIDs 是 default 库中由专用服务（Users/Groups/Storage/Auth）
// 独占管理的系统集合名单；Databases API 对其实行只读策略（读分级放行、写全拒）。
var SystemCollectionIDs = []string{
	"users",
	"sessions",
	"identities",
	"groups",
	"memberships",
	"buckets",
	"files",
}

// SensitiveSystemCollectionIDs 是高敏系统集合：Server API 仅 PlatformAdmin 可读
// （返回前脱敏），Client API 一律拒绝（有 Account 专用 API）。
var SensitiveSystemCollectionIDs = []string{
	"users",
	"sessions",
	"identities",
}

// IsSystemCollectionID 报告集合 ID 是否命中系统集合名单。
func IsSystemCollectionID(id string) bool {
	for _, c := range SystemCollectionIDs {
		if c == id {
			return true
		}
	}
	return false
}

// IsSensitiveSystemCollectionID 报告集合 ID 是否属于高敏系统集合。
func IsSensitiveSystemCollectionID(id string) bool {
	for _, c := range SensitiveSystemCollectionIDs {
		if c == id {
			return true
		}
	}
	return false
}

// IsSystemCollection 仅在 default 库中按名单判定系统集合；
// 自定义数据库中的同名集合不受影响。
func IsSystemCollection(projectID, databaseID, collectionID string) bool {
	return databaseID == "default" && IsSystemCollectionID(collectionID)
}
