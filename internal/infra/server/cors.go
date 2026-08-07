package server

import (
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/torchwooddev/torchwood/internal/pkg/config"
)

func CORSMiddleware(cfg *config.Http_Cors) func(http.Handler) http.Handler {
	allowed := cfg.GetAllowOrigins()
	credentials := cfg.GetAllowCredentials()
	if credentials {
		filtered := make([]string, 0, len(allowed))
		for _, o := range allowed {
			if o == "*" {
				log.Printf("[cors] warning: allow_credentials=true with wildcard origin is invalid per CORS spec; ignoring %q", o)
				continue
			}
			filtered = append(filtered, o)
		}
		allowed = filtered
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if isOriginAllowed(allowed, origin) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				if credentials && origin != "" {
					w.Header().Set("Vary", "Origin")
				}
			}
			if len(cfg.GetAllowMethods()) > 0 {
				w.Header().Set("Access-Control-Allow-Methods", strings.Join(cfg.GetAllowMethods(), ", "))
			}
			if len(cfg.GetAllowHeaders()) > 0 {
				w.Header().Set("Access-Control-Allow-Headers", strings.Join(cfg.GetAllowHeaders(), ", "))
			}
			if credentials {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			if cfg.GetMaxAge() > 0 {
				w.Header().Set("Access-Control-Max-Age", strconv.Itoa(int(cfg.GetMaxAge())))
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isOriginAllowed(allowed []string, origin string) bool {
	for _, o := range allowed {
		if o == "*" || strings.EqualFold(o, origin) {
			return true
		}
	}
	return false
}
