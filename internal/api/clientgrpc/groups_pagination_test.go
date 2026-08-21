package clientgrpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	sharedv1 "github.com/torchwooddev/torchwood/genproto/shared/v1"
	appclient "github.com/torchwooddev/torchwood/internal/app/client"
	appserver "github.com/torchwooddev/torchwood/internal/app/server"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"github.com/torchwooddev/torchwood/pkg/query"
)

// Round3 H6-1：Client API ListGroups 同样必须回传 NextPageToken。

// clientGroupsProjectRepo 返回固定项目。
type clientGroupsProjectRepo struct{}

func (clientGroupsProjectRepo) CreateProject(context.Context, *projects.Project) error { return nil }
func (clientGroupsProjectRepo) GetProject(_ context.Context, id string) (*projects.Project, error) {
	return &projects.Project{ID: id}, nil
}
func (clientGroupsProjectRepo) GetProjectByName(context.Context, string) (*projects.Project, error) {
	return nil, nil
}
func (clientGroupsProjectRepo) ListProjects(context.Context) ([]projects.Project, error) {
	return nil, nil
}
func (clientGroupsProjectRepo) UpdateProject(context.Context, *projects.Project) error { return nil }
func (clientGroupsProjectRepo) DeleteProject(context.Context, string) error            { return nil }

// clientGroupsDocDB 仅实现 ListDocuments/GetDocument/EnsureSystemCollections
// 语义（client ListGroups 路径所需），其余方法不参与。
type clientGroupsDocDB struct {
	docs  map[string][]databases.Document
	token string
}

func (d *clientGroupsDocDB) ListDocuments(_ context.Context, _, _, collectionID string, q databases.Query, _ databases.Principal) (*databases.DocumentList, error) {
	parsed, err := query.ParseMany(q.Queries)
	if err != nil {
		return nil, err
	}
	var out []databases.Document
	for _, doc := range d.docs[collectionID] {
		match := true
		for _, f := range parsed.Filters {
			if f.Op == "equal" && len(f.Values) > 0 {
				if v, _ := doc.Data[f.Attribute].(string); v != f.Values[0] {
					match = false
				}
			}
		}
		if match {
			out = append(out, doc)
		}
	}
	return &databases.DocumentList{
		Documents:     out,
		TotalCount:    int64(len(out)),
		NextPageToken: d.token,
	}, nil
}

func (d *clientGroupsDocDB) GetDocument(_ context.Context, _, _, collectionID, docID string, _ databases.Principal) (*databases.Document, error) {
	for _, doc := range d.docs[collectionID] {
		if doc.ID == docID {
			cp := doc
			return &cp, nil
		}
	}
	return nil, nil
}

func (d *clientGroupsDocDB) CreateDocument(context.Context, string, string, string, databases.Document, []databases.Permission, databases.Principal) (databases.Document, error) {
	return databases.Document{}, nil
}
func (d *clientGroupsDocDB) UpdateDocument(context.Context, string, string, string, databases.DocumentUpdate, databases.Principal) (databases.Document, error) {
	return databases.Document{}, nil
}
func (d *clientGroupsDocDB) UpsertDocument(context.Context, string, string, string, databases.Document, []string, []databases.Permission, databases.Principal) (databases.Document, error) {
	return databases.Document{}, nil
}
func (d *clientGroupsDocDB) DeleteDocument(context.Context, string, string, string, string, databases.DeleteOptions, databases.Principal) error {
	return nil
}
func (d *clientGroupsDocDB) CountDocuments(context.Context, string, string, string, databases.Query, databases.Principal) (int64, error) {
	return 0, nil
}
func (d *clientGroupsDocDB) SumDocumentField(context.Context, string, string, string, string, databases.Principal) (int64, error) {
	return 0, nil
}
func (d *clientGroupsDocDB) BulkUpdateDocuments(context.Context, string, string, string, []string, map[string]any, []databases.Permission, databases.Principal) (int64, error) {
	return 0, nil
}
func (d *clientGroupsDocDB) BulkDeleteDocuments(context.Context, string, string, string, []string, databases.Principal) (int64, error) {
	return 0, nil
}
func (d *clientGroupsDocDB) EnsureSystemCollections(context.Context, string, int64) error { return nil }
func (d *clientGroupsDocDB) CreateDatabase(context.Context, string, string, string) error { return nil }
func (d *clientGroupsDocDB) GetDatabase(context.Context, string, string) (*databases.Collection, error) {
	return nil, nil
}
func (d *clientGroupsDocDB) ListDatabases(context.Context, string) ([]databases.Collection, error) {
	return nil, nil
}
func (d *clientGroupsDocDB) DeleteDatabase(context.Context, string, string) error { return nil }
func (d *clientGroupsDocDB) CreateCollection(context.Context, string, string, string, string, []databases.Attribute, []databases.Index, []databases.Permission, bool) error {
	return nil
}
func (d *clientGroupsDocDB) GetCollection(context.Context, string, string, string) (*databases.Collection, error) {
	return nil, nil
}
func (d *clientGroupsDocDB) ListCollections(context.Context, string, string, databases.ListQuery) ([]databases.Collection, databases.ListMeta, error) {
	return nil, databases.ListMeta{}, nil
}
func (d *clientGroupsDocDB) DeleteCollection(context.Context, string, string, string) error {
	return nil
}
func (d *clientGroupsDocDB) UpdateCollection(context.Context, string, string, string, databases.CollectionPatch) error {
	return nil
}
func (d *clientGroupsDocDB) CreateAttribute(context.Context, string, string, string, databases.Attribute) error {
	return nil
}
func (d *clientGroupsDocDB) DeleteAttribute(context.Context, string, string, string, string) error {
	return nil
}
func (d *clientGroupsDocDB) CreateIndex(context.Context, string, string, string, databases.Index) error {
	return nil
}
func (d *clientGroupsDocDB) DeleteIndex(context.Context, string, string, string, string) error {
	return nil
}

func TestClientGRPC_ListGroups_EchoesNextPageToken(t *testing.T) {
	docDB := &clientGroupsDocDB{
		token: "tok-9",
		docs: map[string][]databases.Document{
			"groups": {{ID: "group-1", Data: map[string]any{"name": "T", "total": int64(1)}}},
		},
	}
	serverGroups := appserver.NewGroups(clientGroupsProjectRepo{}, docDB, nil)
	svc := NewGroupsService(appclient.NewGroups(serverGroups, docDB))

	ctx := contexts.WithPrincipal(context.Background(), &shared.Principal{
		ActorID: "user-1", ActorKind: shared.ActorKindEndUser, UserID: "user-1",
		ProjectID: "proj-1", Roles: []string{"users", "user:user-1"},
	})
	resp, err := svc.ListGroups(ctx, &sharedv1.ListRequest{PageSize: 10})
	require.NoError(t, err)
	require.Len(t, resp.Groups, 1)
	require.Equal(t, "tok-9", resp.Meta.GetNextPageToken(), "Client ListGroups 必须回传 NextPageToken")
	require.Equal(t, int32(1), resp.Meta.GetTotalCount())
}
