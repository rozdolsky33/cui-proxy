package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, s string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadAppliesDefaults(t *testing.T) {
	c, err := Load(writeTemp(t, "upstreams: {codex: 'https://api.openai.com'}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != "127.0.0.1:8787" || c.UI.Listen != "127.0.0.1:8788" {
		t.Errorf("listen defaults: %q %q", c.Listen, c.UI.Listen)
	}
	if c.Policy.OnError != "block" || c.Policy.MaxBodyBytes != 20<<20 {
		t.Errorf("policy defaults: %+v", c.Policy)
	}
	if c.Vault.Mode != "memory" {
		t.Errorf("vault mode: %q", c.Vault.Mode)
	}
}

func TestLoadValidFile(t *testing.T) {
	c, err := Load("../../testdata/config/valid.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Rules) != 3 || c.Rules[1].Validate != "ipv4" || c.Upstreams["codex"] == "" {
		t.Errorf("unexpected: %+v", c)
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	if _, err := Load(writeTemp(t, "lisen: 127.0.0.1:1\n")); err == nil {
		t.Fatal("expected error for typo field")
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Fatal("expected error")
	}
}

func base() *Config {
	return &Config{
		Listen: "127.0.0.1:8787",
		Policy: Policy{OnError: "block", MaxBodyBytes: 1},
		Vault:  VaultConfig{Mode: "memory"},
	}
}

func TestValidateRejectsNonLoopback(t *testing.T) {
	c := base()
	c.Listen = "0.0.0.0:8787"
	if err := c.Validate(); err == nil {
		t.Error("0.0.0.0 listen should fail")
	}
	c = base()
	c.UI.Listen = "192.168.1.5:8788"
	if err := c.Validate(); err == nil {
		t.Error("non-loopback ui.listen should fail")
	}
	c.AllowNonLoopback = true
	if err := c.Validate(); err != nil {
		t.Errorf("allowed with flag: %v", err)
	}
	for _, ok := range []string{"localhost:1", "[::1]:1", "127.0.0.1:1"} {
		c := base()
		c.Listen = ok
		if err := c.Validate(); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	c = base()
	c.Listen = "garbage"
	if err := c.Validate(); err == nil {
		t.Error("unparseable listen should fail")
	}
}

func TestValidateRejectsBadRule(t *testing.T) {
	good := Rule{Name: "a", Type: "regex", Pattern: `\d+`, Synth: "ipv4"}
	cases := map[string][]Rule{
		"unknown type":   {{Name: "a", Type: "bogus", Synth: "x"}},
		"empty name":     {{Name: "", Type: "regex", Pattern: "x", Synth: "x"}},
		"duplicate name": {good, good},
		"bad regex":      {{Name: "a", Type: "regex", Pattern: "(", Synth: "x"}},
		"empty synth":    {{Name: "a", Type: "regex", Pattern: "x"}},
		"regex no pat":   {{Name: "a", Type: "regex", Synth: "x"}},
		"dict no file":   {{Name: "a", Type: "dictionary", Synth: "x"}},
	}
	for name, rules := range cases {
		c := base()
		c.Rules = rules
		if err := c.Validate(); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	c := base()
	c.Rules = []Rule{good}
	if err := c.Validate(); err != nil {
		t.Errorf("good rule: %v", err)
	}
	if err := c.ValidateWith(func(string) bool { return false }); err == nil {
		t.Error("unknown synth class should fail ValidateWith")
	}
	if err := c.ValidateWith(func(s string) bool { return s == "ipv4" }); err != nil {
		t.Errorf("known synth: %v", err)
	}
}

func TestValidatePolicyAndVaultEnums(t *testing.T) {
	c := base()
	c.Policy.OnError = "maybe"
	if err := c.Validate(); err == nil {
		t.Error("bad on_error")
	}
	c = base()
	c.Vault.Mode = "disk"
	if err := c.Validate(); err == nil {
		t.Error("bad vault mode")
	}
}

func TestValidateRejectsFileModeWithoutKeyFile(t *testing.T) {
	c := base()
	c.Vault = VaultConfig{Mode: "file", File: "v.bin"}
	if err := c.Validate(); err == nil {
		t.Error("file mode without key_file should fail")
	}
	c.Vault.KeyFile = "k"
	if err := c.Validate(); err != nil {
		t.Errorf("file mode with key_file: %v", err)
	}
	c.Vault.File = ""
	if err := c.Validate(); err == nil {
		t.Error("file mode without file should fail")
	}
}
