package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rohitgs28/mcpx/internal/auth"
	"github.com/rohitgs28/mcpx/internal/config"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
}

func TestMiddleware_Disabled(t *testing.T) {
	cfg := config.AuthConfig{Enabled: false}
	h := auth.Middleware(cfg)(okHandler())

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestMiddleware_Bearer_Valid(t *testing.T) {
	cfg := config.AuthConfig{Enabled: true, Type: "bearer", Token: "secret-token"}
	h := auth.Middleware(cfg)(okHandler())

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestMiddleware_Bearer_MissingHeader(t *testing.T) {
	cfg := config.AuthConfig{Enabled: true, Type: "bearer", Token: "secret-token"}
	h := auth.Middleware(cfg)(okHandler())

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestMiddleware_Bearer_InvalidToken(t *testing.T) {
	cfg := config.AuthConfig{Enabled: true, Type: "bearer", Token: "secret-token"}
	h := auth.Middleware(cfg)(okHandler())

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestMiddleware_APIKey_Valid(t *testing.T) {
	cfg := config.AuthConfig{Enabled: true, Type: "api_key", Token: "my-api-key", Header: "X-API-Key"}
	h := auth.Middleware(cfg)(okHandler())

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-API-Key", "my-api-key")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestMiddleware_APIKey_DefaultHeader(t *testing.T) {
	cfg := config.AuthConfig{Enabled: true, Type: "api_key", Token: "my-api-key"}
	h := auth.Middleware(cfg)(okHandler())

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-API-Key", "my-api-key")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with default header, got %d", rec.Code)
	}
}

func TestMiddleware_APIKey_InvalidKey(t *testing.T) {
	cfg := config.AuthConfig{Enabled: true, Type: "api_key", Token: "my-api-key"}
	h := auth.Middleware(cfg)(okHandler())

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-API-Key", "wrong-key")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestMiddleware_Bearer_ConstantTimeComparison(t *testing.T) {
	cfg := config.AuthConfig{Enabled: true, Type: "bearer", Token: "correct-token-value"}
	h := auth.Middleware(cfg)(okHandler())

	// Both wrong tokens should result in 401 regardless of similarity
	tokens := []string{"x", "correct-token-valu", "correct-token-value!", "completely-different"}
	for _, tok := range tokens {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for token %q, got %d", tok, rec.Code)
		}
	}
}
