# mcpx

**Secure your MCP servers in 5 minutes. One binary. One config file. Zero dependencies.**

[![CI](https://github.com/rohitgs28/mcpx/workflows/CI/badge.svg)](https://github.com/rohitgs28/mcpx/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/rohitgs28/mcpx)](https://goreportcard.com/report/github.com/rohitgs28/mcpx)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Built with Go](https://img.shields.io/badge/built_with-Go-00ADD8.svg)](https://go.dev)

---

mcpx is a lightweight gateway proxy for [Model Context Protocol](https://modelcontextprotocol.io/) servers. It sits between your MCP clients (Claude, Cursor, VS Code, custom agents) and your MCP servers, adding authentication, rate limiting, tool-level access control, and audit logging — without modifying your existing servers.

```
MCP Client (Claude, Cursor, etc.)
      │
      ▼
  ┌────────┐
  │  mcpx  │  auth · rate limit · policy · audit · metrics
  └────────┘
      │
  ┌───┴────────┐
  ▼            ▼
Server A    Server B
(filesystem)  (database)
```

## Why mcpx?

MCP servers are powerful but have **no built-in access control**. Any connected client can call any tool with any arguments. In production, you need auth, rate limiting, policies, and audit trails.

Most MCP gateway solutions require Kubernetes clusters, Docker Desktop, or full API gateway stacks. **mcpx doesn't.** It's a single binary with a single YAML config file.

| | mcpx | Microsoft MCP Gateway | Docker MCP Gateway | Kong AI MCP Proxy |
|---|---|---|---|---|
| **Setup time** | 5 minutes | Hours (K8s required) | Docker Desktop | Kong cluster |
| **Dependencies** | None | Kubernetes, Azure | Docker | Kong, Lua runtime |
| **Config** | One YAML file | CRDs + Helm charts | UI + profiles | kong.yaml + plugins |
| **Binary size** | ~10 MB | Cluster | Docker image | Full gateway |
| **Target users** | Devs & small teams | Enterprise Azure | Docker users | Existing Kong users |
| **Prometheus metrics** | ✅ Built-in | Via adapter | Via Docker | Via plugin |
| **Deep health checks** | ✅ Per-backend | ❌ | ❌ | Via plugin |
| **License** | MIT | MIT | Apache 2.0 | Apache 2.0 |

## Quick Start

```bash
# Install from source
git clone https://github.com/rohitgs28/mcpx
cd mcpx
go build -o mcpx ./cmd/mcpx

# Or install directly
go install github.com/rohitgs28/mcpx/cmd/mcpx@latest

# Run with config
./mcpx -c mcpx.yaml
```

```bash
# Or use Docker
docker build -t mcpx .
docker run -p 8080:8080 -v $(pwd)/mcpx.yaml:/etc/mcpx/mcpx.yaml mcpx
```

The gateway starts on `:8080`. Point your MCP clients to `http://localhost:8080/mcp/{server_name}` instead of directly to your backend servers.

## Configuration

```yaml
listen: ":8080"

servers:
  - name: filesystem
    url: http://localhost:3001
    policy:
      allow_tools:
        - read_file
        - list_directory
      deny_tools:
        - write_file
        - delete_file

  - name: database
    url: http://localhost:3002
    policy:
      read_only: true  # blocks tools/call, allows tools/list

auth:
  enabled: true
  type: bearer
  token: "your-secret-token"

audit:
  enabled: true
  output: stdout

rate_limit:
  enabled: true
  rps: 100
  burst: 20
  per_tool: true
  tool_rps: 10
  tool_burst: 5
```

## Features

### 🔐 Authentication

Bearer token and API key authentication. Requests without valid credentials are rejected before reaching any backend. OAuth 2.1 support is on the [roadmap](ROADMAP.md).

### 🛡️ Tool-Level Access Control

Define allow and deny lists per server. Use `read_only: true` to let clients discover tools without calling them. Deny lists take precedence.

```yaml
policy:
  allow_tools: [read_file, list_directory]
  deny_tools: [write_file, delete_file]
```

### ⏱️ Rate Limiting

Global rate limiting protects all backends. Per-tool rate limiting prevents abuse of expensive operations. Token bucket algorithm with configurable burst.

### 📊 Prometheus Metrics

Built-in `/metrics` endpoint exposes request counts, latencies, tool usage, policy decisions, auth failures, and rate limit hits. Plug into Grafana, Datadog, or any Prometheus-compatible system.

```
mcpx_requests_total{server="filesystem",method="tools/call",status_code="2xx"} 42
mcpx_tool_calls_total{server="filesystem",tool="read_file",decision="allow"} 38
mcpx_request_duration_ms_bucket{server="filesystem",le="50"} 35
mcpx_auth_failures_total 3
mcpx_rate_limit_hits_total 1
```

### 🏥 Deep Health Checks

`/health` probes each backend server and reports individual status, latency, and policy configuration. Returns `degraded` when some backends are down, `unhealthy` when all are down.

```json
{
  "status": "degraded",
  "servers": [
    {"name": "filesystem", "healthy": true, "latency_ms": 2.1},
    {"name": "database", "healthy": false, "error": "unreachable: connection refused"}
  ]
}
```

### 📝 Audit Logging

Every request is logged with server name, method, tool name, client IP, policy decision, and latency. JSON output for your existing log infrastructure.

### 🌐 CORS Support

Browser-based MCP clients can connect through the gateway with configurable CORS headers.

### 🔀 Multi-Server Routing

Register multiple MCP servers behind a single gateway. Clients address them by name: `/mcp/filesystem`, `/mcp/database`, `/mcp/github`.

## API

| Endpoint | Description |
|---|---|
| `POST /mcp/{server}` | Proxy MCP JSON-RPC requests to the named backend |
| `GET /health` | Deep health check with per-backend status |
| `GET /servers` | List all registered backend servers |
| `GET /metrics` | Prometheus metrics |

## Architecture

```
cmd/mcpx/main.go              CLI entrypoint, middleware chain assembly
internal/
├── config/config.go           YAML config loading and validation
├── mcp/message.go             MCP JSON-RPC message types and parsing
├── proxy/proxy.go             Core reverse proxy with request inspection
├── auth/auth.go               Bearer token and API key middleware
├── ratelimit/ratelimit.go     Global and per-tool rate limiting
├── audit/audit.go             Structured audit logging (slog + JSON)
├── policy/policy.go           Tool-level allow/deny policy engine
├── metrics/metrics.go         Prometheus-compatible metrics (zero deps)
├── health/health.go           Deep health checking with backend probes
└── cors/cors.go               CORS middleware for browser clients
```

**Middleware chain:** Auth → Rate Limit → Policy → Audit → Proxy → Backend

Every request is inspected at the MCP protocol level. The gateway parses JSON-RPC messages to extract the method and tool name, then evaluates the policy before forwarding.

## Roadmap

See [ROADMAP.md](ROADMAP.md) for the full plan. Key upcoming work:

- [ ] SSE/WebSocket transport proxying
- [ ] OAuth 2.1 authentication
- [ ] Stdio transport (spawn local MCP servers)
- [ ] OpenTelemetry tracing
- [ ] Hot config reload
- [ ] Web dashboard
- [ ] Plugin system (Go + WASM)
- [ ] Helm chart

## Contributing

Contributions welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for setup instructions.

```bash
go test ./...       # run tests
go vet ./...        # lint
golangci-lint run   # extended lint
```

## License

MIT. See [LICENSE](LICENSE) for details.
