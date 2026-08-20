package shared

import (
	"github.com/torchwooddev/torchwood/pkg/ident"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RejectExternalDatabaseID 校验对外 database_id：charset 合法且不是项目数据面
// sentinel。挂到 Server/Client 库/集合/文档/事务入口。charset 已拒 `_`，显式
// 比较是皮带（与 ident.ValidateSchemaResourceID 同为 InvalidArgument）。
func RejectExternalDatabaseID(id string) error {
	if err := ident.ValidateSchemaResourceID(id); err != nil {
		return err
	}
	if id == ident.ProjectDataPlaneID {
		return status.Error(codes.InvalidArgument, "database_id is reserved")
	}
	return nil
}
