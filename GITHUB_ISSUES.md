# GitHub Issues to Create for mcpx

Copy-paste these into GitHub Issues. Label each as indicated.

---

## Issue 1: Add SSE (Server-Sent Events) transport proxy
**Labels:** `enhancement`, `help wanted`, `transport`

MCP supports SSE as a streaming transport. Currently mcpx only proxies HTTP requests. We need to proxy SSE connections so that streaming MCP servers work through the gateway.

### Requirements
- Proxy SSE connections from client to backend
- Apply auth middleware before establishing SSE connection
- Apply policy middleware to tool calls within the SSE stream
- Audit log SSE connections (connect/disconnect events)
- Rate limiting should apply per-message within the stream

### References
- MCP transport spec: https://modelcontextprotocol.io/specification/2025-03-26/basic/transports
- Go SSE libraries to evaluate

---

## Issue 2: Add WebSocket transport proxy
**Labels:** `enhancement`, `help wanted`, `transport`

MCP supports WebSocket as a bidirectional streaming transport. We need to proxy WebSocket connections with the same middleware chain (auth, policy, rate limiting, audit).

### Requirements
- WebSocket upgrade handling
- Bidirectional message inspection
- Apply policy to tool calls within WebSocket messages
- Connection lifecycle audit logging

---

## Issue 3: Add OAuth 2.1 authentication
**Labels:** `enhancement`, `security`, `help wanted`

Replace or supplement bearer token auth with OAuth 2.1, aligned with the MCP authorization spec.

### Requirements
- JWT validation with configurable JWKS endpoint
- Token introspection support
- Configurable scopes per server
- Backward compatible with existing bearer token auth

### References
- MCP auth spec: https://modelcontextprotocol.io/specification/2025-03-26/basic/authorization

---

## Issue 4: Add `--version` flag
**Labels:** `good first issue`, `hacktoberfest`

Add a `--version` or `-v` flag that prints the mcpx version and exits.

Expected output:
```
$ mcpx --version
mcpx v0.1.0
```

### Implementation
- Add version constant in `cmd/mcpx/main.go`
- Parse `-v` / `--version` flag before loading config
- Print version and `os.Exit(0)`

---

## Issue 5: Add request ID propagation
**Labels:** `enhancement`, `good first issue`, `hacktoberfest`

Generate a unique request ID for each incoming request and pass it through to the backend via the `X-Request-ID` header. Include it in audit logs.

### Requirements
- Generate UUID v4 for each request
- Set `X-Request-ID` header on proxied request
- Include `request_id` field in audit log entries
- If client sends `X-Request-ID`, use that instead of generating one

---

## Issue 6: Add config file validation with helpful errors
**Labels:** `good first issue`, `hacktoberfest`

When mcpx.yaml has errors (missing required fields, invalid URLs, etc.), the error messages should be clear and actionable.

### Examples of good error messages
```
Error: server "filesystem" has no url configured (line 5 in mcpx.yaml)
Error: auth.type must be "bearer", "api_key", or "none" (got "oauth")
Error: rate_limit.rps must be positive (got -1)
```

---

## Issue 7: Add example configs for popular MCP servers
**Labels:** `documentation`, `good first issue`, `hacktoberfest`

Create an `examples/` directory with ready-to-use mcpx.yaml configs for popular MCP server setups:

- `examples/claude-desktop.yaml` — Securing Claude Desktop MCP servers
- `examples/filesystem-readonly.yaml` — Read-only filesystem access
- `examples/multi-server.yaml` — Multiple servers behind one gateway
- `examples/ollama.yaml` — Securing Ollama-based MCP servers

---

## Issue 8: Add hot config reload (SIGHUP)
**Labels:** `enhancement`, `help wanted`

Reload mcpx.yaml when the process receives SIGHUP, without dropping existing connections.

### Requirements
- Watch for SIGHUP signal
- Re-parse and validate config
- Apply new server list, policies, and rate limits
- Log config reload events
- Don't drop active connections during reload

---

## Issue 9: Add Prometheus metrics endpoint
**Labels:** `enhancement`, `observability`

**NOTE: Implementation ready — see `internal/metrics/metrics.go`**

Expose Prometheus-compatible metrics at `/metrics`. Track:
- `mcpx_requests_total` — by server, method, status
- `mcpx_tool_calls_total` — by server, tool, decision
- `mcpx_request_duration_ms` — histogram by server
- `mcpx_auth_failures_total`
- `mcpx_rate_limit_hits_total`
- `mcpx_active_connections`
- `mcpx_servers_registered`

---

## Issue 10: Add Dockerfile for multi-arch builds
**Labels:** `enhancement`, `good first issue`

Update the Dockerfile to support multi-architecture builds (amd64 + arm64) using Docker buildx.

```dockerfile
FROM --platform=$BUILDPLATFORM golang:1.22 AS builder
ARG TARGETARCH
RUN GOARCH=$TARGETARCH go build -o mcpx ./cmd/mcpx
```

This enables `docker buildx build --platform linux/amd64,linux/arm64`.
