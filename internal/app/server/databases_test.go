package server

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestValidateAttributeType(t *testing.T) {
	d := &Databases{}
	for _, typ := range []string{"string", "INTEGER", "json"} {
		require.NoError(t, d.ValidateAttributeType(typ))
	}
	require.Error(t, d.ValidateAttributeType(""))
	st, _ := status.FromError(d.ValidateAttributeType("map"))
	require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestValidateIndex(t *testing.T) {
	d := &Databases{}
	require.NoError(t, d.ValidateIndex(databases.Index{
		ID:         "idx_email",
		Type:       "unique",
		Attributes: []string{"email"},
	}))
	require.Error(t, d.ValidateIndex(databases.Index{ID: "idx", Type: "unique"}))
	st, _ := status.FromError(d.ValidateIndex(databases.Index{ID: "idx", Type: "bad", Attributes: []string{"email"}}))
	require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestValidateIdentifier(t *testing.T) {
	d := &Databases{}
	require.NoError(t, d.ValidateIdentifier("users"))
	require.Error(t, d.ValidateIdentifier(""))
}

// TestValidateIdentifier_LengthLimit：标识符长度上限（POC 期封死 PG 63 字节截断：
// 两个仅超长部分不同的集合/属性曾会映射同一物理表/列）。
func TestValidateIdentifier_LengthLimit(t *testing.T) {
	d := &Databases{}
	require.NoError(t, d.ValidateIdentifier(strings.Repeat("a", 63)))
	st, _ := status.FromError(d.ValidateIdentifier(strings.Repeat("a", 64)))
	require.Equal(t, codes.InvalidArgument, st.Code())

	// collectionID 专用上限 40。
	require.NoError(t, d.validateCollectionID(strings.Repeat("c", 40)))
	st, _ = status.FromError(d.validateCollectionID(strings.Repeat("c", 41)))
	require.Equal(t, codes.InvalidArgument, st.Code())

	// 索引 ID 专用上限 40。
	require.NoError(t, d.ValidateIndex(databases.Index{ID: strings.Repeat("i", 40), Type: "key", Attributes: []string{"email"}}))
	st, _ = status.FromError(d.ValidateIndex(databases.Index{ID: strings.Repeat("i", 41), Type: "key", Attributes: []string{"email"}}))
	require.Equal(t, codes.InvalidArgument, st.Code())

	// 组合校验：coll 40 + idx 20 拼接 idx_<coll>_<idx> = 65 > 63，各段合法也必须拒绝。
	require.NoError(t, validateIndexNameLen(strings.Repeat("c", 30), strings.Repeat("i", 20)))
	st, _ = status.FromError(validateIndexNameLen(strings.Repeat("c", 40), strings.Repeat("i", 20)))
	require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestCreateDatabase_InvalidID(t *testing.T) {
	d := &Databases{}
	for _, id := range []string{"bad-id", "bad id", "1starts_with_digit", "MyApp", "my_app"} {
		st, _ := status.FromError(d.CreateDatabase(platformAdminCtx(context.Background()), "proj", id, "name"))
		require.Equal(t, codes.InvalidArgument, st.Code(), "id %q should be rejected", id)
	}
}
