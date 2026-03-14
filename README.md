<p align="center">
  <h1 align="center">mcpx</h1>
  <p align="center">
    <strong>A lightweight gateway proxy for the Model Context Protocol.</strong>
  </p>
  <p align="center">
    <a href="https://github.com/rohitgs28/mcpx/actions"><img src="https://github.com/rohitgs28/mcpx/workflows/CI/badge.svg" alt="CI"></a>
    <a href="https://github.com/rohitgs28/mcpx/blob/main/LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License"></a>
    <a href="https://github.com/rohitgs28/mcpx"><img src="https://img.shields.io/badge/built_with-Go-00ADD8.svg" alt="Built with Go"></a>
  </p>
</p>

---

mcpx sits between MCP clients and MCP servers. It adds authentication, rate limiting, tool-level access control, and audit logging without modifying your existing MCP servers.

```
MCP Client (Claude, Cursor, etc.)
      |
      v
  ┌────────┐
  │  mcpx  │  auth, rate limit, policy, audit
  └────────┘
      |
  ┌───┴────────┐
  v            v
Server A    Server B
(filesystem)  (database)
```

## The Problem

MCP servers are powerful but have no built-in access control. Any connected client can call any tool with any arguments. In production, you need:

- Authentication before clients can reach your servers
- Rate limiting to prevent abuse and manage API costs
- Tool-level policies (allow read_file but block delete_file)
- Audit trails for compliance and debugging
- A single entry point instead of exposing every server directly

mcpx solves all of these with a single config file.

## Quick Start

```bash
# Build from source
git clone https://github.com/rohitgs28/mcpx
cd mcpx
go build -o mcpx ./cmd/mcpx

# Run with example config
./mcpx -c mcpx.yaml
```

```bash
# Or use Docker
docker build -t mcpx .
docker run -p 8080:8080 -v $(pwd)/mcpx.yaml:/etc/mcpx/mcpx.yaml mcpx
```

The gateway starts on `:8080`. MCP clients connect to `http://localhost:8080/mcp/{server_name}` instead of directly to the backend server.

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
      read_only: true  # blocks all tools/call, allows tools/list

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

### Authentication
Supports bearer token and API key authentication. Requests without valid credentials are rejected before reaching any backend server.

### Tool-Level Access Control
Define allow and deny lists per server. Use read-only mode to let clients discover available tools without being able to call them. Deny lists take precedence over allow lists.

### Rate Limiting
Global rate limiting protects all backends. Per-tool rate limiting prevents abuse of expensive or sensitive tools. Uses token bucket algorithm with configurable burst capacity.

### Audit Logging
Every request is logged with server name, method, tool name, client IP, policy decision, and latency. Output to stdout for piping to your existing log infrastructure, or write directly to a file.

### Multi-Server Routing
Register multiple MCP servers behind a single gateway. Clients address servers by name: `/mcp/filesystem`, `/mcp/database`, `/mcp/github`.

## API

| Endpoint | Description |
|----------|-------------|
| `POST /mcp/{server}` | Proxy MCP requests to the named backend |
| `GET /health` | Health check with server count |
| `GET /servers` | List all registered backend servers |

## Architecture

```
cmd/mcpx/main.go         CLI entrypoint, middleware chain assembly
internal/
├── config/config.go      YAML config loading and validation
├── mcp/message.go        MCP JSON-RPC message types and parsing
├── proxy/proxy.go        Core reverse proxy with request inspection
├── auth/auth.go          Bearer token and API key middleware
├── ratelimit/ratelimit.go  Global and per-tool rate limiting
├── audit/audit.go        Structured audit logging (slog + JSON)
└── policy/policy.go      Tool-level allow/deny policy engine
```

The middleware chain is: **Auth -> Rate Limit -> Policy -> Proxy -> Backend**.

Every request is inspected at the MCP protocol level. The gateway parses JSON-RPC messages to extract the method name and tool name, then evaluates the policy before forwarding.

## Roadmap

- [ ] WebSocket/SSE support for streaming MCP transports
- [ ] OAuth 2.1 authentication (aligned with MCP spec)
- [ ] Stdio transport support (spawn and manage local MCP servers)
- [ ] Web dashboard for real-time request monitoring
- [ ] OpenTelemetry tracing integration
- [ ] Plugin system for custom middleware
- [ ] Multi-tenant support with per-client policies
- [ ] Prometheus metrics endpoint

## Contributing

Contributions welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for setup instructions.

```bash
go test ./...       # run tests
go vet ./...        # vet
golangci-lint run   # lint
```

## License

MIT. See [LICENSE](LICENSE) for details.
