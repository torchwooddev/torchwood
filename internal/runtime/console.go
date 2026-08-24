package runtime

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/torchwooddev/torchwood/console"
)

// NewConsoleHandler serves the embedded Admin Console SPA.
// P3-10：index.html no-cache、assets/* immutable、资源 404 不回退 index.html。
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
		// 资源 404 不回退 index.html：带扩展名或 assets/* 的缺失直接 404
		if path != "" {
			if _, err := dist.Open(path); err != nil {
				if isConsoleAssetPath(path) {
					http.NotFound(w, r)
					return
				}
				// SPA fallback: 仅对无扩展名的前端路由回退 index.html
				path = ""
			}
		}
		// 缓存头策略
		if path == "" || path == "index.html" {
			w.Header().Set("Cache-Control", "no-cache")
		} else if len(path) >= 7 && path[:7] == "assets/" {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		// Rewrite the URL path so FileServer resolves against the embedded FS root.
		r.URL.Path = "/" + path
		fileServer.ServeHTTP(w, r)
	}), nil
}

// isConsoleAssetPath 判定是否为静态资源路径：assets/* 或带文件扩展名的请求，缺失时应 404 而非回退 index.html。
func isConsoleAssetPath(path string) bool {
	if len(path) >= 7 && path[:7] == "assets/" {
		return true
	}
	// 带点号的路径视作文件资源（如 favicon.ico、manifest.json）
	if idx := lastDotIndex(path); idx >= 0 {
		// 确保后缀在最后一段路径中（不跨目录）
		if slash := lastSlashIndex(path); slash < idx {
			return true
		}
	}
	return false
}

func lastDotIndex(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '.' {
			return i
		}
		if s[i] == '/' {
			break
		}
	}
	return -1
}

func lastSlashIndex(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return i
		}
	}
	return -1
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
