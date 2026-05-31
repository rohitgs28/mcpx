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
	Listen     string            `yaml:"listen"`
	Servers    []ServerConfig    `yaml:"servers"`
	Auth       *AuthConfig       `yaml:"auth"`
	Audit      *AuditConfig      `yaml:"audit"`
	RateLimit  *RateLimitConfig  `yaml:"rate_limit"`
	CORS       *CORSConfig       `yaml:"cors"`
	Inspection *InspectionConfig `yaml:"inspection"`
}

// InspectionConfig controls response-level inspection of tools/list payloads.
type InspectionConfig struct {
	// ToolIntegrity pins tool schemas and reacts to mutation:
	// "off" (default), "warn" (log only), or "enforce" (drop changed tools).
	ToolIntegrity string `yaml:"tool_integrity"`
	// FilterToolsList removes policy-denied tools from tools/list responses
	// so clients never see tools they are not permitted to call.
	FilterToolsList bool `yaml:"filter_tools_list"`
}

// ServerConfig defines a backend MCP server.
type ServerConfig struct {
	Name      string  `yaml:"name"`
	URL       string  `yaml:"url"`
	Transport string  `yaml:"transport"` // "http", "sse", "websocket"
	Policy    *Policy `yaml:"policy"`
}

// Policy defines tool-level access control for a server.
type Policy struct {
	ReadOnly   bool     `yaml:"read_only"`
	AllowTools []string `yaml:"allow_tools"`
	DenyTools  []string `yaml:"deny_tools"`
}

// AuthConfig defines authentication settings.
type AuthConfig struct {
	Enabled bool         `yaml:"enabled"`
	Type    string       `yaml:"type"`   // "bearer", "api_key", "none", "oauth"
	Token   string       `yaml:"token"`  // for bearer auth
	Header  string       `yaml:"header"` // for api_key auth (default: X-API-Key)
	OAuth   *OAuthConfig `yaml:"oauth"`  // for oauth (OAuth 2.1 resource server)
}

// OAuthConfig configures the gateway as an OAuth 2.1 protected resource server,
// per the MCP authorization spec. Inbound JWT access tokens are validated for
// signature, expiry, and — critically — audience (RFC 8707): a token MUST list
// this resource in its aud claim or it is rejected. Tokens are never forwarded
// upstream.
type OAuthConfig struct {
	// Resource is this gateway's canonical resource identifier (an https URL),
	// the value an access token's aud claim MUST contain.
	Resource string `yaml:"resource"`
	// AuthorizationServers lists trusted issuer URLs advertised in the
	// RFC 9728 Protected Resource Metadata document.
	AuthorizationServers []string `yaml:"authorization_servers"`
	// JWKSURI is the authorization server's JSON Web Key Set endpoint used to
	// verify token signatures (RS256).
	JWKSURI string `yaml:"jwks_uri"`
	// Issuer, when set, is validated against the token's iss claim.
	Issuer string `yaml:"issuer"`
}

// AuditConfig defines audit logging settings.
type AuditConfig struct {
	Enabled bool   `yaml:"enabled"`
	Output  string `yaml:"output"` // "stdout", "file", "json"
	Path    string `yaml:"path"`   // file path when output is "file"
}

// RateLimitConfig defines rate limiting settings.
type RateLimitConfig struct {
	Enabled   bool    `yaml:"enabled"`
	RPS       float64 `yaml:"rps"`
	Burst     int     `yaml:"burst"`
	PerTool   bool    `yaml:"per_tool"`
	ToolRPS   float64 `yaml:"tool_rps"`
	ToolBurst int     `yaml:"tool_burst"`
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
		case "bearer", "api_key", "none", "oauth", "":
			// valid
		default:
			return fmt.Errorf("auth.type must be \"bearer\", \"api_key\", \"oauth\", or \"none\" (got %q)", c.Auth.Type)
		}
		if c.Auth.Type == "bearer" && c.Auth.Token == "" {
			return fmt.Errorf("auth.token is required when auth.type is \"bearer\"")
		}
		if c.Auth.Type == "oauth" {
			if c.Auth.OAuth == nil {
				return fmt.Errorf("auth.oauth is required when auth.type is \"oauth\"")
			}
			if c.Auth.OAuth.Resource == "" {
				return fmt.Errorf("auth.oauth.resource is required (the audience tokens must be bound to)")
			}
			if c.Auth.OAuth.JWKSURI == "" {
				return fmt.Errorf("auth.oauth.jwks_uri is required to verify token signatures")
			}
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

	if c.Inspection != nil {
		switch c.Inspection.ToolIntegrity {
		case "", "off", "warn", "enforce":
			// valid
		default:
			return fmt.Errorf("inspection.tool_integrity must be \"off\", \"warn\", or \"enforce\" (got %q)", c.Inspection.ToolIntegrity)
		}
	}

	return nil
}
