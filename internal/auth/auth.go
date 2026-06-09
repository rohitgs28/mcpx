// Package auth provides authentication middleware for the MCP gateway.
package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/rohitgs28/mcpx/internal/config"
	"github.com/rohitgs28/mcpx/internal/httperr"
)

func Middleware(cfg config.AuthConfig) func(http.Handler) http.Handler {
	// Build the OAuth validator once (it caches JWKS keys), not per request.
	var validator *OAuthValidator
	if cfg.Enabled && cfg.Type == "oauth" && cfg.OAuth != nil {
		validator = NewOAuthValidator(*cfg.OAuth)
	}
	return func(next http.Handler) http.Handler {
		if !cfg.Enabled {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch cfg.Type {
			case "bearer":
				h := r.Header.Get("Authorization")
				if !strings.HasPrefix(h, "Bearer ") {
					httperr.Write(w, http.StatusUnauthorized, httperr.CodeUnauthorized, "missing Authorization header")
					return
				}
				if subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(h, "Bearer ")), []byte(cfg.Token)) != 1 {
					httperr.Write(w, http.StatusUnauthorized, httperr.CodeUnauthorized, "invalid token")
					return
				}
			case "api_key":
				hdr := cfg.Header
				if hdr == "" {
					hdr = "X-API-Key"
				}
				if subtle.ConstantTimeCompare([]byte(r.Header.Get(hdr)), []byte(cfg.Token)) != 1 {
					httperr.Write(w, http.StatusUnauthorized, httperr.CodeUnauthorized, "invalid API key")
					return
				}
			case "oauth":
				h := r.Header.Get("Authorization")
				if !strings.HasPrefix(h, "Bearer ") {
					w.Header().Set("WWW-Authenticate", challenge(validator.cfg.Resource))
					httperr.Write(w, http.StatusUnauthorized, httperr.CodeUnauthorized, "missing bearer token")
					return
				}
				if err := validator.Validate(strings.TrimPrefix(h, "Bearer ")); err != nil {
					w.Header().Set("WWW-Authenticate", challenge(validator.cfg.Resource))
					httperr.Write(w, http.StatusUnauthorized, httperr.CodeUnauthorized, "invalid token")
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
