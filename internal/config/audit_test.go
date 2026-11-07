package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditDefaults(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/xdg/state")
	for _, c := range []struct {
		profile, onError string
	}{
		{"sanitize", "warn"},
		{"strict", "block"},
	} {
		yaml := "upstreams: {codex: 'https://api.openai.com'}\npolicy: {profile: " + c.profile + "}\nrules: [{name: b, type: builtin}]\n"
		cfg, err := Load(writeTemp(t, yaml))
		if err != nil {
			t.Fatal(err)
		}
		a := cfg.Audit
		if !a.IsEnabled() || a.File != "/xdg/state/cui-proxy/decisions.jsonl" || a.OnError != c.onError ||
			a.MaxBytes() != 50<<20 || a.Retention() != 90 {
			t.Errorf("%s: audit defaults %+v (max %d, retention %d)", c.profile, a, a.MaxBytes(), a.Retention())
		}
		if got := cfg.AuditBlocks(); got != (c.onError == "block") {
			t.Errorf("%s: AuditBlocks = %v", c.profile, got)
		}
	}
}

func TestDefaultAuditFile(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/u")
	if got := DefaultAuditFile(); got != filepath.Join("/home/u", ".local", "state", "cui-proxy", "decisions.jsonl") {
		t.Errorf("without XDG_STATE_HOME: %s", got)
	}
	t.Setenv("XDG_STATE_HOME", "relative/dir") // not absolute: ignored, as the XDG spec says
	if got := DefaultAuditFile(); !strings.HasPrefix(got, "/home/u/") {
		t.Errorf("relative XDG_STATE_HOME: %s", got)
	}
}

func TestAuditValidation(t *testing.T) {
	off, on := false, true
	n := func(v int) *int { return &v }
	strictRules := []Rule{{Name: "r", Type: "regex", Pattern: "x", Synth: "secret"}}
	for _, c := range []struct {
		name    string
		profile string
		audit   Audit
		want    string // "" = valid
	}{
		{"empty is the default", "sanitize", Audit{}, ""},
		{"sanitize may warn", "sanitize", Audit{OnError: "warn"}, ""},
		{"sanitize may block", "sanitize", Audit{OnError: "block"}, ""},
		{"sanitize may disable", "sanitize", Audit{Enabled: &off}, ""},
		{"strict blocks", "strict", Audit{OnError: "block", Enabled: &on}, ""},
		{"strict may not warn", "strict", Audit{OnError: "warn"}, `audit.on_error "warn" is not allowed under policy.profile "strict"`},
		{"strict may not disable", "strict", Audit{Enabled: &off}, `audit.enabled false is not allowed under policy.profile "strict"`},
		{"unknown on_error", "sanitize", Audit{OnError: "ignore"}, "audit.on_error"},
		{"case-sensitive on_error", "sanitize", Audit{OnError: "Block"}, "audit.on_error"},
		{"size zero", "sanitize", Audit{MaxSizeMB: n(0)}, "audit.max_size_mb"},
		{"size negative", "sanitize", Audit{MaxSizeMB: n(-1)}, "audit.max_size_mb"},
		{"size huge", "sanitize", Audit{MaxSizeMB: n(1<<20 + 1)}, "audit.max_size_mb"},
		{"size one", "sanitize", Audit{MaxSizeMB: n(1)}, ""},
		{"retention negative", "sanitize", Audit{RetentionDays: n(-1)}, "audit.retention_days"},
		{"retention zero keeps forever", "sanitize", Audit{RetentionDays: n(0)}, ""},
	} {
		cfg := base()
		cfg.Policy.Profile = c.profile
		cfg.Rules = strictRules
		cfg.Audit = c.audit
		err := cfg.Validate()
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: unexpected error %v", c.name, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s: error %v; want it to mention %q", c.name, err, c.want)
		}
	}
}

func TestAuditRejectedInYAML(t *testing.T) {
	for name, yaml := range map[string]string{
		"unknown key":       "audit: {enabled: true, bogus: 1}\n",
		"strict warn":       "policy: {profile: strict}\naudit: {on_error: warn}\nrules: [{name: b, type: builtin}]\n",
		"strict disabled":   "policy: {profile: strict}\naudit: {enabled: false}\nrules: [{name: b, type: builtin}]\n",
		"size not a number": "audit: {max_size_mb: big}\n",
	} {
		if _, err := Load(writeTemp(t, "upstreams: {codex: 'https://api.openai.com'}\n"+yaml)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// retention_days: 0 is kept as 0 (forever), not replaced by the default.
	cfg, err := Load(writeTemp(t, "upstreams: {codex: 'https://api.openai.com'}\naudit: {retention_days: 0, max_size_mb: 5, file: rel/d.jsonl}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Audit.Retention() != 0 || cfg.Audit.MaxBytes() != 5<<20 || cfg.Audit.File != "rel/d.jsonl" {
		t.Errorf("audit = %+v", cfg.Audit)
	}
}

// AuditBlocks is never weaker than asked: strict (or any unknown profile in
// a config that skipped validation) always blocks, whatever on_error says.
func TestAuditBlocksUnderStrictWhateverOnError(t *testing.T) {
	for _, c := range []struct {
		profile, onError string
		want             bool
	}{
		{"", "", true},
		{"sanitize", "", true},
		{"sanitize", "block", true},
		{"sanitize", "warn", false},
		{"", "warn", false},
		{"strict", "warn", true},
		{"lenient", "warn", true},
	} {
		cfg := &Config{Policy: Policy{Profile: c.profile}, Audit: Audit{OnError: c.onError}}
		if got := cfg.AuditBlocks(); got != c.want {
			t.Errorf("profile %q on_error %q: AuditBlocks = %v, want %v", c.profile, c.onError, got, c.want)
		}
	}
}
