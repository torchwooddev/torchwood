package client

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	clientv1 "github.com/torchwooddev/torchwood/genproto/client/v1"
	sharedv1 "github.com/torchwooddev/torchwood/genproto/shared/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// fakeAccount 记录 RefreshToken 调用次数，Me 可配置首次 401。
type fakeAccount struct {
	clientv1.UnimplementedAccountServiceServer
	refreshCalls atomic.Int32
	meCalls      atomic.Int32
	failFirstMe  atomic.Bool
	lastAuth     atomic.Value // []string
	tokens       *clientv1.TokenBundle
	refreshErr   error
	signInResp   *clientv1.SignInResponse
	signInErr    error
	signUpResp   *clientv1.SignUpResponse
	signUpErr    error
	signOutErr   error
}

func (f *fakeAccount) SignIn(ctx context.Context, req *clientv1.SignInRequest) (*clientv1.SignInResponse, error) {
	if f.signInErr != nil {
		return nil, f.signInErr
	}
	if f.signInResp == nil {
		return &clientv1.SignInResponse{Account: &clientv1.Account{Id: "acc-1", Email: req.Email}}, nil
	}
	return f.signInResp, nil
}

func (f *fakeAccount) SignUp(ctx context.Context, req *clientv1.SignUpRequest) (*clientv1.SignUpResponse, error) {
	if f.signUpErr != nil {
		return nil, f.signUpErr
	}
	if f.signUpResp == nil {
		return &clientv1.SignUpResponse{Account: &clientv1.Account{Id: "acc-1", Email: req.Email, Name: req.Name}}, nil
	}
	return f.signUpResp, nil
}

func (f *fakeAccount) SignOut(ctx context.Context, _ *clientv1.SignOutRequest) (*sharedv1.Empty, error) {
	if f.signOutErr != nil {
		return nil, f.signOutErr
	}
	return &sharedv1.Empty{}, nil
}

func (f *fakeAccount) RefreshToken(_ context.Context, req *clientv1.RefreshTokenRequest) (*clientv1.RefreshTokenResponse, error) {
	f.refreshCalls.Add(1)
	if f.refreshErr != nil {
		return nil, f.refreshErr
	}
	if req.RefreshToken != "refresh-1" {
		return nil, status.Error(codes.Unauthenticated, "invalid refresh token")
	}
	return &clientv1.RefreshTokenResponse{Tokens: f.tokens}, nil
}

func (f *fakeAccount) Me(ctx context.Context, _ *clientv1.MeRequest) (*clientv1.Account, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	f.lastAuth.Store(md.Get("authorization"))
	if f.failFirstMe.Load() && f.meCalls.Add(1) == 1 {
		return nil, status.Error(codes.Unauthenticated, "expired")
	}
	return &clientv1.Account{Id: "acc-1"}, nil
}

// newBufconn 启动注册了 AccountService fake 的 bufconn gRPC 服务。
func newBufconn(t *testing.T) (*bufconn.Listener, *fakeAccount) {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	fake := &fakeAccount{}
	srv := grpc.NewServer()
	clientv1.RegisterAccountServiceServer(srv, fake)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return lis, fake
}

func dialer(lis *bufconn.Listener) grpc.DialOption {
	return grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		return lis.DialContext(ctx)
	})
}

func newTestClient(t *testing.T, opts ...Option) (*Client, *fakeAccount) {
	t.Helper()
	lis, fake := newBufconn(t)
	opts = append(opts, WithDialOptions(dialer(lis)))
	c, err := New("passthrough:///bufconn", opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c, fake
}
