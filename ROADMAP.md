# mcpx Roadmap

## Vision

Make mcpx the simplest, fastest way to secure MCP servers in production. One binary, one config file, zero dependencies.

## Design Principles

1. **Zero dependencies at runtime** — single static binary, no sidecars, no databases
2. **One config file** — everything in `mcpx.yaml`, no scattered env vars
3. **Protocol-aware** — the gateway understands MCP, not just HTTP
4. **Secure by default** — auth on, audit on, deny-by-default policies

## Phase 1: Foundation ✅

- [x] HTTP reverse proxy with MCP message inspection
- [x] Bearer token and API key authentication
- [x] Tool-level allow/deny policy engine
- [x] Global and per-tool rate limiting (token bucket)
- [x] Structured audit logging (slog + JSON)
- [x] Multi-server routing (`/mcp/{server}`)
- [x] Health check endpoint (`/health`)
- [x] Server listing endpoint (`/servers`)
- [x] Docker support
- [x] CI pipeline (build + test + lint)

## Phase 2: Observability & Browser Support (Current)

- [ ] Prometheus metrics endpoint (`/metrics`) — request counts, latencies, tool usage, policy decisions
- [ ] Deep health checks — probe each backend, report individual status and latency
- [ ] CORS middleware — support browser-based MCP clients
- [ ] Request ID propagation — trace requests across gateway and backends
- [ ] Structured error responses — consistent JSON error format with error codes

## Phase 3: Transport Support

- [ ] Server-Sent Events (SSE) proxy — streaming MCP transport
- [ ] WebSocket proxy — bidirectional streaming MCP transport
- [ ] Stdio transport — spawn and manage local MCP servers as child processes
- [ ] Transport auto-detection — route to correct transport based on server config

## Phase 4: Enterprise Security

- [ ] OAuth 2.1 authentication (aligned with MCP authorization spec)
- [ ] JWT validation with JWKS endpoint support
- [ ] mTLS between gateway and backends
- [ ] Per-client policies (multi-tenant access control)
- [ ] Argument-level policy rules (e.g., allow `read_file` only for specific paths)
- [ ] Secret injection — inject credentials into backend requests from a vault

## Phase 5: Operational Excellence

- [ ] OpenTelemetry tracing integration
- [ ] Prometheus metrics with Grafana dashboard template
- [x] Hot config reload (SIGHUP or watch mode)
- [ ] Graceful shutdown with connection draining
- [ ] Circuit breaker per backend
- [ ] Request/response caching for idempotent tools
- [ ] Admin API for runtime config changes

## Phase 6: Ecosystem

- [ ] Plugin system — custom middleware in Go or WASM
- [ ] Web dashboard — real-time request monitoring
- [ ] CLI companion — `mcpx status`, `mcpx logs`, `mcpx test`
- [ ] Terraform provider for declarative config
- [ ] Helm chart for Kubernetes deployments
- [ ] Published to Homebrew, apt, and AUR

## How mcpx is Different

| | mcpx | Microsoft MCP Gateway | Docker MCP Gateway | Kong AI MCP Proxy |
|---|---|---|---|---|
| Setup | Single binary + YAML | Kubernetes cluster | Docker Desktop | Kong Gateway + plugin |
| Dependencies | None | K8s, Azure | Docker | Kong, Lua |
| Config | One file | CRDs + Helm | UI + profiles | kong.yaml + plugins |
| Overhead | ~10MB binary | Cluster | Docker daemon | Full API gateway |
| Target | Individual devs, small teams | Enterprise on Azure | Docker users | Existing Kong users |
| License | MIT | MIT | Apache 2.0 | Apache 2.0 |

mcpx is for developers who want to add security to their MCP servers in 5 minutes, not 5 days.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Pick any unchecked item above and open an issue to claim it.
