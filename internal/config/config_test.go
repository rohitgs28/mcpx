package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadValidConfig(t *testing.T) {
	yaml := `
listen: ":9090"
servers:
  - name: test
    url: http://localhost:3001
    policy:
      allow_tools:
        - read_file
auth:
  enabled: true
  type: bearer
  token: "test-token"
rate_limit:
  enabled: true
  rps: 50
  burst: 10
`
	cfg := loadFromString(t, yaml)

	if cfg.Listen != ":9090" {
		t.Errorf("expected :9090, got %s", cfg.Listen)
	}
	if len(cfg.Servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(cfg.Servers))
	}
	if cfg.Servers[0].Name != "test" {
		t.Errorf("expected server name 'test', got %s", cfg.Servers[0].Name)
	}
	if cfg.Servers[0].Transport != "http" {
		t.Errorf("expected default transport 'http', got %s", cfg.Servers[0].Transport)
	}
}

func TestDefaultListen(t *testing.T) {
	yaml := `
servers:
  - name: test
    url: http://localhost:3001
`
	cfg := loadFromString(t, yaml)

	if cfg.Listen != ":8080" {
		t.Errorf("expected default :8080, got %s", cfg.Listen)
	}
}

func TestValidateServerNameRequired(t *testing.T) {
	yaml := `
servers:
  - url: http://localhost:3001
`
	_, err := loadFromStringErr(yaml)
	if err == nil {
		t.Error("expected error for missing server name")
	}
}

func TestValidateServerURLRequired(t *testing.T) {
	yaml := `
servers:
  - name: test
`
	_, err := loadFromStringErr(yaml)
	if err == nil {
		t.Error("expected error for missing server URL")
	}
}

func TestValidateTransport(t *testing.T) {
	tests := []struct {
		transport string
		wantErr   bool
	}{
		{"", false},
		{"http", false},
		{"sse", false},
		{"websocket", true},
		{"stdio", true},
	}
	for _, tt := range tests {
		yaml := `
servers:
  - name: test
    url: http://localhost:3001
    transport: "` + tt.transport + `"
`
		_, err := loadFromStringErr(yaml)
		if (err != nil) != tt.wantErr {
			t.Errorf("transport %q: err = %v, wantErr %v", tt.transport, err, tt.wantErr)
		}
	}
}

func TestValidateToolRules(t *testing.T) {
	valid := `
servers:
  - name: fs
    url: http://localhost:3001
    policy:
      tool_rules:
        read_file:
          args:
            path: { prefix: "/data/" }
            tag: { regex: "^[a-z]+$", required: true }
`
	cfg, err := loadFromStringErr(valid)
	if err != nil {
		t.Fatalf("valid tool_rules rejected: %v", err)
	}
	ar := cfg.Servers[0].Policy.ToolRules["read_file"].Args["path"]
	if ar.Prefix != "/data/" {
		t.Errorf("prefix = %q, want /data/", ar.Prefix)
	}

	badRegex := `
servers:
  - name: fs
    url: http://localhost:3001
    policy:
      tool_rules:
        read_file:
          args:
            path: { regex: "([" }
`
	if _, err := loadFromStringErr(badRegex); err == nil {
		t.Error("expected error for invalid regex in tool_rules")
	}

	emptyRule := `
servers:
  - name: fs
    url: http://localhost:3001
    policy:
      tool_rules:
        read_file:
          args:
            path: {}
`
	if _, err := loadFromStringErr(emptyRule); err == nil {
		t.Error("expected error for arg rule with no constraints")
	}

	typoKey := `
servers:
  - name: fs
    url: http://localhost:3001
    policy:
      tool_rules:
        read_file:
          args:
            path: { prefx: "/data/" }
`
	if _, err := loadFromStringErr(typoKey); err == nil {
		t.Error("expected misspelled constraint key to be rejected (rule is empty)")
	}
}

func TestValidateAuthType(t *testing.T) {
	yaml := `
servers:
  - name: test
    url: http://localhost:3001
auth:
  enabled: true
  type: "invalid"
`
	_, err := loadFromStringErr(yaml)
	if err == nil {
		t.Error("expected error for invalid auth type")
	}
}

func TestValidateBearerTokenRequired(t *testing.T) {
	yaml := `
servers:
  - name: test
    url: http://localhost:3001
auth:
  enabled: true
  type: bearer
`
	_, err := loadFromStringErr(yaml)
	if err == nil {
		t.Error("expected error for missing bearer token")
	}
}

func TestValidateAuthClients(t *testing.T) {
	clientsOnly := `
servers:
  - name: test
    url: http://localhost:3001
auth:
  enabled: true
  type: bearer
  clients:
    - { name: ci-bot, token: tok-ci }
    - { name: analyst, token: tok-an }
`
	cfg, err := loadFromStringErr(clientsOnly)
	if err != nil {
		t.Fatalf("bearer with clients (no token) should be valid: %v", err)
	}
	if len(cfg.Auth.Clients) != 2 || cfg.Auth.Clients[0].Name != "ci-bot" {
		t.Errorf("clients not parsed: %+v", cfg.Auth.Clients)
	}

	dup := `
servers:
  - name: test
    url: http://localhost:3001
auth:
  enabled: true
  type: bearer
  clients:
    - { name: ci-bot, token: a }
    - { name: ci-bot, token: b }
`
	if _, err := loadFromStringErr(dup); err == nil {
		t.Error("expected duplicate client names to be rejected")
	}

	missingToken := `
servers:
  - name: test
    url: http://localhost:3001
auth:
  enabled: true
  type: api_key
  clients:
    - { name: ci-bot }
`
	if _, err := loadFromStringErr(missingToken); err == nil {
		t.Error("expected credential without token to be rejected")
	}
}

func TestValidateClientPolicies(t *testing.T) {
	valid := `
servers:
  - name: fs
    url: http://localhost:3001
clients:
  ci-bot:
    servers:
      fs:
        allow_tools: [read_file]
        tool_rules:
          read_file:
            args:
              path: { prefix: "/ci/" }
`
	cfg, err := loadFromStringErr(valid)
	if err != nil {
		t.Fatalf("valid clients section rejected: %v", err)
	}
	if cfg.Clients["ci-bot"].Servers["fs"].AllowTools[0] != "read_file" {
		t.Errorf("client policy not parsed: %+v", cfg.Clients)
	}

	badRule := `
servers:
  - name: fs
    url: http://localhost:3001
clients:
  ci-bot:
    servers:
      fs:
        tool_rules:
          read_file:
            args:
              path: { regex: "([" }
`
	if _, err := loadFromStringErr(badRule); err == nil {
		t.Error("expected invalid regex in client policy to be rejected")
	}
}

func TestValidateCircuitBreaker(t *testing.T) {
	valid := `
servers:
  - name: test
    url: http://localhost:3001
circuit_breaker:
  enabled: true
  failure_threshold: 5
  cooldown: 30s
  half_open_max: 1
`
	cfg, err := loadFromStringErr(valid)
	if err != nil {
		t.Fatalf("valid circuit_breaker rejected: %v", err)
	}
	if d := time.Duration(cfg.CircuitBreaker.Cooldown); d != 30*time.Second {
		t.Errorf("cooldown = %v, want 30s", d)
	}

	badDuration := `
servers:
  - name: test
    url: http://localhost:3001
circuit_breaker:
  enabled: true
  cooldown: thirty
`
	if _, err := loadFromStringErr(badDuration); err == nil {
		t.Error("expected invalid duration string to be rejected")
	}

	negative := `
servers:
  - name: test
    url: http://localhost:3001
circuit_breaker:
  enabled: true
  failure_threshold: -1
`
	if _, err := loadFromStringErr(negative); err == nil {
		t.Error("expected negative failure_threshold to be rejected")
	}
}

func TestValidateRateLimitRPS(t *testing.T) {
	yaml := `
servers:
  - name: test
    url: http://localhost:3001
rate_limit:
  enabled: true
  rps: -1
  burst: 10
`
	_, err := loadFromStringErr(yaml)
	if err == nil {
		t.Error("expected error for negative RPS")
	}
}

func TestCORSConfig(t *testing.T) {
	yaml := `
servers:
  - name: test
    url: http://localhost:3001
cors:
  enabled: true
  allowed_origins:
    - "http://localhost:3000"
    - "https://app.example.com"
  max_age: "3600"
`
	cfg := loadFromString(t, yaml)

	if cfg.CORS == nil {
		t.Fatal("expected CORS config")
	}
	if !cfg.CORS.Enabled {
		t.Error("expected CORS enabled")
	}
	if len(cfg.CORS.AllowedOrigins) != 2 {
		t.Errorf("expected 2 origins, got %d", len(cfg.CORS.AllowedOrigins))
	}
}

func TestPolicyConfig(t *testing.T) {
	yaml := `
servers:
  - name: fs
    url: http://localhost:3001
    policy:
      allow_tools: [read_file, list_directory]
      deny_tools: [delete_file]
  - name: db
    url: http://localhost:3002
    policy:
      read_only: true
`
	cfg := loadFromString(t, yaml)

	if cfg.Servers[0].Policy == nil {
		t.Fatal("expected policy on first server")
	}
	if len(cfg.Servers[0].Policy.AllowTools) != 2 {
		t.Errorf("expected 2 allow_tools, got %d", len(cfg.Servers[0].Policy.AllowTools))
	}
	if !cfg.Servers[1].Policy.ReadOnly {
		t.Error("expected read_only on second server")
	}
}

// --- Helpers ---

func loadFromString(t *testing.T, yaml string) *Config {
	t.Helper()
	cfg, err := loadFromStringErr(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return cfg
}

func loadFromStringErr(yaml string) (*Config, error) {
	dir, err := os.MkdirTemp("", "mcpx-test")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, "mcpx.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		return nil, err
	}
	return Load(path)
}
