// Package config handles loading and validating the mcpx gateway configuration.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Listen  string          `yaml:"listen"`
	Servers []ServerConfig  `yaml:"servers"`
	Auth    AuthConfig      `yaml:"auth"`
	Audit   AuditConfig     `yaml:"audit"`
	Limits  RateLimitConfig `yaml:"rate_limit"`
}

type ServerConfig struct {
	Name      string   `yaml:"name"`
	URL       string   `yaml:"url"`
	Transport string   `yaml:"transport"`
	Command   string   `yaml:"command"`
	Args      []string `yaml:"args"`
	Policy    Policy   `yaml:"policy"`
}

type Policy struct {
	AllowTools []string `yaml:"allow_tools"`
	DenyTools  []string `yaml:"deny_tools"`
	ReadOnly   bool     `yaml:"read_only"`
}

type AuthConfig struct {
	Enabled bool   `yaml:"enabled"`
	Type    string `yaml:"type"`
	Token   string `yaml:"token"`
	Header  string `yaml:"header"`
}

type AuditConfig struct {
	Enabled bool   `yaml:"enabled"`
	Output  string `yaml:"output"`
	Path    string `yaml:"path"`
}

type RateLimitConfig struct {
	Enabled    bool    `yaml:"enabled"`
	RPS        float64 `yaml:"rps"`
	Burst      int     `yaml:"burst"`
	PerTool    bool    `yaml:"per_tool"`
	ToolRPS    float64 `yaml:"tool_rps"`
	ToolBurst  int     `yaml:"tool_burst"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}
	cfg := &Config{Listen: ":8080"}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if err := cfg.validate(); err != nil { return nil, err }
	return cfg, nil
}

func (c *Config) validate() error {
	if len(c.Servers) == 0 {
		return fmt.Errorf("config: at least one server must be defined")
	}
	for i, s := range c.Servers {
		if s.Name == "" { return fmt.Errorf("config: server[%d] must have a name", i) }
		if s.URL == "" && s.Command == "" { return fmt.Errorf("config: server %q must have a url or command", s.Name) }
		if s.Transport == "" {
			if s.URL != "" { c.Servers[i].Transport = "http" } else { c.Servers[i].Transport = "stdio" }
		}
	}
	return nil
}
