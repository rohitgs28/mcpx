package httperr

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestWrite(t *testing.T) {
	tests := []struct {
		name   string
		status int
		code   string
		msg    string
	}{
		{"unauthorized", 401, CodeUnauthorized, "invalid token"},
		{"rate limited", 429, CodeRateLimited, "rate limit exceeded"},
		{"unknown server", 404, CodeUnknownServer, "unknown server: foo"},
		{"upstream", 502, CodeUpstreamError, "connection refused"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			Write(rec, tt.status, tt.code, tt.msg)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Fatalf("Content-Type = %q, want application/json", ct)
			}
			var got envelope
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("body is not valid JSON: %v", err)
			}
			if got.Error.Code != tt.code || got.Error.Message != tt.msg {
				t.Fatalf("body = %+v, want code=%q msg=%q", got, tt.code, tt.msg)
			}
		})
	}
}

func TestWriteRPC(t *testing.T) {
	tests := []struct {
		name string
		id   any
	}{
		{"string id", "abc"},
		{"number id", float64(7)},
		{"nil id", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			WriteRPC(rec, 403, tt.id, -32600, "denied")
			if rec.Code != 403 {
				t.Fatalf("status = %d, want 403", rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Fatalf("Content-Type = %q, want application/json", ct)
			}
			var got struct {
				JSONRPC string `json:"jsonrpc"`
				ID      any    `json:"id"`
				Error   struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("body is not valid JSON: %v", err)
			}
			if got.JSONRPC != "2.0" || got.Error.Code != -32600 || got.Error.Message != "denied" {
				t.Fatalf("unexpected body: %+v", got)
			}
			switch want := tt.id.(type) {
			case nil:
				if got.ID != nil {
					t.Fatalf("id = %v, want nil", got.ID)
				}
			default:
				if got.ID != want {
					t.Fatalf("id = %v, want %v", got.ID, want)
				}
			}
		})
	}
}
