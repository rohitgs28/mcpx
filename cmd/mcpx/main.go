// mcpx is a lightweight gateway proxy for the Model Context Protocol.
// It sits between MCP clients and servers, providing auth, rate limiting,
// tool-level access control, and audit logging.
//
// Usage:
//
//	mcpx -c mcpx.yaml
//	mcpx --config /etc/mcpx/config.yaml
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
	"github.com/rohitgs28/mcpx/internal/policy"
	"github.com/rohitgs28/mcpx/internal/proxy"
	"github.com/rohitgs28/mcpx/internal/ratelimit"
)

var (
	version = "dev"
	commit  = "none"
)

func main() {
	configPath := flag.String("c", "mcpx.yaml", "path to config file")
	flag.StringVar(configPath, "config", "mcpx.yaml", "path to config file")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("mcpx %s (%s)\n", version, commit)
		os.Exit(0)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	cfg, err := config.Load(*configPath)
	if err != nil { slog.Error("failed to load config", "error", err); os.Exit(1) }
	slog.Info("config loaded", "servers", len(cfg.Servers), "listen", cfg.Listen)

	pe := policy.New(cfg.Servers)
	al, err := audit.New(cfg.Audit)
	if err != nil { slog.Error("failed to initialize audit logger", "error", err); os.Exit(1) }
	rl := ratelimit.New(cfg.Limits)

	gw, err := proxy.New(cfg, pe, al)
	if err != nil { slog.Error("failed to create gateway", "error", err); os.Exit(1) }

	var h http.Handler = gw
	h = rl.Middleware()(h)
	h = auth.Middleware(cfg.Auth)(h)

	srv := &http.Server{Addr: cfg.Listen, Handler: h, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 120 * time.Second}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		slog.Info("mcpx gateway started", "addr", cfg.Listen, "version", version)
		for _, s := range cfg.Servers { slog.Info("registered server", "name", s.Name, "url", s.URL) }
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed { slog.Error("server error", "error", err); os.Exit(1) }
	}()

	<-done; slog.Info("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second); defer cancel()
	if err := srv.Shutdown(ctx); err != nil { slog.Error("shutdown error", "error", err) }
	slog.Info("mcpx stopped")
}
