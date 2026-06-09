// Package config handles loading and validating mcpx gateway configuration.
//
// Configuration is loaded from a YAML file specified with the -c flag.
// All sections are optional with sensible defaults.
package config

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

// Config is the top-level gateway configuration.
type Config struct {
	Listen     string                  `yaml:"listen"`
	Servers    []ServerConfig          `yaml:"servers"`
	Auth       *AuthConfig             `yaml:"auth"`
	Audit      *AuditConfig            `yaml:"audit"`
	RateLimit  *RateLimitConfig        `yaml:"rate_limit"`
	CORS       *CORSConfig             `yaml:"cors"`
	Inspection *InspectionConfig       `yaml:"inspection"`
	Clients    map[string]ClientConfig `yaml:"clients"` // per-client policy overrides, keyed by client name
}

// ClientConfig narrows what an authenticated client may do, per server.
// The effective decision is the intersection of the server policy and the
// client's override: BOTH must allow a call. Client names come from auth
// (named credentials for bearer/api_key, the identity claim for OAuth);
// a client with no entry here gets the server baseline policy.
type ClientConfig struct {
	Servers map[string]*Policy `yaml:"servers"`
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
	Transport string  `yaml:"transport"` // "http" (default) or its alias "sse"; SSE streams are auto-detected per response
	Policy    *Policy `yaml:"policy"`
}

// Policy defines tool-level access control for a server.
type Policy struct {
	ReadOnly   bool                `yaml:"read_only"`
	AllowTools []string            `yaml:"allow_tools"`
	DenyTools  []string            `yaml:"deny_tools"`
	ToolRules  map[string]ToolRule `yaml:"tool_rules"` // per-tool argument constraints
}

// ToolRule constrains the arguments a tool may be called with.
type ToolRule struct {
	Args map[string]ArgRule `yaml:"args"`
}

// ArgRule is a deterministic constraint on a single top-level tool argument.
// Multiple constraints on the same argument are ANDed. Rules only constrain
// the arguments they name: an absent argument passes unless Required is set.
// Scalar values are compared as strings (bools as "true"/"false", numbers in
// their shortest decimal form); a rule that targets a non-scalar value
// (object, array, null) denies the call — what cannot be compared cannot be
// allowed. Prefix is a literal string check: no path canonicalization is
// performed.
type ArgRule struct {
	Equals   *string  `yaml:"equals"` // pointer so "" is distinguishable from unset
	OneOf    []string `yaml:"one_of"`
	Prefix   string   `yaml:"prefix"`
	Suffix   string   `yaml:"suffix"`
	Regex    string   `yaml:"regex"`
	Required bool     `yaml:"required"`
}

// AuthConfig defines authentication settings.
type AuthConfig struct {
	Enabled bool         `yaml:"enabled"`
	Type    string       `yaml:"type"`   // "bearer", "api_key", "none", "oauth"
	Token   string       `yaml:"token"`  // single shared token for bearer/api_key (identifies as client "default")
	Header  string       `yaml:"header"` // for api_key auth (default: X-API-Key)
	OAuth   *OAuthConfig `yaml:"oauth"`  // for oauth (OAuth 2.1 resource server)
	// Clients lists named credentials for bearer/api_key auth so requests
	// carry a client identity (used by per-client policies and audit logs).
	// May be combined with Token, which matches as client "default".
	Clients []ClientCredential `yaml:"clients"`
	// IdentityClaim is the JWT claim used as the client identity for oauth
	// (default "sub").
	IdentityClaim string `yaml:"identity_claim"`
}

// ClientCredential is a named bearer/api_key token.
type ClientCredential struct {
	Name  string `yaml:"name"`
	Token string `yaml:"token"`
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
		switch srv.Transport {
		case "":
			c.Servers[i].Transport = "http"
		case "http", "sse":
			// valid; "sse" is an alias of "http" — streaming responses are
			// detected per-response via Content-Type, not per-server.
		default:
			return fmt.Errorf("server %q: transport must be \"http\" or \"sse\" (got %q)", srv.Name, srv.Transport)
		}
		if err := validatePolicy(srv.Policy, fmt.Sprintf("server %q", srv.Name)); err != nil {
			return err
		}
	}

	if c.Auth != nil && c.Auth.Enabled {
		switch c.Auth.Type {
		case "bearer", "api_key", "none", "oauth", "":
			// valid
		default:
			return fmt.Errorf("auth.type must be \"bearer\", \"api_key\", \"oauth\", or \"none\" (got %q)", c.Auth.Type)
		}
		if (c.Auth.Type == "bearer" || c.Auth.Type == "api_key") && c.Auth.Token == "" && len(c.Auth.Clients) == 0 {
			return fmt.Errorf("auth.token or auth.clients is required when auth.type is %q", c.Auth.Type)
		}
		seen := make(map[string]bool, len(c.Auth.Clients))
		for i, cc := range c.Auth.Clients {
			if cc.Name == "" {
				return fmt.Errorf("auth.clients[%d]: name is required", i)
			}
			if cc.Token == "" {
				return fmt.Errorf("auth.clients[%d] (%q): token is required", i, cc.Name)
			}
			if seen[cc.Name] {
				return fmt.Errorf("auth.clients: duplicate client name %q", cc.Name)
			}
			seen[cc.Name] = true
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

	// Client policy names are deliberately not checked against auth.clients:
	// OAuth identities (JWT subjects) are not enumerable in config.
	for name, cc := range c.Clients {
		if name == "" {
			return fmt.Errorf("clients: client name must not be empty")
		}
		for srv, p := range cc.Servers {
			if err := validatePolicy(p, fmt.Sprintf("clients.%s.servers.%s", name, srv)); err != nil {
				return err
			}
		}
	}

	return nil
}

// validatePolicy checks a policy's tool_rules: every argument rule must carry
// at least one constraint (which also catches misspelled constraint keys,
// since those leave the rule empty) and regexes must compile.
func validatePolicy(p *Policy, where string) error {
	if p == nil {
		return nil
	}
	for tool, tr := range p.ToolRules {
		for arg, ar := range tr.Args {
			if ar.Equals == nil && len(ar.OneOf) == 0 && ar.Prefix == "" && ar.Suffix == "" && ar.Regex == "" && !ar.Required {
				return fmt.Errorf("%s: tool_rules.%s.args.%s: at least one constraint (equals, one_of, prefix, suffix, regex, required) is required", where, tool, arg)
			}
			if ar.Regex != "" {
				if _, err := regexp.Compile(ar.Regex); err != nil {
					return fmt.Errorf("%s: tool_rules.%s.args.%s: invalid regex: %w", where, tool, arg, err)
				}
			}
		}
	}
	return nil
}
