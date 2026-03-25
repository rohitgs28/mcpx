// Package middleware provides shared HTTP middleware for the MCP gateway.
package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

type contextKey string

const requestIDKey contextKey = "request_id"

// HeaderRequestID is the HTTP header used to propagate request IDs.
const HeaderRequestID = "X-Request-ID"

// RequestID returns middleware that assigns a unique request ID to every
// incoming request. If the client sends an X-Request-ID header, that value
// is reused; otherwise a new random ID is generated. The ID is stored in
// the request context and echoed back in the response header.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(HeaderRequestID)
		if id == "" {
			id = generateID()
		}

		ctx := context.WithValue(r.Context(), requestIDKey, id)
		w.Header().Set(HeaderRequestID, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetRequestID extracts the request ID from the context. Returns an empty
// string if no request ID is present.
func GetRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// generateID produces a 16-character hex string (8 random bytes).
func generateID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback: this should never happen in practice
		return "00000000deadbeef"
	}
	return hex.EncodeToString(b)
}
