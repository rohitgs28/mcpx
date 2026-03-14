// Package proxy implements the core MCP reverse proxy.
package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/rohitgs28/mcpx/internal/audit"
	"github.com/rohitgs28/mcpx/internal/config"
	"github.com/rohitgs28/mcpx/internal/mcp"
	"github.com/rohitgs28/mcpx/internal/policy"
)

type Gateway struct {
	servers map[string]*Backend
	policy  *policy.Engine
	audit   *audit.Logger
	mux     *http.ServeMux
}

type Backend struct {
	Name   string
	URL    string
	Proxy  *httputil.ReverseProxy
	Config config.ServerConfig
}

func New(cfg *config.Config, pe *policy.Engine, al *audit.Logger) (*Gateway, error) {
	g := &Gateway{servers: make(map[string]*Backend), policy: pe, audit: al, mux: http.NewServeMux()}
	for _, sc := range cfg.Servers {
		if sc.Transport == "http" {
			target, err := url.Parse(sc.URL)
			if err != nil { return nil, fmt.Errorf("parsing URL for server %q: %w", sc.Name, err) }
			p := httputil.NewSingleHostReverseProxy(target)
			p.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
				http.Error(w, fmt.Sprintf(`{"error":"upstream error: %s"}`, err.Error()), http.StatusBadGateway)
			}
			g.servers[sc.Name] = &Backend{Name: sc.Name, URL: sc.URL, Proxy: p, Config: sc}
		}
	}
	g.mux.HandleFunc("/mcp/", g.handleMCP)
	g.mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status": "ok", "servers": len(g.servers)})
	})
	g.mux.HandleFunc("/servers", func(w http.ResponseWriter, r *http.Request) {
		type info struct { Name, URL, Transport string; ReadOnly bool }
		var s []info
		for _, b := range g.servers { s = append(s, info{b.Name, b.URL, b.Config.Transport, b.Config.Policy.ReadOnly}) }
		w.Header().Set("Content-Type", "application/json"); json.NewEncoder(w).Encode(s)
	})
	return g, nil
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) { g.mux.ServeHTTP(w, r) }

func (g *Gateway) handleMCP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	path := strings.TrimPrefix(r.URL.Path, "/mcp/")
	parts := strings.SplitN(path, "/", 2)
	sn := parts[0]
	b, ok := g.servers[sn]
	if !ok { http.Error(w, fmt.Sprintf(`{"error":"unknown server: %s"}`, sn), http.StatusNotFound); return }
	body, err := io.ReadAll(r.Body)
	if err != nil { http.Error(w, `{"error":"failed to read request body"}`, http.StatusBadRequest); return }
	r.Body = io.NopCloser(bytes.NewReader(body))
	mreq, _ := mcp.ParseRequest(body)
	entry := audit.Entry{Timestamp: time.Now().UTC(), Server: sn, ClientIP: r.RemoteAddr}
	if mreq != nil {
		entry.Method = mreq.Method
		if tc, _ := mcp.ParseToolCall(mreq); tc != nil { entry.Tool = tc.Name }
		result := g.policy.Evaluate(sn, mreq)
		if !result.Allowed {
			entry.Allowed = false; entry.Reason = result.Reason; entry.StatusCode = http.StatusForbidden
			entry.DurationMs = time.Since(start).Milliseconds(); g.audit.Log(entry)
			resp := mcp.NewErrorResponse(mreq.ID, -32600, result.Reason)
			w.Header().Set("Content-Type", "application/json"); w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(resp); return
		}
	}
	entry.Allowed = true
	r.URL.Path = "/"; if len(parts) > 1 { r.URL.Path = "/" + parts[1] }
	b.Proxy.ServeHTTP(w, r)
	entry.DurationMs = time.Since(start).Milliseconds(); entry.StatusCode = http.StatusOK
	g.audit.Log(entry)
}
