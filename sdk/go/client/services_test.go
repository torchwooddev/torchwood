package client

import (
	"context"
	"net"
	"sync"
	"testing"

	clientv1 "github.com/torchwooddev/torchwood/genproto/client/v1"
	sharedv1 "github.com/torchwooddev/torchwood/genproto/shared/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

// recorder 记录 fake 服务收到的 metadata 与关键请求，供断言使用。
type recorder struct {
	mu              sync.Mutex
	lastAuth        metadata.MD
	lastGetDocument *clientv1.GetDocumentRequest
	upserts         []*clientv1.UpsertDocumentRequest
}

func (r *recorder) record(ctx context.Context) {
	md, _ := metadata.FromIncomingContext(ctx)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastAuth = md
}

func (r *recorder) auth(key string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if vals := r.lastAuth.Get(key); len(vals) > 0 {
		return vals[0]
	}
	return ""
}

// fakeTeams 实现 Client API Teams 服务。
type fakeTeams struct {
	clientv1.UnimplementedTeamsServiceServer
	rec *recorder
}

func (s *fakeTeams) CreateTeam(ctx context.Context, req *clientv1.CreateTeamRequest) (*clientv1.Team, error) {
	s.rec.record(ctx)
	return &clientv1.Team{Id: "team-1", Name: req.Name}, nil
}

func (s *fakeTeams) GetTeam(ctx context.Context, req *clientv1.GetTeamRequest) (*clientv1.Team, error) {
	s.rec.record(ctx)
	return &clientv1.Team{Id: req.Id, Name: "Team One"}, nil
}

func (s *fakeTeams) ListTeams(ctx context.Context, _ *sharedv1.ListRequest) (*clientv1.ListTeamsResponse, error) {
	s.rec.record(ctx)
	return &clientv1.ListTeamsResponse{Teams: []*clientv1.Team{{Id: "team-1", Name: "Team One"}}}, nil
}

func (s *fakeTeams) CreateMembership(ctx context.Context, req *clientv1.CreateMembershipRequest) (*clientv1.Membership, error) {
	s.rec.record(ctx)
	return &clientv1.Membership{Id: "mem-1", TeamId: req.TeamId, Email: req.Email, Roles: req.Roles}, nil
}

func (s *fakeTeams) ListMemberships(ctx context.Context, req *clientv1.ListMembershipsRequest) (*clientv1.ListMembershipsResponse, error) {
	s.rec.record(ctx)
	return &clientv1.ListMembershipsResponse{Memberships: []*clientv1.Membership{{Id: "mem-1", TeamId: req.TeamId}}}, nil
}

func (s *fakeTeams) UpdateMembershipStatus(ctx context.Context, req *clientv1.UpdateMembershipStatusRequest) (*clientv1.Membership, error) {
	s.rec.record(ctx)
	return &clientv1.Membership{Id: req.MembershipId, TeamId: req.TeamId, Status: req.Status}, nil
}

func (s *fakeTeams) DeleteMembership(ctx context.Context, _ *clientv1.GetMembershipRequest) (*sharedv1.Empty, error) {
	s.rec.record(ctx)
	return &sharedv1.Empty{}, nil
}

// fakeDatabases 实现 Client API Databases 服务。
type fakeDatabases struct {
	clientv1.UnimplementedDatabasesServiceServer
	rec *recorder
}

func (s *fakeDatabases) CreateDocument(ctx context.Context, req *clientv1.CreateDocumentRequest) (*clientv1.Document, error) {
	s.rec.record(ctx)
	return &clientv1.Document{Id: req.DocumentId, Data: req.Data, Permissions: req.Permissions}, nil
}

func (s *fakeDatabases) GetDocument(ctx context.Context, req *clientv1.GetDocumentRequest) (*clientv1.Document, error) {
	s.rec.record(ctx)
	s.rec.mu.Lock()
	s.rec.lastGetDocument = req
	s.rec.mu.Unlock()
	return &clientv1.Document{Id: req.DocumentId}, nil
}

func (s *fakeDatabases) UpsertDocument(ctx context.Context, req *clientv1.UpsertDocumentRequest) (*clientv1.Document, error) {
	s.rec.record(ctx)
	s.rec.mu.Lock()
	s.rec.upserts = append(s.rec.upserts, req)
	s.rec.mu.Unlock()
	return &clientv1.Document{Id: req.DocumentId, Data: req.Data}, nil
}

func (s *fakeDatabases) UpdateDocument(ctx context.Context, req *clientv1.UpdateDocumentRequest) (*clientv1.Document, error) {
	s.rec.record(ctx)
	return &clientv1.Document{Id: req.DocumentId, Data: req.Data}, nil
}

func (s *fakeDatabases) DeleteDocument(ctx context.Context, _ *clientv1.GetDocumentRequest) (*sharedv1.Empty, error) {
	s.rec.record(ctx)
	return &sharedv1.Empty{}, nil
}

func (s *fakeDatabases) ListDocuments(ctx context.Context, _ *clientv1.ListDocumentsRequest) (*clientv1.ListDocumentsResponse, error) {
	s.rec.record(ctx)
	return &clientv1.ListDocumentsResponse{Documents: []*clientv1.Document{{Id: "d1"}}}, nil
}

func (s *fakeDatabases) CountDocuments(ctx context.Context, _ *clientv1.ListDocumentsRequest) (*clientv1.CountDocumentsResponse, error) {
	s.rec.record(ctx)
	return &clientv1.CountDocumentsResponse{Count: 7}, nil
}

// newFullBufconn 启动注册了 Account/Teams/Databases fake 的 bufconn gRPC 服务。
func newFullBufconn(t *testing.T) (*bufconn.Listener, *recorder) {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	rec := &recorder{}
	srv := grpc.NewServer()
	clientv1.RegisterAccountServiceServer(srv, &fakeAccount{})
	clientv1.RegisterTeamsServiceServer(srv, &fakeTeams{rec: rec})
	clientv1.RegisterDatabasesServiceServer(srv, &fakeDatabases{rec: rec})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return lis, rec
}

// newFullClient 返回连接 Account/Teams/Databases fake 的 Client API 客户端。
func newFullClient(t *testing.T, opts ...Option) (*Client, *recorder) {
	t.Helper()
	lis, rec := newFullBufconn(t)
	opts = append(opts, WithDialOptions(grpc.WithContextDialer(
		func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) })))
	c, err := New("passthrough:///bufconn", opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c, rec
}

func TestClientTeams_CreateAndList(t *testing.T) {
	c, _ := newFullClient(t, WithInitialTokens(&clientv1.TokenBundle{AccessToken: "jwt-1"}))
	ctx := context.Background()

	team, err := c.Teams.CreateTeam(ctx, "Team One")
	require.NoError(t, err)
	require.Equal(t, "team-1", team.Id)
	require.Equal(t, "Team One", team.Name)

	got, err := c.Teams.GetTeam(ctx, "team-1")
	require.NoError(t, err)
	require.Equal(t, "Team One", got.Name)

	list, err := c.Teams.ListTeams(ctx)
	require.NoError(t, err)
	require.Len(t, list.Teams, 1)
}

func TestClientTeams_Memberships(t *testing.T) {
	c, _ := newFullClient(t, WithInitialTokens(&clientv1.TokenBundle{AccessToken: "jwt-1"}))
	ctx := context.Background()

	mem, err := c.Teams.CreateMembership(ctx, "team-1", "bob@example.com", "Bob", []string{"member"})
	require.NoError(t, err)
	require.Equal(t, "team-1", mem.TeamId)
	require.Equal(t, []string{"member"}, mem.Roles)

	listed, err := c.Teams.ListMemberships(ctx, "team-1")
	require.NoError(t, err)
	require.Len(t, listed.Memberships, 1)

	updated, err := c.Teams.UpdateMembershipStatus(ctx, "team-1", "mem-1", "active")
	require.NoError(t, err)
	require.Equal(t, "active", updated.Status)

	require.NoError(t, c.Teams.DeleteMembership(ctx, "team-1", "mem-1"))
}

func TestClientDatabases_DocumentCRUD(t *testing.T) {
	c, _ := newFullClient(t, WithInitialTokens(&clientv1.TokenBundle{AccessToken: "jwt-1"}), WithDatabaseID("app"))
	ctx := context.Background()
	docs := c.Databases

	created, err := docs.CreateDocument(ctx, "members", "m1",
		map[string]any{"channel_id": "ch1", "user_id": "u1", "last_read_seq": 10},
		[]string{"read:user:u1"})
	require.NoError(t, err)
	require.Equal(t, "m1", created.Id)
	require.Equal(t, float64(10), created.Data.GetFields()["last_read_seq"].GetNumberValue())

	got, err := docs.GetDocument(ctx, "members", "m1")
	require.NoError(t, err)
	require.Equal(t, "m1", got.Id)

	updated, err := docs.UpdateDocument(ctx, "members", "m1",
		map[string]any{"last_read_seq": 42}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "m1", updated.Id)

	require.NoError(t, docs.DeleteDocument(ctx, "members", "m1"))
}

func TestClientDatabases_UpsertForwardsConflictColumns(t *testing.T) {
	c, rec := newFullClient(t, WithInitialTokens(&clientv1.TokenBundle{AccessToken: "jwt-1"}), WithDatabaseID("app"))
	ctx := context.Background()

	doc, err := c.Databases.UpsertDocument(ctx, "members", "m1",
		map[string]any{"channel_id": "ch1", "user_id": "u1", "last_read_seq": 42},
		[]string{"channel_id", "user_id"},
		[]string{"read:user:u1"})
	require.NoError(t, err)
	require.Equal(t, "m1", doc.Id)

	rec.mu.Lock()
	defer rec.mu.Unlock()
	require.Len(t, rec.upserts, 1)
	require.Equal(t, []string{"channel_id", "user_id"}, rec.upserts[0].ConflictColumns)
	require.Equal(t, []string{"read:user:u1"}, rec.upserts[0].Permissions)
	require.Equal(t, "app", rec.upserts[0].DatabaseId)
}

func TestClientDatabases_ListAndCount(t *testing.T) {
	c, _ := newFullClient(t, WithInitialTokens(&clientv1.TokenBundle{AccessToken: "jwt-1"}), WithDatabaseID("app"))
	ctx := context.Background()

	docs, next, err := c.Databases.ListDocuments(ctx, "members",
		[]string{`equal("channel_id","ch1")`}, 20, "")
	require.NoError(t, err)
	require.Len(t, docs, 1)
	require.Equal(t, "", next)

	count, err := c.Databases.CountDocuments(ctx, "members",
		[]string{`equal("channel_id","ch1")`})
	require.NoError(t, err)
	require.Equal(t, int64(7), count)
}

func TestClientDatabases_UseDatabaseOverride(t *testing.T) {
	c, rec := newFullClient(t, WithInitialTokens(&clientv1.TokenBundle{AccessToken: "jwt-1"}), WithDatabaseID("app"))
	ctx := context.Background()

	_, err := c.UseDatabase("other").GetDocument(ctx, "members", "m1")
	require.NoError(t, err)

	rec.mu.Lock()
	defer rec.mu.Unlock()
	require.NotNil(t, rec.lastGetDocument)
	require.Equal(t, "other", rec.lastGetDocument.DatabaseId)
	require.Equal(t, "app", c.Databases.db)
}
