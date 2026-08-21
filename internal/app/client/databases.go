package client

import (
	"context"
	"fmt"

	"github.com/torchwooddev/torchwood/internal/app/documents"
	"github.com/torchwooddev/torchwood/internal/app/shared"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	domainshared "github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Databases struct {
	projectRepo projects.Repository
	docDB       databases.DocumentDB
	docs        *documents.Documents
}

func NewDatabases(projectRepo projects.Repository, docDB databases.DocumentDB) *Databases {
	return &Databases{projectRepo: projectRepo, docDB: docDB, docs: documents.New(docDB)}
}

func (d *Databases) documentsCore() *documents.Documents {
	if d.docs != nil {
		return d.docs
	}
	return documents.New(d.docDB)
}

func (d *Databases) loadProject(ctx context.Context, projectID string) (*projects.Project, error) {
	if projectID == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id is required")
	}
	project, err := d.projectRepo.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, status.Error(codes.NotFound, "project not found")
	}
	if err := d.docDB.EnsureSystemCollections(ctx, project.ID, project.InternalID); err != nil {
		return nil, fmt.Errorf("ensure system collections: %w", err)
	}
	return project, nil
}

func clientActorOK(p *domainshared.Principal) bool {
	if p == nil || !p.IsAuthenticated() {
		return false
	}
	switch p.ActorKind {
	case domainshared.ActorKindEndUser, domainshared.ActorKindAdmin, domainshared.ActorKindService:
		return true
	default:
		return false
	}
}

func (d *Databases) resolveProject(ctx context.Context) (*projects.Project, databases.Principal, error) {
	p, ok := contexts.Principal(ctx)
	if !ok || p.ProjectID == "" || !clientActorOK(p) {
		return nil, databases.Principal{}, status.Error(codes.Unauthenticated, "unauthenticated")
	}
	project, err := d.loadProject(ctx, p.ProjectID)
	if err != nil {
		return nil, databases.Principal{}, err
	}
	return project, p.DocPrincipal(), nil
}

func (d *Databases) resolveReadPrincipal(ctx context.Context, projectID string) (string, databases.Principal, error) {
	if p, ok := contexts.Principal(ctx); ok && clientActorOK(p) && p.ProjectID != "" {
		if projectID != "" && projectID != p.ProjectID {
			return "", databases.Principal{}, status.Error(codes.InvalidArgument, "project_id mismatch")
		}
		project, err := d.loadProject(ctx, p.ProjectID)
		if err != nil {
			return "", databases.Principal{}, err
		}
		return project.ID, p.DocPrincipal(), nil
	}
	if projectID == "" {
		if p, ok := contexts.Principal(ctx); ok && p.ProjectID != "" {
			projectID = p.ProjectID
		}
	}
	project, err := d.loadProject(ctx, projectID)
	if err != nil {
		return "", databases.Principal{}, err
	}
	return project.ID, databases.GuestPrincipal, nil
}

func (d *Databases) ensureCollection(ctx context.Context, databaseID, collectionID string) (string, databases.Principal, error) {
	project, principal, err := d.resolveProject(ctx)
	if err != nil {
		return "", databases.Principal{}, err
	}
	return d.ensureCollectionForProject(ctx, project.ID, databaseID, collectionID, principal, false)
}

func (d *Databases) ensureCollectionForRead(ctx context.Context, projectID, databaseID, collectionID string) (string, databases.Principal, error) {
	pid, principal, err := d.resolveReadPrincipal(ctx, projectID)
	if err != nil {
		return "", databases.Principal{}, err
	}
	return d.ensureCollectionForProject(ctx, pid, databaseID, collectionID, principal, true)
}

func (d *Databases) ensureCollectionForProject(ctx context.Context, projectID, databaseID, collectionID string, principal databases.Principal, readOnly bool) (string, databases.Principal, error) {
	if err := shared.RejectExternalDatabaseID(databaseID); err != nil {
		return "", databases.Principal{}, err
	}
	// 系统集合仅限项目数据面判定；写路径拒绝全部系统集合，
	// 读路径仅拒绝高敏系统集合（users/sessions/identities，有 Account 专用 API）。
	// 对外 database_id 已拒 `_`，本守卫是纵深防御。
	if databases.IsSystemCollection(projectID, databaseID, collectionID) &&
		(!readOnly || databases.IsSensitiveSystemCollectionID(collectionID)) {
		return "", databases.Principal{}, shared.MapDocumentDBError(databases.ErrPermissionDenied)
	}
	col, err := d.docDB.GetCollection(ctx, projectID, databaseID, collectionID)
	if err != nil {
		return "", databases.Principal{}, err
	}
	if col == nil {
		return "", databases.Principal{}, status.Error(codes.NotFound, "collection not found")
	}
	if col.Disabled {
		return "", databases.Principal{}, shared.MapDocumentDBError(databases.ErrPermissionDenied)
	}
	return projectID, principal, nil
}

func (d *Databases) CreateDocument(
	ctx context.Context,
	databaseID, collectionID, documentID string,
	data map[string]any,
	perms []databases.Permission,
) (*databases.Document, error) {
	projectID, principal, err := d.ensureCollection(ctx, databaseID, collectionID)
	if err != nil {
		return nil, err
	}
	p, _ := contexts.Principal(ctx)
	if len(perms) == 0 {
		perms = ownerDocumentPermissions(p.OwnerID())
	}
	return d.documentsCore().CreateDocument(ctx, projectID, databaseID, collectionID, documentID, data, perms, principal, documents.WriteOptions{})
}

func (d *Databases) ListDocuments(
	ctx context.Context,
	projectID, databaseID, collectionID string,
	q databases.Query,
) ([]databases.Document, int64, string, error) {
	pid, principal, err := d.ensureCollectionForRead(ctx, projectID, databaseID, collectionID)
	if err != nil {
		return nil, 0, "", err
	}
	return d.documentsCore().ListDocuments(ctx, pid, databaseID, collectionID, q, principal)
}

func (d *Databases) GetDocument(ctx context.Context, projectID, databaseID, collectionID, documentID string) (*databases.Document, error) {
	pid, principal, err := d.ensureCollectionForRead(ctx, projectID, databaseID, collectionID)
	if err != nil {
		return nil, err
	}
	return d.documentsCore().GetDocument(ctx, pid, databaseID, collectionID, documentID, principal)
}

// clientDocumentUpdateProtectedFields 是客户端 UpdateDocument 中禁止修改的敏感字段，
// 用户只能通过认证用例（ChangePassword/VerifyEmail 等）间接修改这些字段。
var clientDocumentUpdateProtectedFields = map[string]struct{}{
	"password_hash":  {},
	"email_verified": {},
	"labels":         {},
	"status":         {},
}

func (d *Databases) UpdateDocument(
	ctx context.Context,
	databaseID, collectionID, documentID string,
	data map[string]any,
	perms []databases.Permission,
	increment map[string]int64,
	version *int64,
) (*databases.Document, error) {
	projectID, principal, err := d.ensureCollection(ctx, databaseID, collectionID)
	if err != nil {
		return nil, err
	}
	if err := shared.UpdateDocumentVersionRequired(version); err != nil {
		return nil, err
	}
	filtered := filterClientProtectedFields(data)
	if len(data) > 0 && len(filtered) == 0 && len(perms) == 0 && len(increment) == 0 {
		return nil, status.Error(codes.InvalidArgument, "no updatable fields supplied")
	}
	return d.documentsCore().UpdateDocument(ctx, projectID, databaseID, collectionID, documentID, filtered, perms, increment, principal, version, documents.WriteOptions{})
}

func (d *Databases) UpsertDocument(
	ctx context.Context,
	databaseID, collectionID, documentID string,
	data map[string]any,
	conflictColumns []string,
	perms []databases.Permission,
) (*databases.Document, error) {
	projectID, principal, err := d.ensureCollection(ctx, databaseID, collectionID)
	if err != nil {
		return nil, err
	}
	p, _ := contexts.Principal(ctx)
	if len(perms) == 0 {
		perms = ownerDocumentPermissions(p.OwnerID())
	}
	filtered := filterClientProtectedFields(data)
	if len(filtered) == 0 {
		if len(data) == 0 {
			return nil, status.Error(codes.InvalidArgument, "data is required")
		}
		return nil, status.Error(codes.InvalidArgument, "no updatable fields supplied")
	}
	return d.documentsCore().UpsertDocument(ctx, projectID, databaseID, collectionID, documentID, filtered, conflictColumns, perms, principal, documents.WriteOptions{})
}

func (d *Databases) DeleteDocument(ctx context.Context, databaseID, collectionID, documentID string, version *int64) error {
	projectID, principal, err := d.ensureCollection(ctx, databaseID, collectionID)
	if err != nil {
		return err
	}
	return d.documentsCore().DeleteDocument(ctx, projectID, databaseID, collectionID, documentID, principal, version)
}

func (d *Databases) CountDocuments(ctx context.Context, projectID, databaseID, collectionID string, q databases.Query) (int64, error) {
	pid, principal, err := d.ensureCollectionForRead(ctx, projectID, databaseID, collectionID)
	if err != nil {
		return 0, err
	}
	return d.documentsCore().CountDocuments(ctx, pid, databaseID, collectionID, q, principal)
}

func ownerDocumentPermissions(userID string) []databases.Permission {
	userRole := fmt.Sprintf("user:%s", userID)
	return []databases.Permission{
		{Type: "read", Role: userRole},
		{Type: "update", Role: userRole},
		{Type: "delete", Role: userRole},
	}
}
