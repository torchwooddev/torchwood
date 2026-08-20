package ident

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestValidateSchemaResourceID_Valid(t *testing.T) {
	for _, id := range []string{"a", "default", "shop", "app", "cms", "acmeprodshop2026", strings.Repeat("a", 28)} {
		require.NoError(t, ValidateSchemaResourceID(id), id)
	}
}

func TestValidateSchemaResourceID_Invalid(t *testing.T) {
	for _, id := range []string{
		"",
		"A",
		"Shop",
		"1a",
		"1shop",
		"my-shop",
		"my_shop",
		"shop.app",
		"应用",
		"my shop",
		strings.Repeat("a", 29),
	} {
		err := ValidateSchemaResourceID(id)
		require.Error(t, err, id)
		st, ok := status.FromError(err)
		require.True(t, ok, id)
		require.Equal(t, codes.InvalidArgument, st.Code(), id)
		require.Equal(t, errSchemaResourceID, st.Message(), id)
	}
}

func TestSchemaName(t *testing.T) {
	got, err := SchemaName("shop", "app")
	require.NoError(t, err)
	require.Equal(t, "tw_shop_app", got)

	got, err = SchemaName("default", "default")
	require.NoError(t, err)
	require.Equal(t, "tw_default_default", got)
}

func TestSchemaName_RejectsInvalid(t *testing.T) {
	for _, tc := range []struct {
		project, database string
	}{
		{"Shop", "app"},
		{"shop", "my_app"},
		{"my-shop", "app"},
		{"", "app"},
		{"shop", ""},
		{strings.Repeat("a", 29), "app"},
	} {
		got, err := SchemaName(tc.project, tc.database)
		require.Error(t, err, tc)
		require.Empty(t, got, tc)
	}
}
