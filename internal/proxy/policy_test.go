package proxy

import (
	"net/http"
	"strings"
	"testing"

	"cui-proxy/internal/config"
	"cui-proxy/internal/detect"
	"cui-proxy/internal/transform"
	"cui-proxy/internal/vault"
)

// useProfile swaps the harness's rule set for builtin rules built under
// profile (rule action as given).
func useProfile(t *testing.T, h *harness, profile, action string) {
	t.Helper()
	rules := []config.Rule{{Name: "builtin", Type: "builtin", Action: action}}
	ds, err := detect.BuildForProfile(rules, "", profile)
	if err != nil {
		t.Fatal(err)
	}
	v := vault.New(testKey)
	h.cur.Store(&set{v: v, s: transform.NewSanitizer(ds, v, nil), r: transform.NewRestorer(v)})
	h.cfg.Policy.Profile = profile
}

func TestStrictBlocksDetectionBeforeForwarding(t *testing.T) {
	h := newHarness(t, nil)
	useProfile(t, h, "strict", "")

	res, body := h.post(userBody("ssh admin@" + realIP + " and " + realOCID))
	assertBlockedAudit(t, h, res, body, http.StatusUnprocessableEntity, "policy", "policy:rule_match")
	h.noUpstreamCall()

	s := string(body)
	for _, leak := range []string{realIP, realOCID} {
		if strings.Contains(s, leak) {
			t.Errorf("the error response carries %q", leak)
		}
	}
	if !strings.Contains(s, "reason=policy") || !strings.Contains(s, "kind=rule_match") || !strings.Contains(s, "rule=builtin.") {
		t.Errorf("the 422 message does not say why: %s", s)
	}
	snap := h.reg.Snapshot()
	if snap.BlockedByReason["policy"] != 1 || snap.Blocked != 1 || snap.Replacements != 0 {
		t.Errorf("metrics: blocked=%d by_reason=%v replacements=%d", snap.Blocked, snap.BlockedByReason, snap.Replacements)
	}
	if n := h.set().v.Len(); n != 0 {
		t.Errorf("a blocked request coined %d vault mappings", n)
	}
	for _, line := range strings.Split(h.logs.String(), "\n") {
		if strings.Contains(line, realIP) || strings.Contains(line, realOCID) {
			t.Errorf("a log line carries a value: %q", line)
		}
	}
}

func TestStrictForwardsCleanRequests(t *testing.T) {
	h := newHarness(t, nil)
	useProfile(t, h, "strict", "")
	res, _ := h.post(userBody("explain how the build works"))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	if got := h.upstreamGot(); !strings.Contains(string(got.Body), "explain how the build works") {
		t.Errorf("upstream body = %q", got.Body)
	}
}

// A policy block is a decision, not a failure: on_error: passthrough forwards
// the original request only when sanitizing FAILS, never to override a block.
func TestPolicyBlockIsNeverPassedThrough(t *testing.T) {
	h := newHarness(t, nil)
	h.cfg.Policy.OnError = "passthrough"
	useProfile(t, h, "sanitize", "block") // sanitize profile, but the rule blocks
	res, body := h.post(userBody("host " + realIP))
	assertBlockedAudit(t, h, res, body, http.StatusUnprocessableEntity, "policy", "policy:rule_match")
	h.noUpstreamCall()
	if s := h.reg.Snapshot(); s.Passthrough != 0 || s.BlockedByReason["policy"] != 1 {
		t.Errorf("passthrough=%d by_reason=%v", s.Passthrough, s.BlockedByReason)
	}

	// And passthrough still works for what it is for: a failure that is not a policy block.
	res, _ = h.post("not json " + realIP)
	if res.StatusCode != http.StatusOK {
		t.Errorf("a sanitize failure under passthrough: status %d, want the forward", res.StatusCode)
	}
}

func TestSanitizeProfileStillMasks(t *testing.T) {
	h := newHarness(t, nil)
	useProfile(t, h, "sanitize", "")
	res, _ := h.post(userBody("host " + realIP))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	if got := string(h.upstreamGot().Body); strings.Contains(got, realIP) {
		t.Errorf("the upstream saw the real value: %s", got)
	}
}

// Header and query matches already blocked; the reason is unchanged.
func TestStrictHeaderBlockUnchanged(t *testing.T) {
	h := newHarness(t, nil)
	useProfile(t, h, "strict", "")
	req := h.req("POST", "/v1/responses", userBody("hello"))
	req.Header.Set("X-Custom", "from "+realIP)
	res, body := h.do(req)
	if res.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(string(body), `"code":"header"`) {
		t.Errorf("status %d body %s", res.StatusCode, body)
	}
	h.noUpstreamCall()
}
