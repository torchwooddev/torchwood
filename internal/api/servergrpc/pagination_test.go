package servergrpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
	sharedv1 "github.com/torchwooddev/torchwood/genproto/shared/v1"
	appserver "github.com/torchwooddev/torchwood/internal/app/server"
	appstorage "github.com/torchwooddev/torchwood/internal/app/storage"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/projects"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"github.com/torchwooddev/torchwood/pkg/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ---- Round3 H6-1/H6-2：List 回传 NextPageToken；GetBucket 用 BuildEqual ----

// paginationProjectRepo 返回固定项目。
type paginationProjectRepo struct{}

func (paginationProjectRepo) CreateProject(context.Context, *projects.Project) error { return nil }
func (paginationProjectRepo) GetProject(_ context.Context, id string) (*projects.Project, error) {
	return &projects.Project{ID: id}, nil
}
func (paginationProjectRepo) GetProjectByName(context.Context, string) (*projects.Project, error) {
	return nil, nil
}
func (paginationProjectRepo) ListProjects(context.Context) ([]projects.Project, error) {
	return nil, nil
}
func (paginationProjectRepo) UpdateProject(context.Context, *projects.Project) error { return nil }
func (paginationProjectRepo) DeleteProject(context.Context, string) error            { return nil }

// paginationDocDB 是内存 DocumentDB：按集合存文档，ListDocuments 支持
// equal（含 $id）过滤并返回固定 NextPageToken，用于断言 handler 回传。
type paginationDocDB struct {
	docs  map[string][]databases.Document
	token string
}

func (d *paginationDocDB) ListDocuments(_ context.Context, _, _, collectionID string, q databases.Query, _ databases.Principal) (*databases.DocumentList, error) {
	parsed, err := query.ParseMany(q.Queries)
	if err != nil {
		return nil, err
	}
	var out []databases.Document
	for _, doc := range d.docs[collectionID] {
		match := true
		for _, f := range parsed.Filters {
			if f.Op != "equal" || len(f.Values) == 0 {
				continue
			}
			if f.Attribute == "$id" {
				if doc.ID != f.Values[0] {
					match = false
				}
			} else if v, _ := doc.Data[f.Attribute].(string); v != f.Values[0] {
				match = false
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

func (d *paginationDocDB) GetDocument(_ context.Context, _, _, collectionID, docID string, _ databases.Principal) (*databases.Document, error) {
	for _, doc := range d.docs[collectionID] {
		if doc.ID == docID {
			cp := doc
			return &cp, nil
		}
	}
	return nil, nil
}

func (d *paginationDocDB) CreateDocument(context.Context, string, string, string, databases.Document, []databases.Permission, databases.Principal) (databases.Document, error) {
	return databases.Document{}, nil
}
func (d *paginationDocDB) UpdateDocument(context.Context, string, string, string, databases.DocumentUpdate, databases.Principal) (databases.Document, error) {
	return databases.Document{}, nil
}
func (d *paginationDocDB) UpsertDocument(context.Context, string, string, string, databases.Document, []string, []databases.Permission, databases.Principal) (databases.Document, error) {
	return databases.Document{}, nil
}
func (d *paginationDocDB) DeleteDocument(context.Context, string, string, string, string, databases.DeleteOptions, databases.Principal) error {
	return nil
}
func (d *paginationDocDB) CountDocuments(context.Context, string, string, string, []string, databases.Principal) (int64, error) {
	return 0, nil
}
func (d *paginationDocDB) SumDocumentField(context.Context, string, string, string, string, databases.Principal) (int64, error) {
	return 0, nil
}
func (d *paginationDocDB) BulkUpdateDocuments(context.Context, string, string, string, []string, map[string]any, []databases.Permission, databases.Principal) (int64, error) {
	return 0, nil
}
func (d *paginationDocDB) BulkDeleteDocuments(context.Context, string, string, string, []string, databases.Principal) (int64, error) {
	return 0, nil
}
func (d *paginationDocDB) EnsureSystemCollections(context.Context, string, int64) error { return nil }
func (d *paginationDocDB) CreateDatabase(context.Context, string, string, string) error { return nil }
func (d *paginationDocDB) GetDatabase(context.Context, string, string) (*databases.Collection, error) {
	return nil, nil
}
func (d *paginationDocDB) ListDatabases(context.Context, string) ([]databases.Collection, error) {
	return nil, nil
}
func (d *paginationDocDB) DeleteDatabase(context.Context, string, string) error { return nil }
func (d *paginationDocDB) CreateCollection(context.Context, string, string, string, string, []databases.Attribute, []databases.Index, []databases.Permission, bool) error {
	return nil
}
func (d *paginationDocDB) GetCollection(context.Context, string, string, string) (*databases.Collection, error) {
	return nil, nil
}
func (d *paginationDocDB) ListCollections(context.Context, string, string, databases.ListQuery) ([]databases.Collection, databases.ListMeta, error) {
	return nil, databases.ListMeta{}, nil
}
func (d *paginationDocDB) DeleteCollection(context.Context, string, string, string) error { return nil }
func (d *paginationDocDB) UpdateCollection(context.Context, string, string, string, databases.CollectionPatch) error {
	return nil
}
func (d *paginationDocDB) CreateAttribute(context.Context, string, string, string, databases.Attribute) error {
	return nil
}
func (d *paginationDocDB) DeleteAttribute(context.Context, string, string, string, string) error {
	return nil
}
func (d *paginationDocDB) CreateIndex(context.Context, string, string, string, databases.Index) error {
	return nil
}
func (d *paginationDocDB) DeleteIndex(context.Context, string, string, string, string) error {
	return nil
}

func paginationCtx() context.Context {
	return contexts.WithPrincipal(context.Background(), &shared.Principal{
		ActorID: "admin-1", ActorKind: shared.ActorKindAdmin, ProjectID: "proj-1",
		Roles: []string{"owner"}, IsPlatformAdmin: true,
	})
}

func TestServerGRPC_ListHandlers_EchoNextPageToken(t *testing.T) {
	docDB := &paginationDocDB{
		token: "tok-1",
		docs: map[string][]databases.Document{
			"users":   {{ID: "u-1", Data: map[string]any{"email": "a@b.c", "status": "active"}}},
			"groups":  {{ID: "group-1", Data: map[string]any{"name": "T", "total": int64(1)}}},
			"buckets": {{ID: "b-1", Data: map[string]any{"name": "B", "permissions": []any{"read"}, "public": false}}},
			"files":   {{ID: "f-1", Data: map[string]any{"bucket_id": "b-1", "name": "x.png", "mime_type": "image/png", "size": int64(3)}}},
		},
	}
	ctx := paginationCtx()
	users := NewUsersService(appserver.NewUsers(paginationProjectRepo{}, docDB, nil, nil, nil))
	groups := NewGroupsService(appserver.NewGroups(paginationProjectRepo{}, docDB, nil))
	storage := NewStorageService(appstorage.NewStorage(&config.AppConfig{}, paginationProjectRepo{}, docDB, nil, nil))

	t.Run("ListUsers", func(t *testing.T) {
		resp, err := users.ListUsers(ctx, &sharedv1.ListRequest{PageSize: 10, PageToken: "tok-0"})
		require.NoError(t, err)
		require.Len(t, resp.Users, 1)
		require.Equal(t, "tok-1", resp.Meta.GetNextPageToken(), "ListUsers 必须回传 NextPageToken")
		require.Equal(t, int32(1), resp.Meta.GetTotalCount())
	})

	t.Run("ListGroups", func(t *testing.T) {
		resp, err := groups.ListGroups(ctx, &sharedv1.ListRequest{PageSize: 10})
		require.NoError(t, err)
		require.Len(t, resp.Groups, 1)
		require.Equal(t, "tok-1", resp.Meta.GetNextPageToken(), "ListGroups 必须回传 NextPageToken")
	})

	t.Run("ListMemberships", func(t *testing.T) {
		resp, err := groups.ListMemberships(ctx, &serverv1.ListMembershipsRequest{GroupId: "group-1", PageSize: 10})
		require.NoError(t, err)
		require.Equal(t, "tok-1", resp.Meta.GetNextPageToken(), "ListMemberships 必须回传 NextPageToken")
	})

	t.Run("ListBuckets", func(t *testing.T) {
		resp, err := storage.ListBuckets(ctx, &sharedv1.ListRequest{PageSize: 10})
		require.NoError(t, err)
		require.Len(t, resp.Buckets, 1)
		require.Equal(t, "tok-1", resp.Meta.GetNextPageToken(), "ListBuckets 必须回传 NextPageToken")
		require.Equal(t, int32(1), resp.Meta.GetTotalCount())
	})

	t.Run("ListFiles", func(t *testing.T) {
		resp, err := storage.ListFiles(ctx, &serverv1.ListFilesRequest{BucketId: "b-1", PageSize: 10})
		require.NoError(t, err)
		require.Len(t, resp.Files, 1)
		require.Equal(t, "tok-1", resp.Meta.GetNextPageToken(), "ListFiles 必须回传 NextPageToken")
		require.Equal(t, "b-1", resp.Files[0].BucketId)
	})
}

// Round3 H6-2：GetBucket 用 BuildEqual 构造 $id 过滤——合法 id 命中返回、
// 不存在的 id 返回 NotFound、含引号的 id 不得引发解析错误（手拼串会挂）。
func TestServerGRPC_GetBucket_UsesBuildEqual(t *testing.T) {
	docDB := &paginationDocDB{
		docs: map[string][]databases.Document{
			"buckets": {{ID: "b-1", Data: map[string]any{"name": "B", "permissions": []any{"read"}, "public": true}}},
		},
	}
	storage := NewStorageService(appstorage.NewStorage(&config.AppConfig{}, paginationProjectRepo{}, docDB, nil, nil))
	ctx := paginationCtx()

	t.Run("found", func(t *testing.T) {
		b, err := storage.GetBucket(ctx, &serverv1.GetBucketRequest{Id: "b-1"})
		require.NoError(t, err)
		require.Equal(t, "b-1", b.Id)
		require.Equal(t, "B", b.Name)
	})

	t.Run("not found", func(t *testing.T) {
		_, err := storage.GetBucket(ctx, &serverv1.GetBucketRequest{Id: "missing"})
		require.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("id with quotes does not break query", func(t *testing.T) {
		// 手拼 equal("$id","b"1") 会产生 DSL 解析错误（Internal/InvalidArgument）；
		// BuildEqual 正确转义后应正常执行查询并返回 NotFound。
		_, err := storage.GetBucket(ctx, &serverv1.GetBucketRequest{Id: `b"1`})
		require.Equal(t, codes.NotFound, status.Code(err), "含引号的 id 必须安全走完查询并返回 NotFound")
	})
}
