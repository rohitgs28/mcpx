// Package auth provides authentication middleware for the MCP gateway.
package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/rohitgs28/mcpx/internal/config"
)

func Middleware(cfg config.AuthConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if !cfg.Enabled { return next }
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch cfg.Type {
			case "bearer":
				h := r.Header.Get("Authorization")
				if !strings.HasPrefix(h, "Bearer ") {
					http.Error(w, `{"error":"missing Authorization header"}`, http.StatusUnauthorized); return
				}
				if subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(h, "Bearer ")), []byte(cfg.Token)) != 1 {
					http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized); return
				}
			case "api_key":
				hdr := cfg.Header; if hdr == "" { hdr = "X-API-Key" }
				if subtle.ConstantTimeCompare([]byte(r.Header.Get(hdr)), []byte(cfg.Token)) != 1 {
					http.Error(w, `{"error":"invalid API key"}`, http.StatusUnauthorized); return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
