package projectschema

import (
	"context"
	"fmt"

	domainprojects "github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/torchwooddev/torchwood/pkg/ident"
)

// SchemaManager 实现 projects.SchemaManager 端口：schema 生命周期的
// infra 侧唯一入口（此前 CREATE/DROP SCHEMA 内联在 app/server/projects.go）。
type SchemaManager struct {
	db *clients.Database
}

// NewSchemaManager 构造绑定单个 Database 实例的 schema 生命周期管理器
// （就绪缓存按实例隔离，见 Apply 的缓存键说明）。
func NewSchemaManager(db *clients.Database) *SchemaManager {
	return &SchemaManager{db: db}
}

var _ domainprojects.SchemaManager = (*SchemaManager)(nil)

// Ensure 幂等确保项目 schema 存在且迁移到最新（含就绪缓存直通；ctx 携带
// 事务时并入调用方事务且不写缓存）。
func (m *SchemaManager) Ensure(ctx context.Context, projectID string) error {
	return Apply(ctx, m.db, projectID)
}

// DropCascade 删除项目数据面 schema（CASCADE）。须在调用方事务内执行
// （ctx 携带事务时自动并入），与控制面行删除原子提交。
func (m *SchemaManager) DropCascade(ctx context.Context, projectID string) error {
	schema, err := ident.ProjectSchemaName(projectID)
	if err != nil {
		return err
	}
	if _, err := m.db.Conn(ctx).ExecContext(ctx,
		fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE`, quoteIdent(schema))); err != nil {
		return fmt.Errorf("drop project schema: %w", err)
	}
	return nil
}

// Invalidate 清除就绪缓存并级联清除 DropCascade 语义下的进程内状态。
func (m *SchemaManager) Invalidate(projectID string) {
	Invalidate(m.db, projectID)
}
