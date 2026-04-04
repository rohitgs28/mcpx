// Package cors provides CORS middleware for browser-based MCP clients.
//
// When MCP clients run in browsers (web-based IDEs, dashboards, custom UIs),
// they need CORS headers to connect to the gateway. This middleware handles
// preflight requests and adds the appropriate headers.
package cors

import (
	"net/http"
	"strings"
)

// Config holds CORS configuration.
type Config struct {
	// AllowedOrigins is the list of origins permitted to access the gateway.
	// Use ["*"] to allow all origins (not recommended for production).
	AllowedOrigins []string

	// AllowedMethods defaults to ["GET", "POST", "OPTIONS"].
	AllowedMethods []string

	// AllowedHeaders defaults to common MCP headers.
	AllowedHeaders []string

	// MaxAge is the preflight cache duration in seconds. Defaults to 86400 (24h).
	MaxAge string
}

// DefaultConfig returns a sensible default CORS configuration.
func DefaultConfig() Config {
	return Config{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{
			"Content-Type",
			"Authorization",
			"X-API-Key",
			"X-Request-ID",
			"Mcp-Session-Id",
		},
		MaxAge: "86400",
	}
}

// Middleware returns an http.Handler that wraps the given handler with CORS headers.
func Middleware(cfg Config, next http.Handler) http.Handler {
	origins := make(map[string]bool, len(cfg.AllowedOrigins))
	allowAll := false
	for _, o := range cfg.AllowedOrigins {
		if o == "*" {
			allowAll = true
			break
		}
		origins[o] = true
	}

	methods := strings.Join(cfg.AllowedMethods, ", ")
	headers := strings.Join(cfg.AllowedHeaders, ", ")
	maxAge := cfg.MaxAge
	if maxAge == "" {
		maxAge = "86400"
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		if allowAll {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else if origin != "" && origins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}

		w.Header().Set("Access-Control-Allow-Methods", methods)
		w.Header().Set("Access-Control-Allow-Headers", headers)
		w.Header().Set("Access-Control-Max-Age", maxAge)

		// Handle preflight
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
