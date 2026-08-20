package documentdb

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/pkg/ident"
)

func testSchema(t *testing.T, projectID, databaseID string) string {
	t.Helper()
	s, err := ident.SchemaName(projectID, databaseID)
	require.NoError(t, err)
	return s
}
