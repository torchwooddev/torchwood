package users

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/torchwooddev/torchwood/internal/domain/databases"
	domainusers "github.com/torchwooddev/torchwood/internal/domain/users"
)

func TestMapInsertDuplicate(t *testing.T) {
	t.Parallel()

	emailIdx := fmt.Errorf("%w: ERROR: duplicate key value violates unique constraint \"idx_users_users_email_unique\" (SQLSTATE 23505)", databases.ErrDuplicateKey)
	emailDetail := fmt.Errorf("%w: Key (email)=(a@b.c) already exists.", databases.ErrDuplicateKey)
	pk := fmt.Errorf("%w: ERROR: duplicate key value violates unique constraint \"users_pkey\" (SQLSTATE 23505)", databases.ErrDuplicateKey)

	require.ErrorIs(t, mapInsertDuplicate(emailIdx), domainusers.ErrEmailAlreadyRegistered)
	require.ErrorIs(t, mapInsertDuplicate(emailDetail), domainusers.ErrEmailAlreadyRegistered)
	require.ErrorIs(t, mapInsertDuplicate(pk), databases.ErrDuplicateKey)
	require.False(t, errors.Is(mapInsertDuplicate(pk), domainusers.ErrEmailAlreadyRegistered))
	require.Nil(t, mapInsertDuplicate(nil))
	require.Equal(t, context.Canceled, mapInsertDuplicate(context.Canceled))
}

func TestInsert_NilStore(t *testing.T) {
	t.Parallel()

	err := (*DocumentRepository)(nil).Insert(context.Background(), "p1", &domainusers.User{ID: "u1", Email: "a@b.c"})
	require.ErrorIs(t, err, errNilStore)
	require.False(t, errors.Is(err, domainusers.ErrUserIDRequired))

	err = NewDocumentRepository(nil).Insert(context.Background(), "p1", &domainusers.User{ID: "u1", Email: "a@b.c"})
	require.ErrorIs(t, err, errNilStore)
}
