// Package config loads and validates the cui-proxy YAML configuration.
package config

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"time"

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
	// Action is what a detection by this rule does: "block" refuses the whole
	// request, "sanitize" masks and restores the value. Empty follows
	// policy.profile. Under the strict profile a rule may not weaken it.
	Action string `yaml:"action"`
}

// Profiles and actions (ADR 0001).
const (
	ProfileSanitize = "sanitize" // detections are masked and restored (default)
	ProfileStrict   = "strict"   // any detection blocks the request

	ActionBlock    = "block"
	ActionSanitize = "sanitize"
)

// Policy controls failure handling and limits. OnError is "block" or
// "passthrough". Profile is "sanitize" (default) or "strict". Unsupported
// says what happens to content the proxy cannot inspect (images, files,
// compressed bodies, unknown routes, over-deep or unparseable bodies); the
// only value is "block", stated so the behavior is a visible, documented
// setting.
type Policy struct {
	OnError      string `yaml:"on_error"`
	MaxBodyBytes int64  `yaml:"max_body_bytes"`
	Profile      string `yaml:"profile"`
	Unsupported  string `yaml:"unsupported"`
	Attachments  string `yaml:"attachments"`
}

// EffectiveAction is what a detection by r does under profile: the rule's own
// action if it has one, else the profile's. It is never weaker than asked: the
// empty profile and "sanitize" are the sanitize profile, and ANY other
// profile string (validation rejects them, but a config may skip it) is
// treated as strict.
func EffectiveAction(profile string, r Rule) string {
	if profile != "" && profile != ProfileSanitize {
		return ActionBlock
	}
	if r.Action == ActionBlock {
		return ActionBlock
	}
	return ActionSanitize
}

// VaultConfig selects the vault backend. Mode is "memory" or "file".
type VaultConfig struct {
	Mode    string `yaml:"mode"`
	KeyFile string `yaml:"key_file"`
	File    string `yaml:"file"`
}

// Audit configures the persisted decision log (ADR 0001, phase 2). Every
// field is restart-only. The pointer fields distinguish "unset" (take the
// default) from an explicit value; use the accessor methods, which also give
// the defaults for a config built in code rather than loaded.
type Audit struct {
	Enabled *bool  `yaml:"enabled"` // default true
	File    string `yaml:"file"`    // default DefaultAuditFile(); relative paths resolve against the config file
	// OnError is what happens when a decision cannot be recorded: "block"
	// refuses the request (default under the strict profile), "warn" forwards
	// it and logs a WARN (default under sanitize; refused under strict).
	OnError       string `yaml:"on_error"`
	MaxSizeMB     *int   `yaml:"max_size_mb"`    // rotate before a record would pass this size; default 50
	RetentionDays *int   `yaml:"retention_days"` // delete rotated files older than this; default 90, 0 keeps them forever

	onErrorDefaulted bool // OnError came from the profile default, not the file
}

// ExplicitOnError is on_error as written in the file ("" when it was left
// to the profile default), so a reload that only changes the profile is not
// reported as an audit.on_error change.
func (a Audit) ExplicitOnError() string {
	if a.onErrorDefaulted {
		return ""
	}
	return a.OnError
}

// Audit defaults and the on_error values.
const (
	DefaultAuditMaxSizeMB     = 50
	DefaultAuditRetentionDays = 90
	maxAuditSizeMB            = 1 << 20 // 1 TiB: keeps the byte count far from overflow
	maxAuditRetentionDays     = int((1<<63 - 1) / (24 * time.Hour))

	AuditBlock = "block"
	AuditWarn  = "warn"
)

// IsEnabled reports whether the decision log is on (the default).
func (a Audit) IsEnabled() bool { return a.Enabled == nil || *a.Enabled }

// MaxBytes is the rotation size in bytes.
func (a Audit) MaxBytes() int64 {
	if a.MaxSizeMB == nil || *a.MaxSizeMB <= 0 {
		return DefaultAuditMaxSizeMB << 20
	}
	return int64(*a.MaxSizeMB) << 20
}

// Retention is the retention period in days; 0 keeps rotated files forever.
func (a Audit) Retention() int {
	if a.RetentionDays == nil || *a.RetentionDays < 0 {
		return DefaultAuditRetentionDays
	}
	return *a.RetentionDays
}

// AuditBlocks reports whether a decision that cannot be recorded must block
// the request. It is never weaker than asked: only an explicit "warn" under a
// profile that is exactly sanitize (or empty) lets the request through; any
// other profile string is treated as strict, as in EffectiveAction.
func (c *Config) AuditBlocks() bool {
	if c.Policy.Profile != "" && c.Policy.Profile != ProfileSanitize {
		return true
	}
	return c.Audit.OnError != AuditWarn
}

// DefaultAuditFile is $XDG_STATE_HOME/cui-proxy/decisions.jsonl, else
// ~/.local/state/cui-proxy/decisions.jsonl (also on macOS, so the docs have
// one path). It is "" when neither is known.
func DefaultAuditFile() string {
	if x := os.Getenv("XDG_STATE_HOME"); x != "" && filepath.IsAbs(x) {
		return filepath.Join(x, "cui-proxy", "decisions.jsonl")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".local", "state", "cui-proxy", "decisions.jsonl")
}

// Observability controls optional workload correlation in the metrics API.
type Observability struct {
	Correlate bool `yaml:"correlate"`
}

// Config is the top-level configuration.
type Config struct {
	Observability Observability `yaml:"observability"`
	Listen        string        `yaml:"listen"`
	UI            struct {
		Listen string `yaml:"listen"`
	} `yaml:"ui"`
	AllowNonLoopback bool              `yaml:"allow_non_loopback"`
	Upstreams        map[string]string `yaml:"upstreams"` // adapter name -> base URL
	Policy           Policy            `yaml:"policy"`
	Vault            VaultConfig       `yaml:"vault"`
	Audit            Audit             `yaml:"audit"`
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
	// A second document must not silently discard rules or policy settings.
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("config: %s must contain a single YAML document", path)
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
	if c.Policy.Profile == "" {
		c.Policy.Profile = ProfileSanitize
	}
	if c.Policy.Attachments == "" {
		c.Policy.Attachments = "block"
	}
	if c.Policy.Unsupported == "" {
		c.Policy.Unsupported = "block"
	}
	if c.Policy.MaxBodyBytes == 0 {
		c.Policy.MaxBodyBytes = 20 << 20
	}
	if c.Vault.Mode == "" {
		c.Vault.Mode = "memory"
	}
	if c.Audit.Enabled == nil {
		on := true
		c.Audit.Enabled = &on
	}
	if c.Audit.File == "" {
		c.Audit.File = DefaultAuditFile()
	}
	if c.Audit.OnError == "" {
		c.Audit.onErrorDefaulted = true
		c.Audit.OnError = AuditWarn
		if c.Policy.Profile == ProfileStrict {
			c.Audit.OnError = AuditBlock
		}
	}
	if c.Audit.MaxSizeMB == nil {
		n := DefaultAuditMaxSizeMB
		c.Audit.MaxSizeMB = &n
	}
	if c.Audit.RetentionDays == nil {
		n := DefaultAuditRetentionDays
		c.Audit.RetentionDays = &n
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
	// Empty means the default, for configs built in code rather than loaded.
	switch c.Policy.Profile {
	case "", ProfileSanitize:
	case ProfileStrict:
	default:
		return fmt.Errorf("config: policy.profile %q must be sanitize or strict", c.Policy.Profile)
	}
	// Passthrough forwards the original request when it cannot be sanitized.
	// A failure can hit before a blocking rule's value is scanned, so any
	// blocking rule (strict, or action: block) makes passthrough unsafe.
	if c.Policy.OnError == "passthrough" {
		if c.Policy.Profile == ProfileStrict {
			return errors.New(`config: policy.profile "strict" cannot be combined with policy.on_error "passthrough": passthrough would forward the original request when it cannot be inspected`)
		}
		for i, r := range c.Rules {
			if r.Action == ActionBlock {
				return fmt.Errorf(`config: rules[%d]: action "block" cannot be combined with policy.on_error "passthrough": passthrough would forward the original request when it cannot be inspected before the rule is checked`, i)
			}
		}
	}
	if c.Policy.Attachments != "" && c.Policy.Attachments != "block" && c.Policy.Attachments != "extract_text" {
		return fmt.Errorf("config: policy.attachments must be block or extract_text")
	}
	if c.Policy.Unsupported != "" && c.Policy.Unsupported != "block" {
		return fmt.Errorf("config: policy.unsupported %q must be block (content the proxy cannot inspect is always refused)", c.Policy.Unsupported)
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
	if err := c.validateAudit(); err != nil {
		return err
	}
	if c.Policy.Profile == ProfileStrict && len(c.Rules) == 0 {
		return errors.New(`config: policy.profile "strict" needs at least one rule: with none it would detect, and so block, nothing`)
	}
	seen := make(map[string]bool, len(c.Rules))
	for i, r := range c.Rules {
		if err := validateRule(r, known); err != nil {
			return fmt.Errorf("config: rules[%d]: %w", i, err)
		}
		switch r.Action {
		case "", ActionBlock:
		case ActionSanitize:
			if c.Policy.Profile == ProfileStrict {
				return fmt.Errorf(`config: rules[%d]: action "sanitize" is not allowed under policy.profile "strict" (a verified approval may allow it in a later release; see docs/adr)`, i)
			}
		default:
			return fmt.Errorf("config: rules[%d]: action %q must be block or sanitize", i, r.Action)
		}
		if seen[r.Name] {
			return fmt.Errorf("config: rules[%d]: duplicate name %q", i, r.Name)
		}
		seen[r.Name] = true
	}
	return nil
}

// validateAudit checks the audit block. Under strict the decision log may not
// be weakened: it must be on and a failed write must block.
func (c *Config) validateAudit() error {
	a := c.Audit
	switch a.OnError {
	case "", AuditBlock, AuditWarn:
	default:
		return fmt.Errorf("config: audit.on_error %q must be block or warn", a.OnError)
	}
	if a.MaxSizeMB != nil && (*a.MaxSizeMB < 1 || *a.MaxSizeMB > maxAuditSizeMB) {
		return fmt.Errorf("config: audit.max_size_mb %d must be between 1 and %d", *a.MaxSizeMB, maxAuditSizeMB)
	}
	if a.RetentionDays != nil && (*a.RetentionDays < 0 || *a.RetentionDays > maxAuditRetentionDays) {
		return fmt.Errorf("config: audit.retention_days %d must be between 0 and %d (0 keeps rotated files forever)", *a.RetentionDays, maxAuditRetentionDays)
	}
	if c.Policy.Profile == ProfileStrict {
		if !a.IsEnabled() {
			return errors.New(`config: audit.enabled false is not allowed under policy.profile "strict": every decision must be recorded`)
		}
		if a.OnError == AuditWarn {
			return errors.New(`config: audit.on_error "warn" is not allowed under policy.profile "strict": a decision that cannot be recorded must block the request`)
		}
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
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("config: %s %q: %w", field, addr, err)
	}
	if _, err := net.LookupPort("tcp", port); err != nil {
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
