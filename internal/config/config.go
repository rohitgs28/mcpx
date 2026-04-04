// Package config handles loading and validating mcpx gateway configuration.
//
// Configuration is loaded from a YAML file specified with the -c flag.
// All sections are optional with sensible defaults.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the top-level gateway configuration.
type Config struct {
	Listen    string          `yaml:"listen"`
	Servers   []ServerConfig  `yaml:"servers"`
	Auth      *AuthConfig     `yaml:"auth"`
	Audit     *AuditConfig    `yaml:"audit"`
	RateLimit *RateLimitConfig `yaml:"rate_limit"`
	CORS      *CORSConfig     `yaml:"cors"`
}

// ServerConfig defines a backend MCP server.
type ServerConfig struct {
	Name      string        `yaml:"name"`
	URL       string        `yaml:"url"`
	Transport string        `yaml:"transport"` // "http", "sse", "websocket"
	Policy    *PolicyConfig `yaml:"policy"`
}

// PolicyConfig defines tool-level access control for a server.
type PolicyConfig struct {
	ReadOnly   bool     `yaml:"read_only"`
	AllowTools []string `yaml:"allow_tools"`
	DenyTools  []string `yaml:"deny_tools"`
}

// AuthConfig defines authentication settings.
type AuthConfig struct {
	Enabled bool   `yaml:"enabled"`
	Type    string `yaml:"type"`  // "bearer", "api_key", "none"
	Token   string `yaml:"token"` // for bearer auth
	Header  string `yaml:"header"` // for api_key auth (default: X-API-Key)
}

// AuditConfig defines audit logging settings.
type AuditConfig struct {
	Enabled bool   `yaml:"enabled"`
	Output  string `yaml:"output"` // "stdout", "file", "json"
	Path    string `yaml:"path"`   // file path when output is "file"
}

// RateLimitConfig defines rate limiting settings.
type RateLimitConfig struct {
	Enabled  bool    `yaml:"enabled"`
	RPS      float64 `yaml:"rps"`
	Burst    int     `yaml:"burst"`
	PerTool  bool    `yaml:"per_tool"`
	ToolRPS  float64 `yaml:"tool_rps"`
	ToolBurst int    `yaml:"tool_burst"`
}

// CORSConfig defines CORS settings for browser-based MCP clients.
type CORSConfig struct {
	Enabled        bool     `yaml:"enabled"`
	AllowedOrigins []string `yaml:"allowed_origins"`
	AllowedMethods []string `yaml:"allowed_methods"`
	AllowedHeaders []string `yaml:"allowed_headers"`
	MaxAge         string   `yaml:"max_age"`
}

// Load reads and validates configuration from a YAML file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	cfg := &Config{
		Listen: ":8080", // default
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return cfg, nil
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	if c.Listen == "" {
		return fmt.Errorf("listen address is required")
	}

	for i, srv := range c.Servers {
		if srv.Name == "" {
			return fmt.Errorf("server %d: name is required", i)
		}
		if srv.URL == "" {
			return fmt.Errorf("server %q: url is required", srv.Name)
		}
		if srv.Transport == "" {
			c.Servers[i].Transport = "http"
		}
	}

	if c.Auth != nil && c.Auth.Enabled {
		switch c.Auth.Type {
		case "bearer", "api_key", "none", "":
			// valid
		default:
			return fmt.Errorf("auth.type must be \"bearer\", \"api_key\", or \"none\" (got %q)", c.Auth.Type)
		}
		if c.Auth.Type == "bearer" && c.Auth.Token == "" {
			return fmt.Errorf("auth.token is required when auth.type is \"bearer\"")
		}
	}

	if c.RateLimit != nil && c.RateLimit.Enabled {
		if c.RateLimit.RPS <= 0 {
			return fmt.Errorf("rate_limit.rps must be positive (got %.1f)", c.RateLimit.RPS)
		}
		if c.RateLimit.Burst <= 0 {
			return fmt.Errorf("rate_limit.burst must be positive (got %d)", c.RateLimit.Burst)
		}
	}

	return nil
}
