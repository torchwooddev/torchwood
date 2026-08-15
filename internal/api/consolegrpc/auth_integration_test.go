package consolegrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/stretchr/testify/require"
	consolev1 "github.com/torchwooddev/torchwood/genproto/console/v1"
	"github.com/torchwooddev/torchwood/internal/app/console"
	"github.com/torchwooddev/torchwood/internal/app/server"
	"github.com/torchwooddev/torchwood/internal/infra/bun/bunrepo"
	"github.com/torchwooddev/torchwood/internal/infra/documentdb"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
	"github.com/torchwooddev/torchwood/internal/testutil"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"
)

type bootstrapFixture struct {
	url            string
	interceptorEnv *testutil.InterceptorEnv
}

// setupBootstrapFixture 用真实数据库装配 bootstrap 全链路：
// gateway(RegisterConsoleAuthServiceHandlerServer) + 真实 use-case +
// 生产同款认证拦截器（用于 x-api-key 冒烟）。
func setupBootstrapFixture(t *testing.T) *bootstrapFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	db := testutil.SetupTestDB(t)
	docDB := documentdb.NewPostgresDocumentDB(db, nil)
	cfg := &config.AppConfig{Security: &config.Security{
		Jwt:        &config.Security_Jwt{Secret: "bootstrap-integration-secret"},
		SetupToken: "bootstrap-setup-token",
	}}

	adminRepo := bunrepo.NewAdminRepository(db)
	projectRepo := bunrepo.NewProjectRepository(db)
	admins := console.NewAdmins(adminRepo)
	projects := server.NewProjects(projectRepo, docDB, db)
	apiKeys := server.NewAPIKeys(bunrepo.NewAPIKeyRepository(db))
	auth := console.NewAuth(cfg, adminRepo, nil, nil, nil)
	setupUC := console.NewSetup(cfg, admins, projects, apiKeys, auth, adminRepo, bunrepo.NewAdminProjectRepository(db), projectRepo)
	svc := NewAuthService(auth, setupUC)

	env, err := testutil.NewInterceptorEnv(db, cfg, docDB)
	require.NoError(t, err)

	// 生产网关用 authOutgoingHeaderMatcher 透传 set-cookie + protojson
	// （UseProtoNames）；测试 mux 对齐该配置。
	marshaler := &runtime.JSONPb{
		MarshalOptions:   protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: false},
		UnmarshalOptions: protojson.UnmarshalOptions{DiscardUnknown: true},
	}
	mux := runtime.NewServeMux(
		runtime.WithOutgoingHeaderMatcher(func(key string) (string, bool) {
			if key == "set-cookie" {
				return key, true
			}
			return runtime.DefaultHeaderMatcher(key)
		}),
		runtime.WithMarshalerOption("*", marshaler),
		runtime.WithMarshalerOption("*/*", marshaler),
		runtime.WithMarshalerOption("application/json", marshaler),
	)
	require.NoError(t, consolev1.RegisterConsoleAuthServiceHandlerServer(ctx, mux, svc))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return &bootstrapFixture{url: srv.URL, interceptorEnv: env}
}

type signUpPayload struct {
	Admin struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Role  string `json:"role"`
	} `json:"admin"`
	AccessToken         string `json:"access_token"`
	RefreshToken        string `json:"refresh_token"`
	DefaultAPIKeySecret string `json:"default_api_key_secret"`
}

func TestBootstrap_SignUpEndToEnd(t *testing.T) {
	fixture := setupBootstrapFixture(t)
	ctx := context.Background()

	// 1) 首次 sign-up：owner admin + 默认 project + 默认 API Key secret +
	//    会话 cookie。
	body := bytes.NewBufferString(`{"email":"owner@torchwood.local","password":"Pass@1234","setup_token":"bootstrap-setup-token"}`)
	resp, err := http.Post(fixture.url+"/v1/console/auth/sign-up", "application/json", body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var payload signUpPayload
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	resp.Body.Close()

	require.NotEmpty(t, payload.Admin.Role)
	require.Equal(t, "owner", payload.Admin.Role)
	require.Equal(t, "owner@torchwood.local", payload.Admin.Email)
	require.NotEmpty(t, payload.AccessToken)
	require.NotEmpty(t, payload.RefreshToken)
	require.NotEmpty(t, payload.DefaultAPIKeySecret)

	// 会话 cookie 已下发（SignUp 复用 setSessionCookies）。
	var foundSession, foundRefresh bool
	for _, c := range resp.Header.Values("Set-Cookie") {
		foundSession = foundSession || strings.Contains(c, "TORCHWOOD_session_console=")
		foundRefresh = foundRefresh || strings.Contains(c, "TORCHWOOD_console_refresh=")
	}
	require.True(t, foundSession, "session cookie missing: %v", resp.Header.Values("Set-Cookie"))
	require.True(t, foundRefresh, "refresh cookie missing: %v", resp.Header.Values("Set-Cookie"))

	// 2) setup-status 返回 needs_setup=false。
	resp, err = http.Get(fixture.url + "/v1/console/auth/setup-status")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var statusResp struct {
		NeedsSetup bool `json:"needs_setup"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&statusResp))
	resp.Body.Close()
	require.False(t, statusResp.NeedsSetup)

	// 3) 二次 sign-up → FailedPrecondition（grpc-gateway 映射为 HTTP 400）。
	body = bytes.NewBufferString(`{"email":"second@torchwood.local","password":"Pass@1234","setup_token":"bootstrap-setup-token"}`)
	resp, err = http.Post(fixture.url+"/v1/console/auth/sign-up", "application/json", body)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var errResp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&errResp))
	resp.Body.Close()
	require.Contains(t, errResp.Message, "setup already completed")

	// 4) 用返回的 secret 以 x-api-key 调用 UsersService/ListUsers：all scope
	//    生效，认证拦截器放行。
	err = fixture.interceptorEnv.InvokeUnary(ctx, testutil.MethodListUsers,
		metadata.Pairs("x-api-key", payload.DefaultAPIKeySecret))
	require.NoError(t, err)
}

func TestBootstrap_SetupStatusBeforeSignUp(t *testing.T) {
	fixture := setupBootstrapFixture(t)

	resp, err := http.Get(fixture.url + "/v1/console/auth/setup-status")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var statusResp struct {
		NeedsSetup bool `json:"needs_setup"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&statusResp))
	resp.Body.Close()
	require.True(t, statusResp.NeedsSetup)
}
