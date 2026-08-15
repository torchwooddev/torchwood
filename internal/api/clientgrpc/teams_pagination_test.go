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

// Round3 H6-1：Client API ListTeams 同样必须回传 NextPageToken。

// clientTeamsProjectRepo 返回固定项目。
type clientTeamsProjectRepo struct{}

func (clientTeamsProjectRepo) CreateProject(context.Context, *projects.Project) error { return nil }
func (clientTeamsProjectRepo) GetProject(_ context.Context, id string) (*projects.Project, error) {
	return &projects.Project{ID: id}, nil
}
func (clientTeamsProjectRepo) GetProjectByName(context.Context, string) (*projects.Project, error) {
	return nil, nil
}
func (clientTeamsProjectRepo) ListProjects(context.Context) ([]projects.Project, error) { return nil, nil }
func (clientTeamsProjectRepo) UpdateProject(context.Context, *projects.Project) error   { return nil }
func (clientTeamsProjectRepo) DeleteProject(context.Context, string) error              { return nil }

// clientTeamsDocDB 仅实现 ListDocuments/GetDocument/EnsureSystemCollections
// 语义（client ListTeams 路径所需），其余方法不参与。
type clientTeamsDocDB struct {
	docs  map[string][]databases.Document
	token string
}

func (d *clientTeamsDocDB) ListDocuments(_ context.Context, _, _, collectionID string, q databases.Query, _ databases.Principal) (*databases.DocumentList, error) {
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

func (d *clientTeamsDocDB) GetDocument(_ context.Context, _, _, collectionID, docID string, _ databases.Principal) (*databases.Document, error) {
	for _, doc := range d.docs[collectionID] {
		if doc.ID == docID {
			cp := doc
			return &cp, nil
		}
	}
	return nil, nil
}

func (d *clientTeamsDocDB) CreateDocument(context.Context, string, string, string, databases.Document, []databases.Permission, databases.Principal) (databases.Document, error) {
	return databases.Document{}, nil
}
func (d *clientTeamsDocDB) UpdateDocument(context.Context, string, string, string, databases.DocumentUpdate, databases.Principal) (databases.Document, error) {
	return databases.Document{}, nil
}
func (d *clientTeamsDocDB) UpsertDocument(context.Context, string, string, string, databases.Document, []string, []databases.Permission, databases.Principal) (databases.Document, error) {
	return databases.Document{}, nil
}
func (d *clientTeamsDocDB) DeleteDocument(context.Context, string, string, string, string, databases.DeleteOptions, databases.Principal) error {
	return nil
}
func (d *clientTeamsDocDB) CountDocuments(context.Context, string, string, string, []string, databases.Principal) (int64, error) {
	return 0, nil
}
func (d *clientTeamsDocDB) SumDocumentField(context.Context, string, string, string, string, databases.Principal) (int64, error) {
	return 0, nil
}
func (d *clientTeamsDocDB) BulkUpdateDocuments(context.Context, string, string, string, []string, map[string]any, []databases.Permission, databases.Principal) (int64, error) {
	return 0, nil
}
func (d *clientTeamsDocDB) BulkDeleteDocuments(context.Context, string, string, string, []string, databases.Principal) (int64, error) {
	return 0, nil
}
func (d *clientTeamsDocDB) EnsureSystemCollections(context.Context, string, int64) error { return nil }
func (d *clientTeamsDocDB) CreateDatabase(context.Context, string, string, string) error { return nil }
func (d *clientTeamsDocDB) GetDatabase(context.Context, string, string) (*databases.Collection, error) {
	return nil, nil
}
func (d *clientTeamsDocDB) ListDatabases(context.Context, string) ([]databases.Collection, error) {
	return nil, nil
}
func (d *clientTeamsDocDB) DeleteDatabase(context.Context, string, string) error { return nil }
func (d *clientTeamsDocDB) CreateCollection(context.Context, string, string, string, string, []databases.Attribute, []databases.Index, []databases.Permission, bool) error {
	return nil
}
func (d *clientTeamsDocDB) GetCollection(context.Context, string, string, string) (*databases.Collection, error) {
	return nil, nil
}
func (d *clientTeamsDocDB) ListCollections(context.Context, string, string, databases.ListQuery) ([]databases.Collection, databases.ListMeta, error) {
	return nil, databases.ListMeta{}, nil
}
func (d *clientTeamsDocDB) DeleteCollection(context.Context, string, string, string) error { return nil }
func (d *clientTeamsDocDB) UpdateCollection(context.Context, string, string, string, databases.CollectionPatch) error {
	return nil
}
func (d *clientTeamsDocDB) CreateAttribute(context.Context, string, string, string, databases.Attribute) error {
	return nil
}
func (d *clientTeamsDocDB) DeleteAttribute(context.Context, string, string, string, string) error {
	return nil
}
func (d *clientTeamsDocDB) CreateIndex(context.Context, string, string, string, databases.Index) error {
	return nil
}
func (d *clientTeamsDocDB) DeleteIndex(context.Context, string, string, string, string) error {
	return nil
}

func TestClientGRPC_ListTeams_EchoesNextPageToken(t *testing.T) {
	docDB := &clientTeamsDocDB{
		token: "tok-9",
		docs: map[string][]databases.Document{
			"teams": {{ID: "team-1", Data: map[string]any{"name": "T", "total": int64(1)}}},
		},
	}
	serverTeams := appserver.NewTeams(clientTeamsProjectRepo{}, docDB)
	svc := NewTeamsService(appclient.NewTeams(serverTeams, docDB))

	ctx := contexts.WithPrincipal(context.Background(), &shared.Principal{
		ActorID: "user-1", ActorKind: shared.ActorKindEndUser, UserID: "user-1",
		ProjectID: "proj-1", Roles: []string{"users", "user:user-1"},
	})
	resp, err := svc.ListTeams(ctx, &sharedv1.ListRequest{PageSize: 10})
	require.NoError(t, err)
	require.Len(t, resp.Teams, 1)
	require.Equal(t, "tok-9", resp.Meta.GetNextPageToken(), "Client ListTeams 必须回传 NextPageToken")
	require.Equal(t, int32(1), resp.Meta.GetTotalCount())
}
