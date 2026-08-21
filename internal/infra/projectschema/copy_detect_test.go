package projectschema

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDetectCopyAction(t *testing.T) {
	t.Parallel()

	action, err := detectCopyAction(false, false)
	require.NoError(t, err)
	require.Equal(t, copyNoop, action)

	action, err = detectCopyAction(false, true)
	require.NoError(t, err)
	require.Equal(t, copyNoop, action)

	action, err = detectCopyAction(true, false)
	require.ErrorContains(t, err, "000008 not applied")
	require.Equal(t, copyNoop, action)

	action, err = detectCopyAction(true, true)
	require.NoError(t, err)
	require.Equal(t, copyRun, action)
}
