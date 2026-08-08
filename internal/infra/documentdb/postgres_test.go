package documentdb

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/torchwooddev/torchwood/internal/domain/databases"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestPostgresDocumentDatabase_CRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))

	// Create a custom database and collection.
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "notes", "Notes", nil, nil, nil, true))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "posts", "Posts", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
		{ID: "views", Key: "views", Type: "integer"},
	}, []databases.Index{
		{ID: "title_key", Type: "key", Attributes: []string{"title"}},
	}, nil, true))

	// Create document.
	created, err := docDB.CreateDocument(ctx, projectID, "app", "posts", databases.Document{
		Data: map[string]any{
			"title": "Hello World",
			"views": 42,
		},
	}, []databases.Permission{
		{Type: "read", Role: "any"},
		{Type: "update", Role: "any"},
		{Type: "delete", Role: "any"},
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)

	// Get document.
	got, err := docDB.GetDocument(ctx, projectID, "app", "posts", created.ID, databases.Principal{Roles: []string{"any"}})
	require.NoError(t, err)
	require.Equal(t, "Hello World", got.Data["title"])

	// Update document.
	updated, err := docDB.UpdateDocument(ctx, projectID, "app", "posts", databases.SimpleDocumentUpdate(databases.Document{
		ID: got.ID,
		Data: map[string]any{
			"views": 100,
		},
	}, nil), databases.Principal{Roles: []string{"any"}})
	require.NoError(t, err)
	require.Equal(t, float64(100), updated.Data["views"])

	// List with Appwrite-style query.
	list, err := docDB.ListDocuments(ctx, projectID, "app", "posts", databases.Query{
		Queries: []string{`greaterThan("views",50)`, `orderDesc("$createdAt")`},
	}, databases.Principal{Roles: []string{"any"}})
	require.NoError(t, err)
	require.Len(t, list.Documents, 1)
	require.Equal(t, int64(1), list.TotalCount)

	// Count.
	count, err := docDB.CountDocuments(ctx, projectID, "app", "posts", []string{`equal("title","Hello World")`}, databases.Principal{Roles: []string{"any"}})
	require.NoError(t, err)
	require.Equal(t, int64(1), count)

	// Delete.
	require.NoError(t, docDB.DeleteDocument(ctx, projectID, "app", "posts", created.ID, databases.Principal{Roles: []string{"any"}}))
	got2, err := docDB.GetDocument(ctx, projectID, "app", "posts", created.ID, databases.Principal{Roles: []string{"any"}})
	require.NoError(t, err)
	require.Nil(t, got2)
}

func TestPostgresDocumentDatabase_Permissions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))

	created, err := docDB.CreateDocument(ctx, projectID, "default", "users", databases.Document{
		Data: map[string]any{
			"email": "perm@torchwood.local",
			"name":  "Permission Test",
		},
	}, []databases.Permission{
		{Type: "read", Role: "user:alice"},
	}, databases.SystemPrincipal)
	require.NoError(t, err)

	// User without permission cannot read.
	list, err := docDB.ListDocuments(ctx, projectID, "default", "users", databases.Query{
		Queries: []string{`equal("$id","` + created.ID + `")`},
	}, databases.Principal{Roles: []string{"user:bob"}})
	require.NoError(t, err)
	require.Len(t, list.Documents, 0)

	// User with permission can read.
	list, err = docDB.ListDocuments(ctx, projectID, "default", "users", databases.Query{
		Queries: []string{`equal("$id","` + created.ID + `")`},
	}, databases.Principal{Roles: []string{"user:alice"}})
	require.NoError(t, err)
	require.Len(t, list.Documents, 1)

	// System roles bypass permissions.
	list, err = docDB.ListDocuments(ctx, projectID, "default", "users", databases.Query{}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(list.Documents), 1)

	// Get without permission is denied.
	_, err = docDB.GetDocument(ctx, projectID, "default", "users", created.ID, databases.Principal{Roles: []string{"user:bob"}})
	require.ErrorIs(t, err, ErrPermissionDenied)

	// Get with permission succeeds.
	got, err := docDB.GetDocument(ctx, projectID, "default", "users", created.ID, databases.Principal{Roles: []string{"user:alice"}})
	require.NoError(t, err)
	require.Equal(t, "perm@torchwood.local", got.Data["email"])
}

func TestEnsureSystemCollections_MultipleProjects(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	docDB := NewPostgresDocumentDB(db)

	projectA, internalA, cleanupA := testutil.CreateTestProject(ctx, db)
	defer cleanupA()
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectA, internalA))

	// Second project must use a unique name (projects.name is unique).
	projectB := &model.Project{
		ID:        "test-project-b",
		Name:      "Test Project B",
		Status:    "active",
		Settings:  map[string]any{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	_, err := db.NewInsert().Model(projectB).Exec(ctx)
	require.NoError(t, err)
	defer func() { _, _ = db.NewDelete().Model((*model.Project)(nil)).Where("id = ?", projectB.ID).Exec(ctx) }()
	var internalB int64
	require.NoError(t, db.NewSelect().Model((*model.Project)(nil)).Column("internal_id").Where("id = ?", projectB.ID).Scan(ctx, &internalB))

	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectB.ID, internalB))

	collA, err := docDB.GetCollection(ctx, projectA, "default", "users")
	require.NoError(t, err)
	require.NotNil(t, collA)

	collB, err := docDB.GetCollection(ctx, projectB.ID, "default", "users")
	require.NoError(t, err)
	require.NotNil(t, collB)
}

// TestErrDuplicateKey_DomainAlias (#8): infra alias must be the same instance
// as the domain error so errors.Is comparisons keep working.
func TestErrDuplicateKey_DomainAlias(t *testing.T) {
	require.Equal(t, databases.ErrDuplicateKey, ErrDuplicateKey)
	require.True(t, errors.Is(ErrDuplicateKey, databases.ErrDuplicateKey))
	require.True(t, errors.Is(databases.ErrDuplicateKey, ErrDuplicateKey))
}

// TestDeleteCollection_CleansPerms (#3): deleting a collection must remove its
// _perms rows so that recreating the same collection cannot leak old
// document-level permissions onto new documents.
func TestDeleteCollection_CleansPerms(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, internalID, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))

	createColl := func() {
		t.Helper()
		require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "notes", "Notes", []databases.Attribute{
			{ID: "title", Key: "title", Type: "string", Size: 256},
		}, nil, nil, true))
	}
	countPerms := func() int {
		t.Helper()
		var n int
		sql := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE _tenant = ? AND _collection = ?`, permsTableName(schemaName(internalID, "app")))
		require.NoError(t, db.DB.QueryRowContext(ctx, sql, internalID, "notes").Scan(&n))
		return n
	}

	createColl()
	_, err := docDB.CreateDocument(ctx, projectID, "app", "notes", databases.Document{
		ID:   "doc-1",
		Data: map[string]any{"title": "secret"},
	}, []databases.Permission{{Type: "read", Role: "user:alice"}}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Equal(t, 1, countPerms())

	// After deletion no _perms rows may remain for the collection.
	require.NoError(t, docDB.DeleteCollection(ctx, projectID, "app", "notes"))
	require.Equal(t, 0, countPerms())

	// Recreate the same collection and a document with the same ID but no
	// document-level permissions: alice must NOT see it (no old-perms leak).
	createColl()
	_, err = docDB.CreateDocument(ctx, projectID, "app", "notes", databases.Document{
		ID:   "doc-1",
		Data: map[string]any{"title": "fresh"},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)

	alice := databases.Principal{Roles: []string{"user:alice"}}
	list, err := docDB.ListDocuments(ctx, projectID, "app", "notes", databases.Query{Queries: []string{`equal("$id","doc-1")`}}, alice)
	require.NoError(t, err)
	require.Len(t, list.Documents, 0)

	// System can still see the recreated document.
	sys, err := docDB.ListDocuments(ctx, projectID, "app", "notes", databases.Query{Queries: []string{`equal("$id","doc-1")`}}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, sys.Documents, 1)
}

// TestListDocuments_MultiValueEqualNotEqual (#4): multi-value equal/notEqual
// must work on non-text columns (BIGINT) and keep single/multi value behavior
// on string columns.
func TestListDocuments_MultiValueEqualNotEqual(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "posts", "Posts", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
		{ID: "views", Key: "views", Type: "integer"},
	}, nil, nil, true))

	ids := make([]string, 0, 5)
	for i, title := range []string{"a", "b", "c", "d", "e"} {
		created, err := docDB.CreateDocument(ctx, projectID, "app", "posts", databases.Document{
			Data: map[string]any{"title": title, "views": i + 1},
		}, nil, databases.SystemPrincipal)
		require.NoError(t, err)
		ids = append(ids, created.ID)
	}
	_ = ids

	// Multi-value equal on an integer column.
	list, err := docDB.ListDocuments(ctx, projectID, "app", "posts", databases.Query{
		Queries: []string{`equal("views",[1,2,3])`},
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, list.Documents, 3)
	got := map[float64]bool{}
	for _, d := range list.Documents {
		got[d.Data["views"].(float64)] = true
	}
	require.True(t, got[1] && got[2] && got[3])

	// Multi-value notEqual on an integer column.
	list, err = docDB.ListDocuments(ctx, projectID, "app", "posts", databases.Query{
		Queries: []string{`notEqual("views",[1,2])`},
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, list.Documents, 3)
	got = map[float64]bool{}
	for _, d := range list.Documents {
		got[d.Data["views"].(float64)] = true
	}
	require.True(t, got[3] && got[4] && got[5])

	// Single value equal on a string column (regression).
	list, err = docDB.ListDocuments(ctx, projectID, "app", "posts", databases.Query{
		Queries: []string{`equal("title","a")`},
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, list.Documents, 1)
	require.Equal(t, "a", list.Documents[0].Data["title"])

	// Multi-value equal on a string column (regression).
	list, err = docDB.ListDocuments(ctx, projectID, "app", "posts", databases.Query{
		Queries: []string{`equal("title",["a","b"])`},
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, list.Documents, 2)

	// Single value notEqual on a string column (regression).
	list, err = docDB.ListDocuments(ctx, projectID, "app", "posts", databases.Query{
		Queries: []string{`notEqual("title","a")`},
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, list.Documents, 4)
}

// TestListDocuments_SelectProjection (#6a): select() must filter Data to the
// chosen keys while system fields always remain on the Document struct, with
// $id/$createdAt/$updatedAt aliases honored.
func TestListDocuments_SelectProjection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "profiles", "Profiles", []databases.Attribute{
		{ID: "name", Key: "name", Type: "string", Size: 256},
		{ID: "age", Key: "age", Type: "integer"},
		{ID: "email", Key: "email", Type: "string", Size: 256},
	}, nil, nil, true))

	created, err := docDB.CreateDocument(ctx, projectID, "app", "profiles", databases.Document{
		Data: map[string]any{"name": "alice", "age": 30, "email": "a@b.c"},
	}, nil, databases.SystemPrincipal)
	require.NoError(t, err)

	// Projection to ["name","age"]: Data only holds those keys.
	list, err := docDB.ListDocuments(ctx, projectID, "app", "profiles", databases.Query{
		Queries: []string{`select(["name","age"])`, `limit(10)`},
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, list.Documents, 1)
	doc := list.Documents[0]
	require.Equal(t, map[string]any{"name": "alice", "age": float64(30)}, doc.Data)
	require.NotEmpty(t, doc.ID)
	require.False(t, doc.CreatedAt.IsZero())

	// Projection to ["$id"]: alias maps to the system _id field, so Data is
	// empty while the system fields remain.
	list, err = docDB.ListDocuments(ctx, projectID, "app", "profiles", databases.Query{
		Queries: []string{`select(["$id"])`, `limit(10)`},
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, list.Documents, 1)
	doc = list.Documents[0]
	require.Empty(t, doc.Data)
	require.Equal(t, created.ID, doc.ID)
	require.False(t, doc.CreatedAt.IsZero())
}

// TestListDocuments_CursorPagination (#6b): cursorAfter/cursorBefore keyset
// pagination with default DESC ordering, explicit orderAsc, reverse cursor,
// and error cases (missing cursor doc / invalid order field).
func TestListDocuments_CursorPagination(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "seqdocs", "SeqDocs", []databases.Attribute{
		{ID: "seq", Key: "seq", Type: "integer"},
		{ID: "age", Key: "age", Type: "integer"},
	}, nil, nil, true))

	// Create d1..d4 with strictly increasing _created_at (d4 latest).
	for i := 1; i <= 4; i++ {
		_, err := docDB.CreateDocument(ctx, projectID, "app", "seqdocs", databases.Document{
			ID:   "d" + string(rune('0'+i)),
			Data: map[string]any{"seq": i, "age": 50 - i*10},
		}, nil, databases.SystemPrincipal)
		require.NoError(t, err)
		time.Sleep(2 * time.Millisecond)
	}
	orderDESC := []string{"d4", "d3", "d2", "d1"}

	// Default ordering (DESC) cursorAfter pagination: page 1 = [d4, d3],
	// page 2 with cursor on the last id of page 1 = [d2, d1] (no overlap,
	// list exhausted).
	page1, err := docDB.ListDocuments(ctx, projectID, "app", "seqdocs", databases.Query{
		Queries: []string{`limit(2)`},
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, page1.Documents, 2)
	require.Equal(t, orderDESC[:2], []string{page1.Documents[0].ID, page1.Documents[1].ID})

	page2, err := docDB.ListDocuments(ctx, projectID, "app", "seqdocs", databases.Query{
		Queries: []string{`limit(2)`, `cursorAfter("` + page1.Documents[1].ID + `")`},
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, page2.Documents, 2)
	require.Equal(t, orderDESC[2:], []string{page2.Documents[0].ID, page2.Documents[1].ID})
	require.NotEqual(t, page1.Documents[0].ID, page2.Documents[0].ID)
	require.NotEqual(t, page1.Documents[1].ID, page2.Documents[0].ID)

	// cursorAfter + orderAsc("age"): ages 10,20,30,40 → page1 [d4,d3],
	// page2 [d2,d1].
	asc1, err := docDB.ListDocuments(ctx, projectID, "app", "seqdocs", databases.Query{
		Queries: []string{`orderAsc("age")`, `limit(2)`},
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, asc1.Documents, 2)
	require.Equal(t, "d4", asc1.Documents[0].ID)
	require.Equal(t, "d3", asc1.Documents[1].ID)

	asc2, err := docDB.ListDocuments(ctx, projectID, "app", "seqdocs", databases.Query{
		Queries: []string{`orderAsc("age")`, `limit(2)`, `cursorAfter("` + asc1.Documents[1].ID + `")`},
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, asc2.Documents, 2)
	require.Equal(t, "d2", asc2.Documents[0].ID)
	require.Equal(t, "d1", asc2.Documents[1].ID)

	// cursorBefore reverse pagination (default DESC): the predicate
	// (created_at, _id) > cursor selects the rows before the cursor in the
	// DESC result order, i.e. the previous page [d4, d3] before d2.
	rev, err := docDB.ListDocuments(ctx, projectID, "app", "seqdocs", databases.Query{
		Queries: []string{`limit(2)`, `cursorBefore("d2")`},
	}, databases.SystemPrincipal)
	require.NoError(t, err)
	require.Len(t, rev.Documents, 2)
	require.Equal(t, "d4", rev.Documents[0].ID)
	require.Equal(t, "d3", rev.Documents[1].ID)

	// Cursor document does not exist → InvalidArgument.
	_, err = docDB.ListDocuments(ctx, projectID, "app", "seqdocs", databases.Query{
		Queries: []string{`limit(2)`, `cursorAfter("nope-not-exists")`},
	}, databases.SystemPrincipal)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	// Invalid order field in cursor mode → InvalidArgument (no silent skip).
	_, err = docDB.ListDocuments(ctx, projectID, "app", "seqdocs", databases.Query{
		Queries: []string{`orderAsc("bad field")`, `limit(2)`, `cursorAfter("d1")`},
	}, databases.SystemPrincipal)
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

// TestCreateDocument_AuditColumns (#12): _created_by/_updated_by are filled
// from the principal's first user:<id> role; keys-only principals leave them
// empty; user data cannot spoof _-prefixed audit fields.
func TestCreateDocument_AuditColumns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	defer db.Close()

	projectID, _, cleanup := testutil.CreateTestProject(ctx, db)
	defer cleanup()

	docDB := NewPostgresDocumentDB(db)
	require.NoError(t, docDB.EnsureSystemCollections(ctx, projectID, 0))
	require.NoError(t, docDB.CreateDatabase(ctx, projectID, "app", "Application DB"))
	perms := []databases.Permission{
		{Type: "create", Role: "user:abc"}, {Type: "read", Role: "user:abc"}, {Type: "update", Role: "user:abc"},
		{Type: "create", Role: "keys"}, {Type: "read", Role: "keys"}, {Type: "update", Role: "keys"},
	}
	require.NoError(t, docDB.CreateCollection(ctx, projectID, "app", "audit", "Audit", []databases.Attribute{
		{ID: "title", Key: "title", Type: "string", Size: 256},
	}, nil, perms, false))

	// user:abc principal: spoofed _created_at/_created_by/_updated_by in data
	// are ignored; audit columns carry "abc".
	created, err := docDB.CreateDocument(ctx, projectID, "app", "audit", databases.Document{
		Data: map[string]any{
			"title":        "t1",
			"_created_at":  "2000-01-01T00:00:00Z",
			"_created_by":  "spoof",
			"_updated_by":  "spoof",
			"not_a_col!":   "ignored",
		},
	}, nil, databases.Principal{Roles: []string{"user:abc"}})
	require.NoError(t, err)
	require.Equal(t, "abc", created.CreatedBy)
	require.Equal(t, "abc", created.UpdatedBy)
	require.False(t, created.CreatedAt.IsZero())
	require.WithinDuration(t, time.Now(), created.CreatedAt, time.Hour)
	require.NotContains(t, created.Data, "_created_at")
	require.NotContains(t, created.Data, "_created_by")
	require.NotContains(t, created.Data, "_updated_by")
	require.Equal(t, "t1", created.Data["title"])

	// Update as user:abc → UpdatedBy == "abc".
	updated, err := docDB.UpdateDocument(ctx, projectID, "app", "audit", databases.SimpleDocumentUpdate(databases.Document{
		ID:   created.ID,
		Data: map[string]any{"title": "t2"},
	}, nil), databases.Principal{Roles: []string{"user:abc"}})
	require.NoError(t, err)
	require.Equal(t, "abc", updated.UpdatedBy)
	require.Equal(t, "t2", updated.Data["title"])

	// keys-only principal (no user:<id> role) → audit columns empty.
	keysDoc, err := docDB.CreateDocument(ctx, projectID, "app", "audit", databases.Document{
		Data: map[string]any{"title": "k1"},
	}, nil, databases.Principal{Roles: []string{"keys"}})
	require.NoError(t, err)
	require.Empty(t, keysDoc.CreatedBy)
	require.Empty(t, keysDoc.UpdatedBy)
}
