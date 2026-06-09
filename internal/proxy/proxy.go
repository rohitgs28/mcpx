// Package proxy implements the core MCP reverse proxy.
package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rohitgs28/mcpx/internal/audit"
	"github.com/rohitgs28/mcpx/internal/config"
	"github.com/rohitgs28/mcpx/internal/httperr"
	"github.com/rohitgs28/mcpx/internal/integrity"
	"github.com/rohitgs28/mcpx/internal/mcp"
	"github.com/rohitgs28/mcpx/internal/metrics"
	"github.com/rohitgs28/mcpx/internal/policy"
)

type Gateway struct {
	servers map[string]*Backend
	policy  *policy.Engine
	audit   *audit.Logger
	tools   *integrity.Store
	metrics *metrics.Collector
	filter  bool // remove policy-denied tools from tools/list responses
	inspect bool // true when integrity or filtering needs response interception
	mux     *http.ServeMux
}

type Backend struct {
	Name   string
	URL    string
	Proxy  *httputil.ReverseProxy
	Config config.ServerConfig
}

// ctxKey is the private type for request-scoped values the gateway threads
// through the reverse proxy into ModifyResponse.
type ctxKey int

const ctxKeyReqInfo ctxKey = iota

// reqInfo carries the parsed MCP routing details from handleMCP into the
// ModifyResponse hook (which only receives the outbound *http.Response).
type reqInfo struct {
	server string
	method string
}

// New builds a gateway. The policy engine and audit logger enforce/record
// per-request decisions; the integrity store (may be nil) pins tool schemas.
// Tool-list filtering is enabled via cfg.Inspection.FilterToolsList.
func New(cfg *config.Config, pe *policy.Engine, al *audit.Logger, ts *integrity.Store, mc *metrics.Collector) (*Gateway, error) {
	g := &Gateway{
		servers: make(map[string]*Backend),
		policy:  pe,
		audit:   al,
		tools:   ts,
		metrics: mc,
		mux:     http.NewServeMux(),
	}
	if cfg.Inspection != nil {
		g.filter = cfg.Inspection.FilterToolsList
	}
	g.inspect = g.filter || (ts != nil && ts.Enabled())

	for _, sc := range cfg.Servers {
		if sc.Transport == "http" || sc.Transport == "sse" {
			target, err := url.Parse(sc.URL)
			if err != nil {
				return nil, fmt.Errorf("parsing URL for server %q: %w", sc.Name, err)
			}
			p := httputil.NewSingleHostReverseProxy(target)
			// The stdlib flushes text/event-stream bodies immediately; the
			// interval covers other streaming content types (e.g. ndjson).
			p.FlushInterval = 100 * time.Millisecond
			p.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
				httperr.Write(w, http.StatusBadGateway, httperr.CodeUpstreamError, "upstream error: "+err.Error())
			}
			if g.inspect {
				// Force identity encoding so ModifyResponse sees plain JSON,
				// then inspect tools/list responses. tools/list is also pinned
				// to a JSON response: a Streamable HTTP backend may otherwise
				// answer over SSE, which would bypass integrity pinning and
				// list filtering.
				orig := p.Director
				p.Director = func(req *http.Request) {
					orig(req)
					req.Header.Del("Accept-Encoding")
					if info, _ := req.Context().Value(ctxKeyReqInfo).(*reqInfo); info != nil && info.method == mcp.MethodToolsList {
						req.Header.Set("Accept", "application/json")
					}
				}
				p.ModifyResponse = g.modifyResponse
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
		type info struct {
			Name, URL, Transport string
			ReadOnly             bool
		}
		var s []info
		for _, b := range g.servers {
			readOnly := b.Config.Policy != nil && b.Config.Policy.ReadOnly
			s = append(s, info{b.Name, b.URL, b.Config.Transport, readOnly})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s)
	})
	return g, nil
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) { g.mux.ServeHTTP(w, r) }

// recordToolCall records a tools/call policy decision. It is a no-op for
// non-tool requests (empty tool) and when no metrics collector is configured.
func (g *Gateway) recordToolCall(server, tool, decision string) {
	if g.metrics == nil || tool == "" {
		return
	}
	g.metrics.RecordToolCall(server, tool, decision)
}

func (g *Gateway) handleMCP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	path := strings.TrimPrefix(r.URL.Path, "/mcp/")
	parts := strings.SplitN(path, "/", 2)
	sn := parts[0]
	b, ok := g.servers[sn]
	if !ok {
		httperr.Write(w, http.StatusNotFound, httperr.CodeUnknownServer, "unknown server: "+sn)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		httperr.Write(w, http.StatusBadRequest, httperr.CodeBadRequest, "failed to read request body")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	mreq, _ := mcp.ParseRequest(body)
	entry := audit.Entry{Timestamp: time.Now().UTC(), Server: sn, ClientIP: r.RemoteAddr}
	if mreq != nil {
		entry.Method = mreq.Method
		if tc, _ := mcp.ParseToolCall(mreq); tc != nil {
			entry.Tool = tc.Name
		}
		result := g.policy.Evaluate(sn, mreq)
		if !result.Allowed {
			entry.Allowed = false
			entry.Reason = result.Reason
			entry.StatusCode = http.StatusForbidden
			entry.DurationMs = time.Since(start).Milliseconds()
			g.audit.Log(entry)
			g.recordToolCall(sn, entry.Tool, "deny")
			httperr.WriteRPC(w, http.StatusForbidden, mreq.ID, -32600, result.Reason)
			return
		}
		g.recordToolCall(sn, entry.Tool, "allow")
		// Thread routing details into ModifyResponse for tools/list inspection.
		if g.inspect {
			r = r.WithContext(context.WithValue(r.Context(), ctxKeyReqInfo, &reqInfo{server: sn, method: mreq.Method}))
		}
	}
	entry.Allowed = true
	r.URL.Path = "/"
	if len(parts) > 1 {
		r.URL.Path = "/" + parts[1]
	}
	// Clients that accept SSE may receive a long-lived stream, which must
	// outlive the server's global WriteTimeout.
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	}
	b.Proxy.ServeHTTP(w, r)
	entry.DurationMs = time.Since(start).Milliseconds()
	entry.StatusCode = http.StatusOK
	g.audit.Log(entry)
}

// modifyResponse inspects tools/list responses, applying schema-integrity
// pinning and policy-based tool filtering before the body reaches the client.
// All other responses pass through untouched.
func (g *Gateway) modifyResponse(resp *http.Response) error {
	// Streaming responses (MCP Streamable HTTP / SSE) pass through untouched:
	// reading the body here would buffer the stream and stall the client.
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return nil
	}
	info, _ := resp.Request.Context().Value(ctxKeyReqInfo).(*reqInfo)
	if info == nil || info.method != mcp.MethodToolsList || resp.StatusCode != http.StatusOK {
		return nil
	}
	// We forced identity encoding upstream; if a server compressed anyway,
	// leave the body alone rather than risk corrupting it.
	if resp.Header.Get("Content-Encoding") != "" {
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return err
	}
	newBody, changed := g.processToolsList(info.server, body)
	if !changed {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return nil
	}
	resp.Body = io.NopCloser(bytes.NewReader(newBody))
	resp.ContentLength = int64(len(newBody))
	resp.Header.Set("Content-Length", strconv.Itoa(len(newBody)))
	return nil
}

// processToolsList returns a (possibly rewritten) tools/list response body and
// whether it changed. It drops tools whose schema mutated (enforce mode) and
// tools the policy forbids (when filtering is enabled), logging integrity
// violations to the audit trail.
func (g *Gateway) processToolsList(server string, body []byte) ([]byte, bool) {
	var rpc mcp.Response
	if err := json.Unmarshal(body, &rpc); err != nil || rpc.Error != nil || len(rpc.Result) == 0 {
		return body, false
	}
	tl, err := mcp.ParseToolsListResult(rpc.Result)
	if err != nil {
		return body, false
	}

	// Schema-integrity pinning: detect mutated tools.
	drop := make(map[string]bool)
	if g.tools != nil && g.tools.Enabled() {
		enforce := g.tools.Mode() == integrity.ModeEnforce
		for _, v := range g.tools.Check(server, tl.Tools) {
			reason := "tool schema changed since pinned baseline (possible rug-pull/mutation)"
			g.audit.Log(audit.Entry{
				Timestamp: time.Now().UTC(),
				Server:    server,
				Method:    mcp.MethodToolsList,
				Tool:      v.Tool,
				Allowed:   !enforce,
				Reason:    reason,
			})
			if enforce {
				drop[v.Tool] = true
			}
		}
	}

	changed := false
	kept := make([]json.RawMessage, 0, len(tl.Tools))
	for _, raw := range tl.Tools {
		name := mcp.ToolName(raw)
		if drop[name] {
			changed = true
			continue
		}
		if g.filter && !g.policy.ToolAllowed(server, name) {
			changed = true
			continue
		}
		kept = append(kept, raw)
	}
	if !changed {
		return body, false
	}

	tl.Tools = kept
	newResult, err := json.Marshal(tl)
	if err != nil {
		return body, false
	}
	rpc.Result = newResult
	newBody, err := json.Marshal(rpc)
	if err != nil {
		return body, false
	}
	return newBody, true
}
