package cors

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPreflightRequest(t *testing.T) {
	handler := Middleware(DefaultConfig(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("preflight should not reach the next handler")
	}))

	req := httptest.NewRequest("OPTIONS", "/mcp/test", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("expected *, got %s", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Headers"); got == "" {
		t.Error("expected allow-headers to be set")
	}
}

func TestAllowAllOrigins(t *testing.T) {
	handler := Middleware(DefaultConfig(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/mcp/test", nil)
	req.Header.Set("Origin", "http://any-origin.example.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("expected *, got %s", got)
	}
}

func TestSpecificOrigins(t *testing.T) {
	cfg := Config{
		AllowedOrigins: []string{"http://localhost:3000", "https://app.example.com"},
		AllowedMethods: []string{"GET", "POST"},
		AllowedHeaders: []string{"Content-Type", "Authorization"},
	}

	handler := Middleware(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Allowed origin
	req := httptest.NewRequest("POST", "/mcp/test", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Errorf("expected http://localhost:3000, got %s", got)
	}

	// Disallowed origin
	req2 := httptest.NewRequest("POST", "/mcp/test", nil)
	req2.Header.Set("Origin", "http://evil.com")
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	if got := w2.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected empty ACAO for disallowed origin, got %s", got)
	}
}

func TestPassesThroughNonPreflight(t *testing.T) {
	reached := false
	handler := Middleware(DefaultConfig(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/mcp/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !reached {
		t.Error("expected POST to reach next handler")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}
