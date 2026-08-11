package torchwood

import (
	"context"
	"net"
	"sync"
	"testing"

	clientv1 "github.com/torchwooddev/torchwood/genproto/client/v1"
	serverv1 "github.com/torchwooddev/torchwood/genproto/server/v1"
	sharedv1 "github.com/torchwooddev/torchwood/genproto/shared/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

// recorder 记录 fake 服务收到的请求与鉴权元数据，供断言使用。
type recorder struct {
	mu              sync.Mutex
	lastAuth        metadata.MD
	lastCollection  *serverv1.CreateCollectionRequest
	lastGetDocument *clientv1.GetDocumentRequest
	createdUser     *serverv1.CreateUserRequest
	upserts         []*serverv1.UpsertDocumentRequest
	upsertCount     int
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

func (r *recorder) addUpsert(req *serverv1.UpsertDocumentRequest) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.upserts = append(r.upserts, req)
	r.upsertCount++
}

// --- fakeClientServer 实现 Client API 服务 ---

type fakeClientServer struct {
	rec *recorder
	clientv1.UnimplementedAccountServiceServer
	clientv1.UnimplementedTeamsServiceServer
	clientv1.UnimplementedDatabasesServiceServer
}

func (s *fakeClientServer) SignUp(ctx context.Context, req *clientv1.SignUpRequest) (*clientv1.SignUpResponse, error) {
	s.rec.record(ctx)
	return &clientv1.SignUpResponse{
		Account: &clientv1.Account{Id: "acc-1", Email: req.Email, Name: req.Name},
	}, nil
}

func (s *fakeClientServer) SignIn(ctx context.Context, req *clientv1.SignInRequest) (*clientv1.SignInResponse, error) {
	s.rec.record(ctx)
	return &clientv1.SignInResponse{
		Account: &clientv1.Account{Id: "acc-1", Email: req.Email, Name: "Test User"},
		Tokens:  &clientv1.TokenBundle{AccessToken: "jwt-1", RefreshToken: "rt-1"},
	}, nil
}

func (s *fakeClientServer) RefreshToken(ctx context.Context, _ *clientv1.RefreshTokenRequest) (*clientv1.RefreshTokenResponse, error) {
	s.rec.record(ctx)
	return &clientv1.RefreshTokenResponse{
		Tokens: &clientv1.TokenBundle{AccessToken: "jwt-2", RefreshToken: "rt-2"},
	}, nil
}

func (s *fakeClientServer) Me(ctx context.Context, _ *clientv1.MeRequest) (*clientv1.Account, error) {
	s.rec.record(ctx)
	return &clientv1.Account{Id: "acc-1", Email: "a@example.com", Name: "Test User"}, nil
}

func (s *fakeClientServer) SignOut(ctx context.Context, _ *clientv1.SignOutRequest) (*sharedv1.Empty, error) {
	s.rec.record(ctx)
	return &sharedv1.Empty{}, nil
}

func (s *fakeClientServer) CreateTeam(ctx context.Context, req *clientv1.CreateTeamRequest) (*clientv1.Team, error) {
	s.rec.record(ctx)
	return &clientv1.Team{Id: "team-1", Name: req.Name}, nil
}

func (s *fakeClientServer) GetTeam(ctx context.Context, req *clientv1.GetTeamRequest) (*clientv1.Team, error) {
	s.rec.record(ctx)
	return &clientv1.Team{Id: req.Id, Name: "Team One"}, nil
}

func (s *fakeClientServer) ListTeams(ctx context.Context, _ *sharedv1.ListRequest) (*clientv1.ListTeamsResponse, error) {
	s.rec.record(ctx)
	return &clientv1.ListTeamsResponse{Teams: []*clientv1.Team{{Id: "team-1", Name: "Team One"}}}, nil
}

func (s *fakeClientServer) CreateMembership(ctx context.Context, req *clientv1.CreateMembershipRequest) (*clientv1.Membership, error) {
	s.rec.record(ctx)
	return &clientv1.Membership{Id: "mem-1", TeamId: req.TeamId, Email: req.Email, Roles: req.Roles}, nil
}

func (s *fakeClientServer) ListMemberships(ctx context.Context, req *clientv1.ListMembershipsRequest) (*clientv1.ListMembershipsResponse, error) {
	s.rec.record(ctx)
	return &clientv1.ListMembershipsResponse{Memberships: []*clientv1.Membership{{Id: "mem-1", TeamId: req.TeamId}}}, nil
}

func (s *fakeClientServer) UpdateMembershipStatus(ctx context.Context, req *clientv1.UpdateMembershipStatusRequest) (*clientv1.Membership, error) {
	s.rec.record(ctx)
	return &clientv1.Membership{Id: req.MembershipId, TeamId: req.TeamId, Status: req.Status}, nil
}

func (s *fakeClientServer) DeleteMembership(ctx context.Context, _ *clientv1.GetMembershipRequest) (*sharedv1.Empty, error) {
	s.rec.record(ctx)
	return &sharedv1.Empty{}, nil
}

func (s *fakeClientServer) CreateDocument(ctx context.Context, req *clientv1.CreateDocumentRequest) (*clientv1.Document, error) {
	s.rec.record(ctx)
	return &clientv1.Document{Id: req.DocumentId, Data: req.Data, Permissions: req.Permissions}, nil
}

func (s *fakeClientServer) GetDocument(ctx context.Context, req *clientv1.GetDocumentRequest) (*clientv1.Document, error) {
	s.rec.record(ctx)
	s.rec.mu.Lock()
	s.rec.lastGetDocument = req
	s.rec.mu.Unlock()
	return &clientv1.Document{Id: req.DocumentId}, nil
}

func (s *fakeClientServer) UpsertDocument(ctx context.Context, req *clientv1.UpsertDocumentRequest) (*clientv1.Document, error) {
	s.rec.record(ctx)
	s.rec.addUpsert(&serverv1.UpsertDocumentRequest{
		DatabaseId:      req.DatabaseId,
		CollectionId:    req.CollectionId,
		DocumentId:      req.DocumentId,
		Data:            req.Data,
		Permissions:     req.Permissions,
		ConflictColumns: req.ConflictColumns,
	})
	return &clientv1.Document{Id: req.DocumentId, Data: req.Data}, nil
}

func (s *fakeClientServer) UpdateDocument(ctx context.Context, req *clientv1.UpdateDocumentRequest) (*clientv1.Document, error) {
	s.rec.record(ctx)
	return &clientv1.Document{Id: req.DocumentId, Data: req.Data}, nil
}

func (s *fakeClientServer) DeleteDocument(ctx context.Context, _ *clientv1.GetDocumentRequest) (*sharedv1.Empty, error) {
	s.rec.record(ctx)
	return &sharedv1.Empty{}, nil
}

func (s *fakeClientServer) ListDocuments(ctx context.Context, _ *clientv1.ListDocumentsRequest) (*clientv1.ListDocumentsResponse, error) {
	s.rec.record(ctx)
	return &clientv1.ListDocumentsResponse{Documents: []*clientv1.Document{{Id: "d1"}}}, nil
}

func (s *fakeClientServer) CountDocuments(ctx context.Context, _ *clientv1.ListDocumentsRequest) (*clientv1.CountDocumentsResponse, error) {
	s.rec.record(ctx)
	return &clientv1.CountDocumentsResponse{Count: 7}, nil
}

// --- fakeServerServer 实现 Server API 服务 ---

type fakeServerServer struct {
	rec *recorder
	serverv1.UnimplementedHealthServiceServer
	serverv1.UnimplementedUsersServiceServer
	serverv1.UnimplementedTeamsServiceServer
	serverv1.UnimplementedDatabasesServiceServer
}

func (s *fakeServerServer) Check(ctx context.Context, _ *serverv1.HealthCheckRequest) (*serverv1.HealthCheckResponse, error) {
	s.rec.record(ctx)
	return &serverv1.HealthCheckResponse{Status: "SERVING"}, nil
}

func (s *fakeServerServer) GetVersion(ctx context.Context, _ *serverv1.GetVersionRequest) (*serverv1.GetVersionResponse, error) {
	s.rec.record(ctx)
	return &serverv1.GetVersionResponse{Version: "v0.1.0", Commit: "abc123"}, nil
}

func (s *fakeServerServer) CreateUser(ctx context.Context, req *serverv1.CreateUserRequest) (*serverv1.User, error) {
	s.rec.record(ctx)
	s.rec.mu.Lock()
	s.rec.createdUser = req
	s.rec.mu.Unlock()
	return &serverv1.User{Id: "user-1", Email: req.Email, Name: req.Name, Status: req.Status}, nil
}

func (s *fakeServerServer) GetUser(ctx context.Context, req *serverv1.GetUserRequest) (*serverv1.User, error) {
	s.rec.record(ctx)
	return &serverv1.User{Id: req.Id, Email: "agent-1@agents.local", Name: "Agent One"}, nil
}

func (s *fakeServerServer) ListUsers(ctx context.Context, _ *sharedv1.ListRequest) (*serverv1.ListUsersResponse, error) {
	s.rec.record(ctx)
	return &serverv1.ListUsersResponse{Users: []*serverv1.User{{Id: "user-1", Email: "a@example.com"}}}, nil
}

func (s *fakeServerServer) UpdateUser(ctx context.Context, req *serverv1.UpdateUserRequest) (*serverv1.User, error) {
	s.rec.record(ctx)
	return &serverv1.User{Id: req.Id, Name: req.Name, Status: req.Status}, nil
}

func (s *fakeServerServer) DeleteUser(ctx context.Context, _ *serverv1.GetUserRequest) (*sharedv1.Empty, error) {
	s.rec.record(ctx)
	return &sharedv1.Empty{}, nil
}

func (s *fakeServerServer) ListUserSessions(ctx context.Context, _ *serverv1.GetUserRequest) (*serverv1.ListUserSessionsResponse, error) {
	s.rec.record(ctx)
	return &serverv1.ListUserSessionsResponse{Sessions: []*serverv1.Session{{Id: "sess-1"}}}, nil
}

func (s *fakeServerServer) DeleteUserSession(ctx context.Context, _ *serverv1.DeleteUserSessionRequest) (*sharedv1.Empty, error) {
	s.rec.record(ctx)
	return &sharedv1.Empty{}, nil
}

func (s *fakeServerServer) CreateUserToken(ctx context.Context, _ *serverv1.GetUserRequest) (*serverv1.CreateUserTokenResponse, error) {
	s.rec.record(ctx)
	return &serverv1.CreateUserTokenResponse{Tokens: &serverv1.TokenBundle{AccessToken: "agent-token"}}, nil
}

func (s *fakeServerServer) CreateTeam(ctx context.Context, req *serverv1.CreateTeamRequest) (*serverv1.Team, error) {
	s.rec.record(ctx)
	return &serverv1.Team{Id: "team-1", Name: req.Name, Permissions: req.Permissions}, nil
}

func (s *fakeServerServer) GetTeam(ctx context.Context, req *serverv1.GetTeamRequest) (*serverv1.Team, error) {
	s.rec.record(ctx)
	return &serverv1.Team{Id: req.Id, Name: "Team One"}, nil
}

func (s *fakeServerServer) ListTeams(ctx context.Context, _ *sharedv1.ListRequest) (*serverv1.ListTeamsResponse, error) {
	s.rec.record(ctx)
	return &serverv1.ListTeamsResponse{Teams: []*serverv1.Team{{Id: "team-1", Name: "Team One"}}}, nil
}

func (s *fakeServerServer) CreateMembership(ctx context.Context, req *serverv1.CreateMembershipRequest) (*serverv1.Membership, error) {
	s.rec.record(ctx)
	return &serverv1.Membership{Id: "mem-1", TeamId: req.TeamId, UserId: req.UserId, Roles: req.Roles, Status: req.Status}, nil
}

func (s *fakeServerServer) ListMemberships(ctx context.Context, req *serverv1.ListMembershipsRequest) (*serverv1.ListMembershipsResponse, error) {
	s.rec.record(ctx)
	return &serverv1.ListMembershipsResponse{Memberships: []*serverv1.Membership{{Id: "mem-1", TeamId: req.TeamId}}}, nil
}

func (s *fakeServerServer) GetMembership(ctx context.Context, req *serverv1.GetMembershipRequest) (*serverv1.Membership, error) {
	s.rec.record(ctx)
	return &serverv1.Membership{Id: req.MembershipId, TeamId: req.TeamId}, nil
}

func (s *fakeServerServer) UpdateMembership(ctx context.Context, req *serverv1.UpdateMembershipRequest) (*serverv1.Membership, error) {
	s.rec.record(ctx)
	return &serverv1.Membership{Id: req.MembershipId, TeamId: req.TeamId, Roles: req.Roles}, nil
}

func (s *fakeServerServer) UpdateMembershipStatus(ctx context.Context, req *serverv1.UpdateMembershipStatusRequest) (*serverv1.Membership, error) {
	s.rec.record(ctx)
	return &serverv1.Membership{Id: req.MembershipId, TeamId: req.TeamId, Status: req.Status}, nil
}

func (s *fakeServerServer) DeleteMembership(ctx context.Context, _ *serverv1.GetMembershipRequest) (*sharedv1.Empty, error) {
	s.rec.record(ctx)
	return &sharedv1.Empty{}, nil
}

func (s *fakeServerServer) CreateDatabase(ctx context.Context, req *serverv1.CreateDatabaseRequest) (*serverv1.Database, error) {
	s.rec.record(ctx)
	return &serverv1.Database{Id: req.Id, Name: req.Name}, nil
}

func (s *fakeServerServer) GetDatabase(ctx context.Context, req *serverv1.GetDatabaseRequest) (*serverv1.Database, error) {
	s.rec.record(ctx)
	return &serverv1.Database{Id: req.Id, Name: req.Id}, nil
}

func (s *fakeServerServer) ListDatabases(ctx context.Context, _ *sharedv1.ListRequest) (*serverv1.ListDatabasesResponse, error) {
	s.rec.record(ctx)
	return &serverv1.ListDatabasesResponse{Databases: []*serverv1.Database{{Id: "app"}}}, nil
}

func (s *fakeServerServer) CreateCollection(ctx context.Context, req *serverv1.CreateCollectionRequest) (*serverv1.Collection, error) {
	s.rec.record(ctx)
	s.rec.mu.Lock()
	s.rec.lastCollection = req
	s.rec.mu.Unlock()
	return &serverv1.Collection{Id: req.Id, DatabaseId: req.DatabaseId, Name: req.Name, Permissions: req.Permissions}, nil
}

func (s *fakeServerServer) GetCollection(ctx context.Context, req *serverv1.GetCollectionRequest) (*serverv1.Collection, error) {
	s.rec.record(ctx)
	return &serverv1.Collection{Id: req.CollectionId, DatabaseId: req.DatabaseId, Name: req.CollectionId}, nil
}

func (s *fakeServerServer) ListCollections(ctx context.Context, _ *serverv1.ListCollectionsRequest) (*serverv1.ListCollectionsResponse, error) {
	s.rec.record(ctx)
	return &serverv1.ListCollectionsResponse{Collections: []*serverv1.Collection{{Id: "members"}}}, nil
}

func (s *fakeServerServer) CreateAttribute(ctx context.Context, req *serverv1.CreateAttributeRequest) (*serverv1.Attribute, error) {
	s.rec.record(ctx)
	return &serverv1.Attribute{Id: "attr-" + req.Key, Key: req.Key, Type: req.Type, Required: req.Required, Array: req.Array}, nil
}

func (s *fakeServerServer) CreateIndex(ctx context.Context, req *serverv1.CreateIndexRequest) (*serverv1.Index, error) {
	s.rec.record(ctx)
	return &serverv1.Index{Id: req.Id, Type: req.Type, Attributes: req.Attributes}, nil
}

func (s *fakeServerServer) CreateDocument(ctx context.Context, req *serverv1.CreateDocumentRequest) (*serverv1.Document, error) {
	s.rec.record(ctx)
	return &serverv1.Document{Id: req.DocumentId, Data: req.Data, Permissions: req.Permissions}, nil
}

func (s *fakeServerServer) GetDocument(ctx context.Context, req *serverv1.GetDocumentRequest) (*serverv1.Document, error) {
	s.rec.record(ctx)
	return &serverv1.Document{Id: req.DocumentId}, nil
}

func (s *fakeServerServer) UpdateDocument(ctx context.Context, req *serverv1.UpdateDocumentRequest) (*serverv1.Document, error) {
	s.rec.record(ctx)
	return &serverv1.Document{Id: req.DocumentId, Data: req.Data, Permissions: req.Permissions}, nil
}

func (s *fakeServerServer) UpsertDocument(ctx context.Context, req *serverv1.UpsertDocumentRequest) (*serverv1.Document, error) {
	s.rec.record(ctx)
	s.rec.addUpsert(req)
	return &serverv1.Document{Id: req.DocumentId, Data: req.Data, Permissions: req.Permissions}, nil
}

func (s *fakeServerServer) DeleteDocument(ctx context.Context, _ *serverv1.GetDocumentRequest) (*sharedv1.Empty, error) {
	s.rec.record(ctx)
	return &sharedv1.Empty{}, nil
}

func (s *fakeServerServer) ListDocuments(ctx context.Context, _ *serverv1.ListDocumentsRequest) (*serverv1.ListDocumentsResponse, error) {
	s.rec.record(ctx)
	return &serverv1.ListDocumentsResponse{
		Documents: []*serverv1.Document{{Id: "d1"}, {Id: "d2"}},
		Meta:      &sharedv1.ListResponseMeta{NextPageToken: "next-token"},
	}, nil
}

func (s *fakeServerServer) CountDocuments(ctx context.Context, _ *serverv1.ListDocumentsRequest) (*serverv1.CountDocumentsResponse, error) {
	s.rec.record(ctx)
	return &serverv1.CountDocumentsResponse{Count: 42}, nil
}

func (s *fakeServerServer) BulkUpdateDocuments(ctx context.Context, req *serverv1.BulkUpdateDocumentsRequest) (*serverv1.BulkDocumentsResponse, error) {
	s.rec.record(ctx)
	return &serverv1.BulkDocumentsResponse{Affected: int64(len(req.DocumentIds))}, nil
}

func (s *fakeServerServer) BulkDeleteDocuments(ctx context.Context, req *serverv1.BulkDeleteDocumentsRequest) (*serverv1.BulkDocumentsResponse, error) {
	s.rec.record(ctx)
	return &serverv1.BulkDocumentsResponse{Affected: int64(len(req.DocumentIds))}, nil
}

// newBufconnServer 启动一个注册了 Client 与 Server API fake 服务的 bufconn gRPC 服务。
func newBufconnServer(t *testing.T) (*bufconn.Listener, *recorder) {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	rec := &recorder{}
	srv := grpc.NewServer()
	clientSrv := &fakeClientServer{rec: rec}
	serverSrv := &fakeServerServer{rec: rec}
	clientv1.RegisterAccountServiceServer(srv, clientSrv)
	clientv1.RegisterTeamsServiceServer(srv, clientSrv)
	clientv1.RegisterDatabasesServiceServer(srv, clientSrv)
	serverv1.RegisterHealthServiceServer(srv, serverSrv)
	serverv1.RegisterUsersServiceServer(srv, serverSrv)
	serverv1.RegisterTeamsServiceServer(srv, serverSrv)
	serverv1.RegisterDatabasesServiceServer(srv, serverSrv)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return lis, rec
}

func bufconnDialer(lis *bufconn.Listener) grpc.DialOption {
	return grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		return lis.DialContext(ctx)
	})
}

// clientAPI 返回连接 fake bufconn 的 Client（Client API）。
func clientAPI(t *testing.T, opts ...Option) (*Client, *recorder) {
	t.Helper()
	lis, rec := newBufconnServer(t)
	opts = append(opts, WithDialOptions(bufconnDialer(lis)))
	c, err := NewClient("passthrough:///bufconn", opts...)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, rec
}

// serverAPI 返回连接 fake bufconn 的 ServerClient（Server API）。
func serverAPI(t *testing.T, opts ...Option) (*ServerClient, *recorder) {
	t.Helper()
	lis, rec := newBufconnServer(t)
	opts = append(opts, WithDialOptions(bufconnDialer(lis)))
	c, err := NewServerClient("passthrough:///bufconn", opts...)
	if err != nil {
		t.Fatalf("NewServerClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, rec
}
