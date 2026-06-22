# mcpx

**Secure your MCP servers in 5 minutes. One binary. One config file. No infrastructure to run.**

[![CI](https://github.com/rohitgs28/mcpx/workflows/CI/badge.svg)](https://github.com/rohitgs28/mcpx/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/rohitgs28/mcpx)](https://goreportcard.com/report/github.com/rohitgs28/mcpx)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Built with Go](https://img.shields.io/badge/built_with-Go-00ADD8.svg)](https://go.dev)

---

mcpx is a lightweight gateway proxy for [Model Context Protocol](https://modelcontextprotocol.io/) servers. It sits between your MCP clients (Claude, Cursor, VS Code, custom agents) and your MCP servers, adding authentication, rate limiting, tool-level access control, and audit logging - without modifying your existing servers.

```
MCP Client (Claude, Cursor, etc.)
      │
      ▼
  ┌────────┐
  │  mcpx  │  auth · rate limit · policy · audit · metrics · tool integrity
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
| **Dependencies** | Single static binary | Kubernetes, Azure | Docker | Kong, Lua runtime |
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

# Hot-reload the config file on every change
./mcpx -c mcpx.yaml -watch
```

Send `SIGHUP` to force a reload at any time (`kill -HUP $(pidof mcpx)`). Reloads rebuild the handler chain atomically: in-flight requests finish against the old config, new ones see the new config. Invalid configs are rejected and the previous config keeps running.

```bash
# Or use Docker
docker build -t mcpx .
docker run -p 8080:8080 -v $(pwd)/mcpx.yaml:/etc/mcpx/mcpx.yaml mcpx
```

The gateway starts on `:8080`. Point your MCP clients to `http://localhost:8080/mcp/{server_name}` instead of directly to your backend servers.

## Try it in 60 seconds

The repo ships a tiny mock MCP server so you can see the gateway work with no setup.

```bash
# One command (Docker):
docker compose up --build

# …or run locally in two terminals:
go run ./examples/mock-mcp                  # terminal 1 - mock server on :3001
go run ./cmd/mcpx -c examples/demo.yaml     # terminal 2 - gateway on :8080
```

Then list the tools through the gateway:

```bash
curl -s localhost:8080/mcp/demo \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' | jq '.result.tools[].name'
# "echo"
# "read_file"          ← dangerous_delete is filtered out by policy

curl -s localhost:8080/mcp/demo \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"dangerous_delete"}}'
# {"jsonrpc":"2.0",...,"error":{"code":-32600,"message":"tool \"dangerous_delete\" is denied ..."}}  (HTTP 403)
```

The mock advertises `echo`, `read_file`, and `dangerous_delete`; the demo policy denies the last one, so it's hidden from `tools/list` and blocked on call.

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

  - name: github            # local stdio server, spawned by the gateway
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    env:
      GITHUB_PERSONAL_ACCESS_TOKEN: "ghp_..."

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

inspection:
  tool_integrity: enforce   # off | warn | enforce - pin tool schemas, block mutation
  filter_tools_list: true   # hide policy-denied tools from tools/list responses
```

## Features

### 🔐 Authentication

Bearer token, API key, and **OAuth 2.1** authentication. Requests without valid credentials are rejected before reaching any backend.

In `oauth` mode mcpx acts as an OAuth 2.1 protected resource server aligned with the [MCP authorization spec](https://modelcontextprotocol.io/specification/draft/basic/authorization): it verifies JWT (RS256) signatures against the authorization server's JWKS and enforces **audience binding** (RFC 8707) - a token is rejected unless its `aud` claim names this gateway, which is the spec's defense against token-passthrough and confused-deputy attacks. It also publishes [RFC 9728](https://datatracker.ietf.org/doc/html/rfc9728) Protected Resource Metadata at `/.well-known/oauth-protected-resource` and advertises it via `WWW-Authenticate` on `401` responses.

```yaml
auth:
  enabled: true
  type: oauth
  oauth:
    resource: "https://gateway.example.com/mcp"   # expected token audience
    jwks_uri: "https://auth.example.com/.well-known/jwks.json"
    issuer: "https://auth.example.com"             # optional
    authorization_servers:
      - "https://auth.example.com"
```

### 🛡️ Tool-Level Access Control

Define allow and deny lists per server. Use `read_only: true` to let clients discover tools without calling them. Deny lists take precedence.

```yaml
policy:
  allow_tools: [read_file, list_directory]
  deny_tools: [write_file, delete_file]
```

With `inspection.filter_tools_list: true`, denied tools are also stripped from `tools/list` responses, so the model never even sees a tool it cannot call. Filtering is per client when auth provides an identity.

### 🎯 Argument-Level Rules

Constrain *what* a tool may be called with, not just whether it may be called. Constraints (`equals`, `one_of`, `prefix`, `suffix`, `regex`, `required`) are ANDed per argument and evaluated deterministically - violations return a `403` naming the violated rule.

```yaml
policy:
  tool_rules:
    read_file:
      args:
        path: { prefix: "/data/" }
    query:
      args:
        sql: { regex: "^SELECT ", required: true }
```

Scalar values compare as strings (bools and numbers normalized); a rule that targets a non-scalar value fails closed. `prefix` is a literal check - no path canonicalization.

### 👥 Per-Client Policies

Give each caller an identity and a policy. Bearer/API-key auth accepts multiple named credentials; OAuth derives the identity from a JWT claim (`identity_claim`, default `sub`). The effective decision is the **intersection** of the server policy and the client's override - both must allow a call, so overrides can only restrict, never widen, the baseline.

```yaml
auth:
  enabled: true
  type: bearer
  clients:
    - { name: ci-bot,  token: "ci-secret" }
    - { name: analyst, token: "analyst-secret" }

clients:
  ci-bot:
    servers:
      filesystem:
        allow_tools: [read_file]
```

Each client sees its own filtered `tools/list`, audit entries carry the client name, and the tool-call metric gains a `client` label.

### 📡 Streaming (SSE) Pass-Through

MCP Streamable HTTP responses (`Content-Type: text/event-stream`) stream through the gateway unbuffered - events reach the client as the backend emits them, and streams may outlive the server's write timeout. Request-side auth, policy, and rate limiting apply as usual. To keep the `tools/list` security guarantee, when inspection is enabled the gateway pins `tools/list` requests to JSON responses (rewrites `Accept`), so integrity pinning and list filtering cannot be bypassed by a streaming response.

### 🖥️ Stdio Transport (Local MCP Servers)

Most real-world MCP servers are local processes spoken to over stdin/stdout (`npx ...`, `uvx ...`). With `transport: stdio` the gateway spawns the server itself and bridges HTTP JSON-RPC onto it - so auth, policies, rate limiting, audit logging, tool integrity pinning, and list filtering all apply to local servers exactly as they do to HTTP backends.

```yaml
servers:
  - name: filesystem
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/data"]
    env:                    # extra environment for the child
      LOG_LEVEL: info
    workdir: /data          # optional working directory
    request_timeout: 30s    # per-request reply deadline (default 30s)
    policy:
      allow_tools: [read_file, list_directory]
```

One shared child process serves all gateway clients: it is spawned lazily on first request, respawned (with backoff) if it crashes, and retired when its config entry changes or disappears. Request IDs are rewritten internally so concurrent clients never collide, and the MCP `initialize` handshake is cached - later clients get the cached result, and the handshake is replayed automatically after a respawn so existing sessions keep working. Children survive config hot-reloads that leave their entry untouched, and a graceful gateway shutdown closes their stdin (killing only those that linger).

Notifications return `202 Accepted` (mirroring the Streamable HTTP spec). Server-initiated messages (sampling, log notifications) are dropped with a log line - there is no single addressable client on a shared gateway.

### ⚡ Circuit Breaker

After a configurable number of consecutive backend failures (transport errors or `5xx`), the gateway fails fast with `503` instead of queueing requests against a dead upstream. After a cooldown, limited probe requests test recovery. State survives config hot-reloads.

```yaml
circuit_breaker:
  enabled: true
  failure_threshold: 5
  cooldown: 30s
  half_open_max: 1
```

### 🔎 Tool Integrity Pinning

mcpx hashes the **full schema** of every tool (name, description, and input schema) the first time a backend advertises it, then flags any later change. This deterministically detects **rug-pull tool mutation** ([CVE-2025-54136](https://nvd.nist.gov/vuln/detail/CVE-2025-54136)), cross-server shadowing, and full-schema poisoning - attacks where a server silently rewrites a tool the client already approved.

```yaml
inspection:
  tool_integrity: enforce   # off | warn | enforce
```

- **`warn`** - log a violation to the audit trail, pass the tool through.
- **`enforce`** - log it and drop the mutated tool from `tools/list` so clients can't invoke it.

The baseline lives in memory and survives config hot-reloads. Hashing is canonical (key-order independent), so legitimate re-serialization doesn't trigger false positives.

> **Scope note:** this defends *static* tool-schema integrity, which is deterministically checkable. It does **not** attempt regex/LLM scanning of tool descriptions or runtime tool *output* for prompt injection - those are bypassable and better handled by least-privilege scoping, so mcpx deliberately doesn't ship security theater there.

### ⏱️ Rate Limiting

Global rate limiting protects all backends. Per-tool rate limiting prevents abuse of expensive operations. Token bucket algorithm with configurable burst.

### 📊 Prometheus Metrics

Built-in `/metrics` endpoint exposes request counts, latencies, tool usage, policy decisions, auth failures, and rate limit hits. Plug into Grafana, Datadog, or any Prometheus-compatible system.

```
mcpx_requests_total{server="filesystem",method="tools/call",status_code="2xx"} 42
mcpx_tool_calls_total{server="filesystem",tool="read_file",decision="allow",client="ci-bot"} 38
mcpx_request_duration_ms_bucket{server="filesystem",le="50"} 35
mcpx_auth_failures_total 3
mcpx_rate_limit_hits_total 1
mcpx_breaker_trips_total 1
mcpx_breaker_state{server="database"} 1
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

Every request is logged with server name, method, tool name, authenticated client, client IP, policy decision, and latency. JSON output for your existing log infrastructure. Every response carries an `X-Request-ID` for correlation, and all gateway-generated errors share a consistent JSON envelope (`{"error":{"code":...,"message":...}}`, or a JSON-RPC error when the request carried a parseable message).

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
| `GET /.well-known/oauth-protected-resource` | RFC 9728 metadata (only when `auth.type: oauth`) |

## Architecture

```
cmd/mcpx/main.go              CLI entrypoint, middleware chain assembly
internal/
├── config/config.go           YAML config loading and validation
├── mcp/message.go             MCP JSON-RPC message types and parsing
├── proxy/proxy.go             Core reverse proxy with request inspection
├── auth/auth.go               Bearer, API key, and OAuth 2.1 middleware
├── auth/oauth.go              JWT/JWKS validation + RFC 9728 metadata
├── ratelimit/ratelimit.go     Global and per-tool rate limiting
├── audit/audit.go             Structured audit logging (slog + JSON)
├── policy/policy.go           Tool/argument/client policy engine
├── integrity/integrity.go     Full-schema tool pinning (rug-pull detection)
├── breaker/breaker.go         Per-backend circuit breaker
├── httperr/httperr.go         Consistent JSON error envelopes
├── middleware/requestid.go    X-Request-ID propagation
├── metrics/metrics.go         Prometheus-compatible metrics (no deps)
├── health/health.go           Deep health checking with backend probes
└── cors/cors.go               CORS middleware for browser clients
```

**Middleware chain:** CORS → Metrics → Auth → Rate Limit → Gateway (Policy → Audit → Proxy)

Every request is inspected at the MCP protocol level. The gateway parses JSON-RPC messages to extract the method and tool name, evaluates the policy before forwarding, and inspects `tools/list` responses for schema integrity and policy filtering on the way back.

## Security

mcpx is explicit about what it defends and what it doesn't. See **[SECURITY.md](SECURITY.md)** for the full threat model - which MCP attacks (rug-pulls, token passthrough, confused deputy, schema poisoning) it mitigates and how, plus the deliberate non-goals (it does **not** do bypassable regex/LLM prompt-injection scanning). Report vulnerabilities via [GitHub Security Advisories](https://github.com/rohitgs28/mcpx/security/advisories/new).

## Roadmap

See [ROADMAP.md](ROADMAP.md) for the full plan. Key upcoming work:

- [x] OAuth 2.1 authentication (audience validation + RFC 9728 metadata)
- [x] Full-schema tool integrity pinning (rug-pull detection)
- [x] Hot config reload (SIGHUP + watch)
- [x] SSE / Streamable HTTP pass-through
- [x] Argument-level and per-client policies
- [x] Per-backend circuit breaker
- [x] Stdio transport (spawn local MCP servers)
- [ ] WebSocket transport proxying
- [ ] OpenTelemetry tracing
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
