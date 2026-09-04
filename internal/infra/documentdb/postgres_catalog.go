// 目录与 schema 寻址：database CRUD（public 全局 catalog，redesign §4.2/C1）、
// 两段式 schema 解析、内部租户 id。
package documentdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/infra/projectschema"
	"github.com/torchwooddev/torchwood/pkg/ident"
)

func (p *postgresDocumentDB) CreateDatabase(ctx context.Context, projectID, id, name string) error {
	schema, err := p.businessSchema(projectID, id)
	if err != nil {
		return p.mapError(err)
	}
	// R02-P1-2：schema / _perms 表与 catalog 元数据包进同一事务，
	// 任一步失败整体回滚，避免"schema 已建而元数据缺失"。
	// catalog 已全局化（public 两表），建库不再依赖项目数据面 schema。
	return p.mapError(p.db.RunInTx(ctx, func(txCtx context.Context) error {
		if _, err := p.conn(txCtx).ExecContext(txCtx, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s`, quoteIdent(schema))); err != nil {
			return fmt.Errorf("create schema: %w", err)
		}
		if err := p.ensurePermsTable(txCtx, schema); err != nil {
			return err
		}
		m := &model.DocumentDatabase{
			ProjectID:  projectID,
			DatabaseID: id,
			Name:       name,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}
		_, err := p.conn(txCtx).NewInsert().Model(m).Exec(txCtx)
		return err
	}))
}

func (p *postgresDocumentDB) GetDatabase(ctx context.Context, projectID, id string) (*databases.Database, error) {
	m := new(model.DocumentDatabase)
	err := p.conn(ctx).NewSelect().Model(m).
		Where("project_id = ? AND database_id = ?", projectID, id).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, p.mapError(err)
	}
	return mapDatabase(m), nil
}

func (p *postgresDocumentDB) ListDatabases(ctx context.Context, projectID string) ([]databases.Database, error) {
	var ms []model.DocumentDatabase
	err := p.conn(ctx).NewSelect().Model(&ms).
		Where("project_id = ?", projectID).Order("created_at DESC").Scan(ctx)
	if err != nil {
		return nil, p.mapError(err)
	}
	out := make([]databases.Database, len(ms))
	for i := range ms {
		out[i] = *mapDatabase(&ms[i])
	}
	return out, nil
}

func (p *postgresDocumentDB) DeleteDatabase(ctx context.Context, projectID, id string) error {
	schema, err := p.businessSchema(projectID, id)
	if err != nil {
		return p.mapError(err)
	}
	return p.mapError(p.db.RunInTx(ctx, func(txCtx context.Context) error {
		if _, err := p.conn(txCtx).ExecContext(txCtx, fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE`, quoteIdent(schema))); err != nil {
			return err
		}
		// F4-2：物理对象删除后必须清理元数据，否则同名库/集合无法重建。
		// catalog_collections 的 FK ON DELETE CASCADE 是兜底，显式删除保持
		// 与删除路径同事务的清晰边界。
		if _, err := p.conn(txCtx).NewDelete().Model((*model.DocumentCollection)(nil)).
			Where("project_id = ? AND database_id = ?", projectID, id).Exec(txCtx); err != nil {
			return err
		}
		_, err := p.conn(txCtx).NewDelete().Model((*model.DocumentDatabase)(nil)).
			Where("project_id = ? AND database_id = ?", projectID, id).Exec(txCtx)
		return err
	}))
}

// EnsureCatalog 对项目数据面执行 projectschema.Apply。职责已收缩（阶段②包 A）：
// catalog 元数据在 public 全局两表（000025），此处仅确保项目 schema 存在——
// sentinel 系统集合的物理表寄居 tw_<project>，建集合前必须就绪；业务库两段式
// schema 与项目数据面无依赖，不走本函数。sys_users 仍在时跳过 Apply，
// 避免把 staging 表提前 rename 成最终名。
func (p *postgresDocumentDB) EnsureCatalog(ctx context.Context, projectID string) error {
	staging, err := p.systemTablesStaging(ctx, projectID)
	if err != nil {
		return p.mapError(err)
	}
	if staging {
		return nil
	}
	return p.mapError(mapIdentError(projectschema.Apply(ctx, p.db, projectID)))
}

// systemTablesStaging 探测 sys_users 仍在（000008 已应用、000009 未应用）。
// 此时 EnsureCatalog 不得 Apply 后续迁移。
func (p *postgresDocumentDB) systemTablesStaging(ctx context.Context, projectID string) (bool, error) {
	schema, err := ident.ProjectSchemaName(projectID)
	if err != nil {
		return false, p.mapError(mapIdentError(err))
	}
	var has bool
	err = p.conn(ctx).QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1 FROM information_schema.tables
  WHERE table_schema = ? AND table_name = 'sys_users'
)
`, schema).Scan(&has)
	if err != nil {
		return false, p.mapError(err)
	}
	return has, nil
}

// documentSchema 解析文档读写 / CreateCollection 的目标 schema。
// 仅此处允许 sentinel → ProjectSchemaName（一段式 tw_<project>），
// 供测试在项目数据面重建旧文档表；生产入口已拒绝 database_id="_"。
func (p *postgresDocumentDB) documentSchema(ctx context.Context, projectID, databaseID string) (int64, string, error) {
	internalID, err := p.resolveInternalID(ctx, projectID)
	if err != nil {
		return 0, "", err
	}
	if databaseID == ident.ProjectDataPlaneID {
		schema, err := ident.ProjectSchemaName(projectID)
		return internalID, schema, mapIdentError(err)
	}
	schema, err := ident.SchemaName(projectID, databaseID)
	return internalID, schema, mapIdentError(err)
}

// businessSchema 供 CreateDatabase / DeleteDatabase：只许两段式。显式拒
// sentinel、拒一段式，DROP SCHEMA 永不映射到 tw_<project>。
func (p *postgresDocumentDB) businessSchema(projectID, databaseID string) (string, error) {
	if databaseID == ident.ProjectDataPlaneID {
		return "", status.Error(codes.InvalidArgument, "database_id is reserved")
	}
	schema, err := ident.SchemaName(projectID, databaseID)
	if err != nil {
		return "", mapIdentError(err)
	}
	if !ident.IsTwoSegmentSchema(schema) {
		return "", status.Error(codes.Internal, "refusing to DDL a non two-segment schema")
	}
	if one, err := ident.ProjectSchemaName(projectID); err == nil && schema == one {
		return "", status.Error(codes.Internal, "refusing to DROP/CREATE project data-plane schema")
	}
	return schema, nil
}

func tableName(schema, collectionID string) string {
	return quoteIdent(schema) + "." + quoteIdent(collectionID)
}

func permsTableName(schema string) string {
	return quoteIdent(schema) + "." + quoteIdent("_perms")
}

// InternalIDCacheInvalidator 是 internalIDCache 的进程内失效面：组合根把
// projectschema manager 的 Invalidate 桥接到此（结构化接口，避免
// documentdb ↔ projectschema 反向依赖）。Round4 J5-3。
type InternalIDCacheInvalidator interface {
	InvalidateInternalIDCache(projectID string)
}

// InvalidateInternalIDCache 清除项目的 internal_id 进程内缓存。项目删除后
// 同 ID 重建时，陈旧缓存的 internal_id 会以错误 _tenant 标签写入新实例，
// 造成静默数据分裂（audit §B-P3）。
func (p *postgresDocumentDB) InvalidateInternalIDCache(projectID string) {
	p.internalIDCache.Delete(projectID)
}

// resolveInternalIDFresh 绕过缓存强制重解析并回填。DDL 会把 internal_id 烤进
// collection 表的 _tenant 列默认值（createCollectionTable），若此刻缓存陈旧
// （项目删除重建窗口），整张新表的默认租户都会错（R4-J5-3），建表路径必须
// 取实时值。
func (p *postgresDocumentDB) resolveInternalIDFresh(ctx context.Context, projectID string) (int64, error) {
	p.internalIDCache.Delete(projectID)
	return p.resolveInternalID(ctx, projectID)
}

func (p *postgresDocumentDB) resolveInternalID(ctx context.Context, projectID string) (int64, error) {
	if cached, ok := p.internalIDCache.Load(projectID); ok {
		return cached.(int64), nil
	}
	var internalID int64
	err := p.conn(ctx).NewSelect().Model((*model.Project)(nil)).Column("internal_id").Where("id = ?", projectID).Scan(ctx, &internalID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("project not found: %s", projectID)
		}
		return 0, p.mapError(err)
	}
	p.internalIDCache.Store(projectID, internalID)
	return internalID, nil
}

func mapDatabase(m *model.DocumentDatabase) *databases.Database {
	if m == nil {
		return nil
	}
	return &databases.Database{
		ID:        m.DatabaseID,
		ProjectID: m.ProjectID,
		Name:      m.Name,
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
	}
}
