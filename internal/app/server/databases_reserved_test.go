package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// collectionDocDB 包装 fakeDocDB，使 GetCollection 返回可用集合，
// 供 CreateDocument 保留字校验的单元测试使用。
type collectionDocDB struct {
	*fakeDocDB
}

func (collectionDocDB) GetCollection(context.Context, string, string, string) (*databases.Collection, error) {
	return &databases.Collection{ID: "coll1"}, nil
}

func TestCreateDocument_RejectsReservedIDs(t *testing.T) {
	d := &Databases{projectRepo: fakeProjectRepo{}, docDB: collectionDocDB{fakeDocDB: newFakeDocDB()}}
	ctx := platformAdminCtx(context.Background())
	principal := databases.Principal{PlatformAdmin: true}

	// REST 字面量路由保留字（F11-3）：documents/count、documents/bulk(+delete)。
	for _, id := range []string{"count", "bulk"} {
		_, err := d.CreateDocument(ctx, "p1", "db1", "coll1", id, map[string]any{"a": 1}, nil, principal)
		require.Equal(t, codes.InvalidArgument, status.Code(err), "document_id %q 应被拒绝", id)
	}

	// 非保留字 id 正常放行（文档写入 fakeDocDB 成功）。
	doc, err := d.CreateDocument(ctx, "p1", "db1", "coll1", "doc_1", map[string]any{"a": 1}, nil, principal)
	require.NoError(t, err)
	require.Equal(t, "doc_1", doc.ID)
}
