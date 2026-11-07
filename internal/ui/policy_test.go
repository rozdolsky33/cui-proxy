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
