package config

import (
	"strings"
	"testing"
)

func TestPolicyDefaults(t *testing.T) {
	c, err := Load(writeTemp(t, "upstreams: {codex: 'https://api.openai.com'}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Policy.Profile != "sanitize" || c.Policy.Unsupported != "block" {
		t.Errorf("defaults: profile %q, unsupported %q; want sanitize, block", c.Policy.Profile, c.Policy.Unsupported)
	}
}

func TestPolicyProfileAndUnsupportedValidation(t *testing.T) {
	ok := func(mut func(*Config)) {
		t.Helper()
		c := base()
		mut(c)
		if err := c.Validate(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	}
	bad := func(want string, mut func(*Config)) {
		t.Helper()
		c := base()
		mut(c)
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("error %v; want it to mention %q", err, want)
		}
	}
	// A hand-built config without the new fields means the defaults.
	ok(func(c *Config) {})
	ok(func(c *Config) { c.Policy.Profile = "sanitize"; c.Policy.Unsupported = "block" })
	ok(func(c *Config) {
		c.Policy.Profile = "strict"
		c.Rules = []Rule{{Name: "r", Type: "regex", Pattern: "x", Synth: "secret"}}
	})
	bad("policy.profile", func(c *Config) { c.Policy.Profile = "lenient" })
	bad("policy.unsupported", func(c *Config) { c.Policy.Unsupported = "allow" })

	// strict is rigid: failures must block, never forward the original.
	bad("strict", func(c *Config) { c.Policy.Profile = "strict"; c.Policy.OnError = "passthrough" })
	ok(func(c *Config) { c.Policy.Profile = "sanitize"; c.Policy.OnError = "passthrough" })
}

func TestRuleActionValidation(t *testing.T) {
	rule := func(action string) Rule {
		return Rule{Name: "r", Type: "regex", Pattern: "x", Synth: "secret", Action: action}
	}
	for _, tc := range []struct {
		profile, action string
		wantErr         string // "" = valid
	}{
		{"sanitize", "", ""},
		{"sanitize", "block", ""},
		{"sanitize", "sanitize", ""},
		{"strict", "", ""},
		{"strict", "block", ""},
		{"strict", "sanitize", "strict"}, // a rule may not weaken strict (an approval will, later)
		{"sanitize", "allow", "action"},
		{"strict", "Block", "action"}, // exact, case-sensitive values
	} {
		c := base()
		c.Policy.Profile = tc.profile
		c.Rules = []Rule{rule(tc.action)}
		err := c.Validate()
		switch {
		case tc.wantErr == "" && err != nil:
			t.Errorf("%s/%q: unexpected error %v", tc.profile, tc.action, err)
		case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
			t.Errorf("%s/%q: error %v; want it to mention %q", tc.profile, tc.action, err, tc.wantErr)
		}
	}
}

func TestEffectiveAction(t *testing.T) {
	for _, tc := range []struct{ profile, action, want string }{
		{"", "", "sanitize"}, // hand-built configs: the default profile
		{"sanitize", "", "sanitize"},
		{"sanitize", "block", "block"},
		{"sanitize", "sanitize", "sanitize"},
		{"strict", "", "block"},
		{"strict", "block", "block"},
		{"strict", "sanitize", "block"}, // never weaker than strict, even if validation was skipped
	} {
		if got := EffectiveAction(tc.profile, Rule{Action: tc.action}); got != tc.want {
			t.Errorf("EffectiveAction(%q, action %q) = %q; want %q", tc.profile, tc.action, got, tc.want)
		}
	}
}

// Any blocking rule, not only the strict profile, refuses passthrough: a
// failure that hits before the blocking rule is scanned must not forward.
func TestBlockingRulesRefusePassthrough(t *testing.T) {
	c := base()
	c.Policy.OnError = "passthrough"
	c.Rules = []Rule{{Name: "r", Type: "regex", Pattern: "x", Synth: "secret", Action: "block"}}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "passthrough") || !strings.Contains(err.Error(), "block") {
		t.Errorf("error = %v; want passthrough refused next to a blocking rule", err)
	}
	// Without a blocking rule, passthrough is still accepted (UNSAFE, but its own opt-in).
	c.Rules[0].Action = ""
	if err := c.Validate(); err != nil {
		t.Errorf("sanitize rules with passthrough: %v", err)
	}
}

// An unknown profile string, if validation were skipped, is treated as the
// strictest: never weaker than asked.
func TestEffectiveActionUnknownProfileIsStrict(t *testing.T) {
	for _, profile := range []string{"STRICT", "Strict", "lenient", " strict"} {
		if got := EffectiveAction(profile, Rule{Action: "sanitize"}); got != "block" {
			t.Errorf("EffectiveAction(%q) = %q; want block", profile, got)
		}
	}
}

// Strict with no rules detects nothing: refusing it keeps "strict" honest.
func TestStrictNeedsAtLeastOneRule(t *testing.T) {
	c := base()
	c.Policy.Profile = "strict"
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "strict") || !strings.Contains(err.Error(), "rule") {
		t.Errorf("error = %v; want strict without rules refused", err)
	}
	c.Policy.Profile = "sanitize" // no rules under sanitize: allowed (policy check warns)
	if err := c.Validate(); err != nil {
		t.Errorf("sanitize with no rules: %v", err)
	}
}

func TestAttachmentSetting(t *testing.T) {
	for _, mode := range []string{"block", "extract_text"} {
		_, err := Load(writeTemp(t, "upstreams: {codex: 'https://api.openai.com'}\npolicy: {attachments: "+mode+"}\n"))
		if err != nil {
			t.Errorf("%s: %v", mode, err)
		}
	}
	_, err := Load(writeTemp(t, "upstreams: {codex: 'https://api.openai.com'}\npolicy: {attachments: allow}\n"))
	if err == nil {
		t.Fatal("accepted unsafe attachment mode")
	}
}
