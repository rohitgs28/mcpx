// mcpx - Lightweight MCP gateway proxy
//
// Sits between MCP clients and MCP servers, adding auth, rate limiting,
// tool-level access control, audit logging, and Prometheus metrics.
//
// Usage:
//
//	mcpx -c mcpx.yaml
//	mcpx --version
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rohitgs28/mcpx/internal/audit"
	"github.com/rohitgs28/mcpx/internal/auth"
	"github.com/rohitgs28/mcpx/internal/config"
	"github.com/rohitgs28/mcpx/internal/cors"
	"github.com/rohitgs28/mcpx/internal/health"
	"github.com/rohitgs28/mcpx/internal/metrics"
	"github.com/rohitgs28/mcpx/internal/policy"
	"github.com/rohitgs28/mcpx/internal/proxy"
	"github.com/rohitgs28/mcpx/internal/ratelimit"
)

const version = "0.1.0"

func main() {
	// CLI flags
	configPath := flag.String("c", "mcpx.yaml", "path to config file")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.BoolVar(showVersion, "v", false, "print version and exit (shorthand)")
	flag.Parse()

	if *showVersion {
		fmt.Printf("mcpx v%s\n", version)
		os.Exit(0)
	}

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "path", *configPath, "error", err)
		os.Exit(1)
	}

	// Initialize metrics collector
	mc := metrics.New()
	mc.SetServersRegistered(int64(len(cfg.Servers)))

	// Build health checker with server info
	healthServers := make([]health.ServerInfo, 0, len(cfg.Servers))
	for _, srv := range cfg.Servers {
		hs := health.ServerInfo{
			Name: srv.Name,
			URL:  srv.URL,
		}
		if srv.Policy != nil {
			hs.Policy = &health.PolicySummary{
				ReadOnly:   srv.Policy.ReadOnly,
				AllowTools: srv.Policy.AllowTools,
				DenyTools:  srv.Policy.DenyTools,
			}
		}
		healthServers = append(healthServers, hs)
	}
	checker := health.NewChecker(healthServers, version)

	// Build the proxy handler
	proxyHandler := proxy.New(cfg, mc)

	// Assemble middleware chain: CORS -> Auth -> Rate Limit -> Policy -> Audit -> Proxy
	var handler http.Handler = proxyHandler

	// Audit logging (innermost wrapping, closest to proxy)
	if cfg.Audit != nil && cfg.Audit.Enabled {
		handler = audit.Middleware(cfg.Audit, handler)
	}

	// Policy enforcement
	handler = policy.Middleware(cfg, handler)

	// Rate limiting
	if cfg.RateLimit != nil && cfg.RateLimit.Enabled {
		handler = ratelimit.Middleware(cfg.RateLimit, mc, handler)
	}

	// Authentication
	if cfg.Auth != nil && cfg.Auth.Enabled {
		handler = auth.Middleware(cfg.Auth, mc, handler)
	}

	// Metrics tracking (wraps everything to capture full request lifecycle)
	handler = metricsMiddleware(mc, handler)

	// CORS (outermost, so preflight is handled before auth)
	if cfg.CORS != nil && cfg.CORS.Enabled {
		corsCfg := cors.Config{
			AllowedOrigins: cfg.CORS.AllowedOrigins,
			AllowedMethods: cfg.CORS.AllowedMethods,
			AllowedHeaders: cfg.CORS.AllowedHeaders,
			MaxAge:         cfg.CORS.MaxAge,
		}
		handler = cors.Middleware(corsCfg, handler)
	} else {
		// Default CORS for convenience
		handler = cors.Middleware(cors.DefaultConfig(), handler)
	}

	// Set up routes
	mux := http.NewServeMux()

	// MCP proxy routes
	mux.Handle("/mcp/", handler)

	// Health check — deep health with backend probing
	mux.HandleFunc("/health", checker.Handler())

	// Server listing
	mux.HandleFunc("/servers", func(w http.ResponseWriter, r *http.Request) {
		serversHandler(w, r, cfg)
	})

	// Prometheus metrics
	mux.HandleFunc("/metrics", mc.Handler())

	// Build HTTP server
	srv := &http.Server{
		Addr:         cfg.Listen,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		slog.Info("mcpx gateway starting",
			"version", version,
			"listen", cfg.Listen,
			"servers", len(cfg.Servers),
			"auth", cfg.Auth != nil && cfg.Auth.Enabled,
			"rate_limit", cfg.RateLimit != nil && cfg.RateLimit.Enabled,
			"audit", cfg.Audit != nil && cfg.Audit.Enabled,
		)

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	<-done
	slog.Info("shutting down gracefully...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("shutdown error", "error", err)
	}

	slog.Info("mcpx stopped")
}

// metricsMiddleware wraps a handler to track request count, latency, and active connections.
func metricsMiddleware(mc *metrics.Collector, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mc.IncrActiveConnections()
		defer mc.DecrActiveConnections()

		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(rw, r)

		// Extract server name from path: /mcp/{server}
		server := extractServerName(r.URL.Path)
		method := r.Header.Get("X-MCP-Method") // set by proxy after parsing
		if method == "" {
			method = r.Method
		}

		mc.RecordRequest(server, method, rw.statusCode, time.Since(start))
	})
}

// responseWriter captures the status code for metrics.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// extractServerName extracts the server name from /mcp/{server}/...
func extractServerName(path string) string {
	// path: /mcp/filesystem or /mcp/filesystem/extra
	if len(path) < 5 {
		return "unknown"
	}
	rest := path[5:] // strip "/mcp/"
	for i, c := range rest {
		if c == '/' {
			return rest[:i]
		}
	}
	return rest
}

// serversHandler returns the list of registered backend servers.
func serversHandler(w http.ResponseWriter, _ *http.Request, cfg *config.Config) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"servers":[`))
	for i, srv := range cfg.Servers {
		if i > 0 {
			w.Write([]byte(","))
		}
		fmt.Fprintf(w, `{"name":%q,"url":%q}`, srv.Name, srv.URL)
	}
	w.Write([]byte("]}"))
}
