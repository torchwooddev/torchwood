package server

import (
	"context"
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

func TestCreateDatabase_InvalidID(t *testing.T) {
	d := &Databases{}
	for _, id := range []string{"bad-id", "bad id", "1starts_with_digit"} {
		st, _ := status.FromError(d.CreateDatabase(platformAdminCtx(context.Background()), "proj", id, "name"))
		require.Equal(t, codes.InvalidArgument, st.Code(), "id %q should be rejected", id)
	}
}

func TestCreateDatabase_DefaultRejected(t *testing.T) {
	d := &Databases{}
	st, _ := status.FromError(d.CreateDatabase(platformAdminCtx(context.Background()), "proj", "default", "name"))
	require.Equal(t, codes.InvalidArgument, st.Code())
}
