package databases

import "github.com/torchwooddev/torchwood/pkg/ident"

// SystemDatabaseID 是系统集合的内部 database 寻址 id（项目数据面 sentinel `_`）。
// 与 ident.ProjectDataPlaneID 同值；对外 API 必须经 RejectExternalDatabaseID 拒绝。
// 物理表落在 tw_<project>，不建 tw_<project>_ 两段式 schema。
const SystemDatabaseID = ident.ProjectDataPlaneID

// SystemCollectionIDs 是 cut 前项目数据面七张系统资源的历史名单。
// Databases API 已用 RejectExternalDatabaseID 拒绝 sentinel；本名单只给
// DocumentDB 皮带（isSystem 跳过 _version、写保护）与 expand/copy 播种。
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

// IsSystemCollection 仅在项目数据面（databaseID == SystemDatabaseID）按名单
// 判定系统集合；业务库（含 default）中的同名集合不受影响。
func IsSystemCollection(projectID, databaseID, collectionID string) bool {
	return databaseID == SystemDatabaseID && IsSystemCollectionID(collectionID)
}
