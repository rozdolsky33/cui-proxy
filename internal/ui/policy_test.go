package ui

import (
	"encoding/json"
	"strings"
	"testing"

	"cui-proxy/internal/config"
	"cui-proxy/internal/detect"
)

func TestConfigViewShowsProfileAndEffectiveActions(t *testing.T) {
	d := testDeps(t)
	c := d.Cfg()
	c.Policy.Profile = "sanitize"
	c.Rules[1].Action = "block"
	rec := do(New(d), "GET", "/api/config", "", nil)
	var v struct {
		Policy struct{ Profile, Unsupported string }
		Rules  []struct{ Name, Action string }
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Policy.Profile != "sanitize" || v.Policy.Unsupported != "block" {
		t.Errorf("policy = %+v", v.Policy)
	}
	if len(v.Rules) != 2 || v.Rules[0].Action != "sanitize" || v.Rules[1].Action != "block" {
		t.Errorf("rules = %+v", v.Rules)
	}

	// Under strict every rule shows block, whatever the rule says.
	c.Policy.Profile = "strict"
	c.Rules[1].Action = ""
	rec = do(New(d), "GET", "/api/config", "", nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Policy.Profile != "strict" || v.Rules[0].Action != "block" || v.Rules[1].Action != "block" {
		t.Errorf("strict view: %+v %+v", v.Policy, v.Rules)
	}
}

// A hand-built config (no profile set) reads as the default.
func TestConfigViewDefaultsTheProfile(t *testing.T) {
	d := testDeps(t)
	d.Cfg().Policy.Profile = ""
	rec := do(New(d), "GET", "/api/config", "", nil)
	if !strings.Contains(rec.Body.String(), `"profile":"sanitize"`) {
		t.Errorf("%s", rec.Body.String())
	}
}

func TestTestEndpointExplainsAPolicyBlock(t *testing.T) {
	d := testDeps(t)
	ds, err := detect.BuildForProfile([]config.Rule{{Name: "builtin", Type: "builtin"}}, "", "strict")
	if err != nil {
		t.Fatal(err)
	}
	d.Detectors = func() []detect.Detector { return ds }
	rec := do(New(d), "POST", "/api/test", `{"text":"ssh admin@10.20.30.15"}`, jsonCT)
	if rec.Code != 422 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	b := rec.Body.String()
	if !strings.Contains(b, "blocked by policy") || !strings.Contains(b, "rule=builtin.ipv4") {
		t.Errorf("the test box does not say it was blocked, and by which rule: %s", b)
	}
	if strings.Contains(b, "10.20.30.15") {
		t.Errorf("the response echoes the value: %s", b)
	}
}

// The config view shows the decision log settings, with the effective
// on_error (strict always blocks).
func TestConfigViewShowsAudit(t *testing.T) {
	d := testDeps(t)
	c := d.Cfg()
	off, days := false, 0
	c.Audit = config.Audit{File: "/state/decisions.jsonl", OnError: "warn", RetentionDays: &days}
	var v struct {
		Audit struct {
			Enabled       bool   `json:"enabled"`
			File          string `json:"file"`
			OnError       string `json:"on_error"`
			MaxSizeMB     int    `json:"max_size_mb"`
			RetentionDays int    `json:"retention_days"`
		}
	}
	get := func() {
		t.Helper()
		rec := do(New(d), "GET", "/api/config", "", nil)
		if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
	}
	get()
	if !v.Audit.Enabled || v.Audit.File != "/state/decisions.jsonl" || v.Audit.OnError != "warn" || v.Audit.MaxSizeMB != 50 || v.Audit.RetentionDays != 0 {
		t.Errorf("audit view %+v", v.Audit)
	}
	c.Policy.Profile = "strict"
	c.Audit.Enabled = &off
	get()
	if v.Audit.Enabled || v.Audit.OnError != "block" {
		t.Errorf("strict view %+v", v.Audit)
	}
}

func TestConfigViewAttachmentPolicy(t *testing.T) {
	d := testDeps(t)
	for _, tc := range []struct{ mode, want string }{{"", "block"}, {"extract_text", "extract_text"}} {
		d.Cfg().Policy.Attachments = tc.mode
		rec := do(New(d), "GET", "/api/config", "", nil)
		if !strings.Contains(rec.Body.String(), `"attachments":"`+tc.want+`"`) {
			t.Fatalf("policy not visible: %s", rec.Body.String())
		}
	}
}
