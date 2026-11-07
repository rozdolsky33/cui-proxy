// Package config loads and validates the cui-proxy YAML configuration.
package config

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

// Rule describes one detection rule. Type is "regex", "dictionary" or "builtin".
type Rule struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`
	Pattern  string `yaml:"pattern"`
	Validate string `yaml:"validate"`
	Synth    string `yaml:"synth"`
	File     string `yaml:"file"`
}

// Policy controls failure handling and limits. OnError is "block" or "passthrough".
type Policy struct {
	OnError      string `yaml:"on_error"`
	MaxBodyBytes int64  `yaml:"max_body_bytes"`
}

// VaultConfig selects the vault backend. Mode is "memory" or "file".
type VaultConfig struct {
	Mode    string `yaml:"mode"`
	KeyFile string `yaml:"key_file"`
	File    string `yaml:"file"`
}

// Config is the top-level configuration.
type Config struct {
	Listen string `yaml:"listen"`
	UI     struct {
		Listen string `yaml:"listen"`
	} `yaml:"ui"`
	AllowNonLoopback bool              `yaml:"allow_non_loopback"`
	Upstreams        map[string]string `yaml:"upstreams"` // adapter name -> base URL
	Policy           Policy            `yaml:"policy"`
	Vault            VaultConfig       `yaml:"vault"`
	Rules            []Rule            `yaml:"rules"`
}

// Load parses the YAML file at path (unknown fields are errors), applies
// defaults and validates the result.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	defer f.Close()
	var c Config
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	c.applyDefaults()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) applyDefaults() {
	if c.Listen == "" {
		c.Listen = "127.0.0.1:8787"
	}
	if c.UI.Listen == "" {
		c.UI.Listen = "127.0.0.1:8788"
	}
	if c.Policy.OnError == "" {
		c.Policy.OnError = "block"
	}
	if c.Policy.MaxBodyBytes == 0 {
		c.Policy.MaxBodyBytes = 20 << 20
	}
	if c.Vault.Mode == "" {
		c.Vault.Mode = "memory"
	}
}

// Validate checks the config. Any non-empty Synth class is accepted; use
// ValidateWith to check classes against the generator registry.
func (c *Config) Validate() error {
	return c.ValidateWith(nil)
}

// ValidateWith is Validate plus, when known is non-nil, a check that every
// rule's Synth class is known.
func (c *Config) ValidateWith(known func(class string) bool) error {
	if c.Listen == "" {
		return errors.New("config: listen is empty")
	}
	if err := c.checkListen("listen", c.Listen); err != nil {
		return err
	}
	if c.UI.Listen != "" {
		if err := c.checkListen("ui.listen", c.UI.Listen); err != nil {
			return err
		}
	}
	if c.Policy.OnError != "block" && c.Policy.OnError != "passthrough" {
		return fmt.Errorf("config: policy.on_error %q must be block or passthrough", c.Policy.OnError)
	}
	if c.Policy.MaxBodyBytes < 0 {
		return errors.New("config: policy.max_body_bytes must not be negative")
	}
	switch c.Vault.Mode {
	case "memory":
	case "file":
		if c.Vault.KeyFile == "" {
			return errors.New("config: vault.mode file requires vault.key_file")
		}
		if c.Vault.File == "" {
			return errors.New("config: vault.mode file requires vault.file")
		}
	default:
		return fmt.Errorf("config: vault.mode %q must be memory or file", c.Vault.Mode)
	}
	seen := make(map[string]bool, len(c.Rules))
	for i, r := range c.Rules {
		if err := validateRule(r, known); err != nil {
			return fmt.Errorf("config: rules[%d]: %w", i, err)
		}
		if seen[r.Name] {
			return fmt.Errorf("config: rules[%d]: duplicate name %q", i, r.Name)
		}
		seen[r.Name] = true
	}
	return nil
}

func validateRule(r Rule, known func(string) bool) error {
	if r.Name == "" {
		return errors.New("name is empty")
	}
	switch r.Type {
	case "regex":
		if r.Pattern == "" {
			return fmt.Errorf("%q: regex rule needs pattern", r.Name)
		}
		if _, err := regexp.Compile(r.Pattern); err != nil {
			return fmt.Errorf("%q: bad pattern: %w", r.Name, err)
		}
	case "dictionary":
		if r.File == "" {
			return fmt.Errorf("%q: dictionary rule needs file", r.Name)
		}
	case "builtin":
		// Expands to the v1 detector set; each detector carries its own class.
		return nil
	default:
		return fmt.Errorf("%q: unknown type %q", r.Name, r.Type)
	}
	if r.Synth == "" {
		return fmt.Errorf("%q: synth is empty", r.Name)
	}
	if known != nil && !known(r.Synth) {
		return fmt.Errorf("%q: unknown synth class %q", r.Name, r.Synth)
	}
	return nil
}

func (c *Config) checkListen(field, addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("config: %s %q: %w", field, addr, err)
	}
	if c.AllowNonLoopback || host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("config: %s %q is not loopback (set allow_non_loopback to override)", field, addr)
}
