package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rohitgs28/mcpx/internal/config"
)

func TestLoad_ValidConfig(t *testing.T) {
	yaml := `
listen: ":9090"
servers:
  - name: test
    url: http://localhost:3000
    transport: http
auth:
  enabled: false
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Listen != ":9090" {
		t.Fatalf("expected :9090, got %s", cfg.Listen)
	}
	if len(cfg.Servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(cfg.Servers))
	}
	if cfg.Servers[0].Name != "test" {
		t.Fatalf("expected server name 'test', got %s", cfg.Servers[0].Name)
	}
}

func TestLoad_DefaultListenAddress(t *testing.T) {
	yaml := `
servers:
  - name: test
    url: http://localhost:3000
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Listen != ":8080" {
		t.Fatalf("expected default :8080, got %s", cfg.Listen)
	}
}

func TestLoad_DefaultTransportInference(t *testing.T) {
	yaml := `
servers:
  - name: api
    url: http://localhost:3000
  - name: local
    command: /usr/bin/mcp-server
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Servers[0].Transport != "http" {
		t.Fatalf("expected http transport for URL server, got %s", cfg.Servers[0].Transport)
	}
	if cfg.Servers[1].Transport != "stdio" {
		t.Fatalf("expected stdio transport for command server, got %s", cfg.Servers[1].Transport)
	}
}

func TestLoad_ValidationNoServers(t *testing.T) {
	yaml := `listen: ":8080"`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("expected error for config with no servers")
	}
}

func TestLoad_ValidationServerWithoutName(t *testing.T) {
	yaml := `
servers:
  - url: http://localhost:3000
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("expected error for server without name")
	}
}

func TestLoad_ValidationServerWithoutURLOrCommand(t *testing.T) {
	yaml := `
servers:
  - name: broken
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("expected error for server without url or command")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := config.Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte("{{invalid yaml content"), 0644)

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoad_FullConfig(t *testing.T) {
	yaml := `
listen: ":9090"
servers:
  - name: fs
    url: http://localhost:3001
    transport: http
    policy:
      allow_tools: ["read_file", "list_directory"]
      deny_tools: ["delete_file"]
      read_only: false
auth:
  enabled: true
  type: bearer
  token: test-token
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
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(yaml), 0644)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Auth.Enabled {
		t.Fatal("expected auth enabled")
	}
	if cfg.Auth.Type != "bearer" {
		t.Fatalf("expected bearer auth, got %s", cfg.Auth.Type)
	}
	if !cfg.Limits.Enabled {
		t.Fatal("expected rate limiting enabled")
	}
	if cfg.Limits.RPS != 100 {
		t.Fatalf("expected RPS 100, got %f", cfg.Limits.RPS)
	}
	if !cfg.Limits.PerTool {
		t.Fatal("expected per-tool rate limiting")
	}
	if len(cfg.Servers[0].Policy.AllowTools) != 2 {
		t.Fatalf("expected 2 allow tools, got %d", len(cfg.Servers[0].Policy.AllowTools))
	}
}
