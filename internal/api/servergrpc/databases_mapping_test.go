package servergrpc

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
)

// TestMapCollection_AttributeDefaultValue：GetCollection/ListCollections 响应必须
// 回填 default_value（catalog 落库后响应层不得再是断点）。
func TestMapCollection_AttributeDefaultValue(t *testing.T) {
	c := &databases.Collection{
		ID:         "posts",
		DatabaseID: "app",
		Name:       "Posts",
		Attributes: []databases.Attribute{
			{ID: "title", Key: "title", Type: "string", Size: 128, Default: "untitled"},
			{ID: "views", Key: "views", Type: "integer", Default: 42},
			{ID: "pinned", Key: "pinned", Type: "boolean"},
		},
	}
	out := mapCollection(c)
	require.Len(t, out.Attributes, 3)
	require.Equal(t, "untitled", out.Attributes[0].DefaultValue)
	require.Equal(t, "42", out.Attributes[1].DefaultValue)
	require.Empty(t, out.Attributes[2].DefaultValue)
}
