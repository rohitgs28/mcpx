# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-06-21

First public release. mcpx is a lightweight gateway proxy that sits between MCP
clients and MCP servers, adding security and observability without modifying the
backends. Single static binary, single YAML config.

### Added

- Reverse proxy for MCP JSON-RPC over HTTP, with multi-server routing (`/mcp/{server}`).
- Authentication: bearer token, API key, and OAuth 2.1 (JWT/JWKS validation,
  audience binding per RFC 8707, RFC 9728 protected-resource metadata).
- Tool-level access control: per-server allow/deny lists and `read_only` mode.
- Argument-level rules (`equals`, `one_of`, `prefix`, `suffix`, `regex`, `required`).
- Per-client policies with identity from auth, enforced as the intersection of
  server and client policy.
- Tool-integrity pinning: full-schema hashing to detect rug-pull tool mutation
  (CVE-2025-54136), with `off` / `warn` / `enforce` modes.
- `tools/list` filtering so clients never see tools they cannot call.
- Stdio transport: spawn and bridge local MCP servers (`npx`, `uvx`, ...).
- Streamable HTTP (SSE) pass-through.
- Per-backend circuit breaker.
- Global and per-tool rate limiting (token bucket).
- Prometheus metrics at `/metrics`.
- Deep health checks at `/health` with per-backend status.
- Structured JSON audit logging with `X-Request-ID` correlation.
- CORS support and hot config reload (SIGHUP + `-watch`).

[0.1.0]: https://github.com/rohitgs28/mcpx/releases/tag/v0.1.0
