package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

// oauthTokenServer 返回一个伪造的 token 端点，Exchange 从这里取得 access token。
func oauthTokenServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"test-token","token_type":"Bearer","expires_in":3600}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestGoogleOAuth_EmailVerifiedParsed(t *testing.T) {
	userInfo := map[string]any{
		"id":             "google-1",
		"email":          "alice@example.com",
		"email_verified": true,
		"name":           "Alice",
		"picture":        "https://example.com/alice.png",
	}
	userSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(userInfo)
	}))
	defer userSrv.Close()

	tokenSrv := oauthTokenServer(t)
	a := &genericOAuthAuthenticator{
		cfg:         &oauth2.Config{Endpoint: oauth2.Endpoint{TokenURL: tokenSrv.URL}},
		userInfoURL: userSrv.URL,
	}

	info, err := a.Exchange(context.Background(), "code", "verifier")
	require.NoError(t, err)
	require.Equal(t, "google-1", info.ProviderUID)
	require.Equal(t, "alice@example.com", info.Email)
	require.True(t, info.EmailVerified)
}

func TestGoogleOAuth_EmailVerifiedFalsePropagated(t *testing.T) {
	// Google Workspace 边缘账户：email_verified=false（管理员未验证/域未验证），
	// 必须原样传播，由 resolveOAuthUser 拒绝（安全评审 M8）。
	userSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":             "google-2",
			"email":          "unverified@example.com",
			"email_verified": false,
		})
	}))
	defer userSrv.Close()

	tokenSrv := oauthTokenServer(t)
	a := &genericOAuthAuthenticator{
		cfg:         &oauth2.Config{Endpoint: oauth2.Endpoint{TokenURL: tokenSrv.URL}},
		userInfoURL: userSrv.URL,
	}

	info, err := a.Exchange(context.Background(), "code", "verifier")
	require.NoError(t, err)
	require.Equal(t, "unverified@example.com", info.Email)
	require.False(t, info.EmailVerified)
}

func TestGoogleOAuth_MissingEmailVerifiedDefaultsFalse(t *testing.T) {
	// adapter 未填充 email_verified 时默认 false，强制校验逻辑兜底拒绝（fail-closed）。
	userSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "google-3",
			"email": "legacy@example.com",
		})
	}))
	defer userSrv.Close()

	tokenSrv := oauthTokenServer(t)
	a := &genericOAuthAuthenticator{
		cfg:         &oauth2.Config{Endpoint: oauth2.Endpoint{TokenURL: tokenSrv.URL}},
		userInfoURL: userSrv.URL,
	}

	info, err := a.Exchange(context.Background(), "code", "verifier")
	require.NoError(t, err)
	require.Equal(t, "legacy@example.com", info.Email)
	require.False(t, info.EmailVerified)
}

func TestGitHubOAuth_Exchange_MarksVerified(t *testing.T) {
	// GitHub 邮箱要么来自 public profile，要么经 fetchGitHubPrimaryEmail 强制
	// verified 过滤；email 非空即视为已验证（安全评审 M8）。
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":    123.0,
				"login": "octocat",
				"name":  "The Octocat",
				"email": "octo@example.com",
			})
		case "/user/emails":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"email": "octo@example.com", "primary": true, "verified": true},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer apiSrv.Close()
	oldBase := gitHubAPIBase
	gitHubAPIBase = strings.TrimSuffix(apiSrv.URL, "/")
	t.Cleanup(func() { gitHubAPIBase = oldBase })

	tokenSrv := oauthTokenServer(t)
	a := &githubOAuthAuthenticator{genericOAuthAuthenticator{
		cfg: &oauth2.Config{Endpoint: oauth2.Endpoint{TokenURL: tokenSrv.URL}},
	}}

	info, err := a.Exchange(context.Background(), "code", "verifier")
	require.NoError(t, err)
	require.Equal(t, "octo@example.com", info.Email)
	require.True(t, info.EmailVerified)
}

func TestGitHubOAuth_Exchange_VerifiedPrimaryEmailFallback(t *testing.T) {
	// profile email 为空时回退到 /user/emails 的 verified primary 邮箱。
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":    456.0,
				"login": "alice",
			})
		case "/user/emails":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"email": "noise@example.com", "primary": false, "verified": false},
				{"email": "alice@example.com", "primary": true, "verified": true},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer apiSrv.Close()
	oldBase := gitHubAPIBase
	gitHubAPIBase = strings.TrimSuffix(apiSrv.URL, "/")
	t.Cleanup(func() { gitHubAPIBase = oldBase })

	tokenSrv := oauthTokenServer(t)
	a := &githubOAuthAuthenticator{genericOAuthAuthenticator{
		cfg: &oauth2.Config{Endpoint: oauth2.Endpoint{TokenURL: tokenSrv.URL}},
	}}

	info, err := a.Exchange(context.Background(), "code", "verifier")
	require.NoError(t, err)
	require.Equal(t, "alice@example.com", info.Email)
	require.True(t, info.EmailVerified)
}

func TestGitHubOAuth_NoVerifiedEmailFails(t *testing.T) {
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 789.0, "login": "bob"})
		case "/user/emails":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"email": "bob@example.com", "primary": true, "verified": false},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer apiSrv.Close()
	oldBase := gitHubAPIBase
	gitHubAPIBase = strings.TrimSuffix(apiSrv.URL, "/")
	t.Cleanup(func() { gitHubAPIBase = oldBase })

	tokenSrv := oauthTokenServer(t)
	a := &githubOAuthAuthenticator{genericOAuthAuthenticator{
		cfg: &oauth2.Config{Endpoint: oauth2.Endpoint{TokenURL: tokenSrv.URL}},
	}}

	_, err := a.Exchange(context.Background(), "code", "verifier")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no verified email")
}
