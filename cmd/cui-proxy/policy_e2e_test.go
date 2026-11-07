package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// strictConfig is the e2e config with policy.profile set.
func strictConfig(e *e2eEnv, profile string) string {
	return strings.Replace(e2eConfig(e.upstreamBase(), "hosts.txt", ""),
		"policy: { on_error: block,", "policy: { profile: "+profile+", on_error: block,", 1)
}

// minimalResponder answers every request with a one-event SSE stream.
func minimalResponder(w http.ResponseWriter, _ *http.Request, _ capture) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	writeEvent(w, "response.completed", `{"type":"response.completed","response":{"id":"r1","status":"completed"}}`)
}

func (e *e2eEnv) post(text string) (*http.Response, string) {
	e.t.Helper()
	res, err := e.client.Post(e.pxURL+"/v1/responses", "application/json", strings.NewReader(userBody(text)))
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res, string(b)
}

func (e *e2eEnv) reloadWith(cfg string) {
	e.t.Helper()
	writeFile(e.t, e.cfgPath, cfg)
	if code, b := e.ui("POST", "/api/reload", "{}"); code != 200 {
		e.t.Fatalf("reload: %d %s", code, b)
	}
}

func TestStrictProfileBlocksEndToEnd(t *testing.T) {
	e := newE2E(t)
	e.up.setRespond(minimalResponder)
	const secretText = "deploy to " + e2eHost + " at 10.20.30.15"

	// Under the default profile the request is masked and forwarded.
	res, _ := e.post(secretText)
	if res.StatusCode != 200 {
		t.Fatalf("sanitize profile: status %d", res.StatusCode)
	}
	c := e.up.next(t)
	assertNoValues(t, "upstream request under sanitize", c.dump(), e2eHost, "10.20.30.15")

	// Switch to strict through a reload: no restart.
	e.reloadWith(strictConfig(e, "strict"))
	before := e.up.count()
	res, body := e.post(secretText)
	if res.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, `"code":"policy"`) {
		t.Fatalf("strict: status %d body %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "rule=") {
		t.Errorf("the block does not name the rule: %s", body)
	}
	assertNoValues(t, "strict error body", body, e2eHost, "10.20.30.15")
	if got := e.up.count(); got != before {
		t.Fatalf("a request blocked by strict reached the upstream (%d -> %d requests)", before, got)
	}
	// Observable on the dashboard, with no value.
	if m := e.metrics(); m.BlockedByReason["policy"] != 1 {
		t.Errorf("blocked_by_reason = %v; want policy:1", m.BlockedByReason)
	}
	evs, raw := e.events()
	last := evs[len(evs)-1]
	if last.Blocked != "policy:rule_match" || last.Status != 422 {
		t.Errorf("audit event = %+v", last)
	}
	assertNoValues(t, "audit feed", string(raw), e2eHost, "10.20.30.15")
	if code, b := e.ui("GET", "/api/config", ""); code != 200 || !strings.Contains(string(b), `"profile":"strict"`) {
		t.Errorf("/api/config does not show the active profile: %d %s", code, b)
	}

	// A clean request is still forwarded under strict.
	if res, _ := e.post("explain the build"); res.StatusCode != 200 {
		t.Errorf("clean request under strict: status %d", res.StatusCode)
	}

	// And back: the reload applies in both directions.
	e.reloadWith(strictConfig(e, "sanitize"))
	if res, _ := e.post(secretText); res.StatusCode != 200 {
		t.Errorf("after switching back to sanitize: status %d", res.StatusCode)
	}
}

// Strict is not weakened by a config trying to: validation refuses it and the
// running policy stays.
func TestReloadRefusesToWeakenStrict(t *testing.T) {
	e := newE2E(t)
	e.up.setRespond(minimalResponder)
	e.reloadWith(strictConfig(e, "strict"))

	weaker := strings.Replace(strictConfig(e, "strict"), "on_error: block", "on_error: passthrough", 1)
	writeFile(t, e.cfgPath, weaker)
	if code, _ := e.ui("POST", "/api/reload", "{}"); code == 200 {
		t.Fatal("a strict config with on_error passthrough was accepted")
	}
	before := e.up.count()
	if res, _ := e.post("host 10.20.30.15"); res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("after the refused reload the policy changed: status %d", res.StatusCode)
	}
	if e.up.count() != before {
		t.Error("a request reached the upstream under the (still active) strict policy")
	}
}
