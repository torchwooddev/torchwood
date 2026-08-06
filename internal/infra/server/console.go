package server

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/torchwoodio/torchwood/console"
)

// NewConsoleHandler serves the embedded Admin Console SPA.
func NewConsoleHandler() (http.Handler, error) {
	dist, err := fs.Sub(console.Dist, "dist")
	if err != nil {
		return nil, err
	}
	fileServer := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setConsoleSecurityHeaders(w)
		path := strings.TrimPrefix(r.URL.Path, "/console")
		path = strings.TrimPrefix(path, "/")
		if path != "" {
			if _, err := dist.Open(path); err != nil {
				// SPA fallback: serve index.html for unknown routes.
				path = ""
			}
		}
		// Rewrite the URL path so FileServer resolves against the embedded FS root.
		r.URL.Path = "/" + path
		fileServer.ServeHTTP(w, r)
	}), nil
}

// setConsoleSecurityHeaders hardens the Admin Console SPA responses. The Vite
// build emits no inline scripts, so script-src can stay 'self'; inline styles
// come from shadcn/Tailwind runtime and need 'unsafe-inline'.
//
// CSRF 说明：console 会话凭证由 SameSite=Lax 的 HttpOnly cookie 携带（见
// internal/api/consolegrpc/cookies.go），跨站 POST 不会附带 cookie；本服务
// 变更类端点均为 POST，故该前提已构成 CSRF 防护，无需额外的 CSRF token。
func setConsoleSecurityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:")
}
