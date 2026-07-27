package serverhttp

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/deeploop-ai/graviton/internal/app/client"
	"github.com/deeploop-ai/graviton/internal/pkg/contexts"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
)

// OAuthHandler handles browser OAuth2 callback redirects.
type OAuthHandler struct {
	account *client.Account
}

func NewOAuthHandler(account *client.Account) *OAuthHandler {
	return &OAuthHandler{account: account}
}

func (h *OAuthHandler) Register(mux *runtime.ServeMux) {
	_ = mux.HandlePath("GET", "/v1/account/oauth2/{provider}/callback", h.callback)
}

func (h *OAuthHandler) callback(w http.ResponseWriter, r *http.Request, pathParams map[string]string) {
	provider := strings.TrimSpace(pathParams["provider"])
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if provider == "" || code == "" || state == "" {
		http.Redirect(w, r, "/?error=invalid_oauth_callback", http.StatusFound)
		return
	}

	ctx := contexts.WithClientInfo(r.Context(), contexts.ClientInfo{
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
	})

	result, err := h.account.HandleOAuth2Callback(ctx, provider, code, state)
	if err != nil {
		target := "/?error=oauth_failed"
		if result != nil && result.RedirectURL != "" {
			target = result.RedirectURL
		}
		http.Redirect(w, r, target, http.StatusFound)
		return
	}

	if result.SessionCookie != "" && result.User != nil {
		http.SetCookie(w, &http.Cookie{
			Name:     fmt.Sprintf("GRAVITON_session_%s", result.ProjectID),
			Value:    result.SessionCookie,
			Path:     "/",
			HttpOnly: true,
			Secure:   r.TLS != nil,
			SameSite: http.SameSiteLaxMode,
		})
	}
	http.Redirect(w, r, result.RedirectURL, http.StatusFound)
}

func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		if idx := strings.Index(forwarded, ","); idx > 0 {
			return strings.TrimSpace(forwarded[:idx])
		}
		return strings.TrimSpace(forwarded)
	}
	if realIP := r.Header.Get("X-Real-Ip"); realIP != "" {
		return realIP
	}
	return strings.TrimSpace(r.RemoteAddr)
}
