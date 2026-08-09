package servergrpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
	appserver "github.com/torchwooddev/torchwood/internal/app/server"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/domain/shared"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/internal/pkg/contexts"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"
)

// TestDatabasesService_AuditResource (A8): databases gRPC handler 注入审计资源，
// CreateDocument 后 LatestAuditLog().ResourceID 非空且格式正确。
func TestDatabasesService_AuditResource(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, internalID, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := documentdb.NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, internalID))

	env, err := testutil.NewInterceptorEnv(db, &config.AppConfig{}, docDB)
	require.NoError(t, err)

	uc := appserver.NewDatabases(bunrepo.NewProjectRepository(db), docDB)
	svc := NewDatabasesService(uc)

	require.NoError(t, uc.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, uc.CreateCollection(ctx, projectID, "app", "posts", "Posts", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, nil, true))

	const method = "/torchwood.server.v1.DatabasesService/CreateDocument"
	info := &grpc.UnaryServerInfo{FullMethod: method}
	handler := func(ctx context.Context, req any) (any, error) {
		return svc.CreateDocument(ctx, req.(*serverv1.CreateDocumentRequest))
	}

	principal := &shared.Principal{
		ActorKind: shared.ActorKindService,
		ProjectID: projectID,
		Roles:     []string{"keys"},
	}
	req := &serverv1.CreateDocumentRequest{
		DatabaseId:   "app",
		CollectionId: "posts",
		DocumentId:   "doc-1",
		Data:         mapStringToStruct(t, map[string]any{"title": "t"}),
	}
	_, err = env.Audit.UnaryAuditMiddleware(contexts.WithPrincipal(ctx, principal), req, info, handler)
	require.NoError(t, err)

	log, err := env.LatestAuditLog(ctx)
	require.NoError(t, err)
	require.Equal(t, method, log.Action)
	require.Equal(t, "databases/app/collections/posts/documents/doc-1", log.ResourceID)
}

func mapStringToStruct(t *testing.T, m map[string]any) *structpb.Struct {
	t.Helper()
	s, err := structpb.NewStruct(m)
	require.NoError(t, err)
	return s
}
