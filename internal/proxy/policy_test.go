package proxy

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"cui-proxy/internal/adapter/codex"
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
// the original request only when sanitizing FAILS, never to override a block,
// and never when any rule blocks (a failure that hits before the blocking
// rule is scanned would otherwise carry the value through).
func TestPolicyBlockIsNeverPassedThrough(t *testing.T) {
	h := newHarness(t, nil)
	h.cfg.Policy.OnError = "passthrough"
	useProfile(t, h, "sanitize", "block") // sanitize profile, but the rule blocks

	// The blocking rule matches: blocked as policy.
	res, body := h.post(userBody("host " + realIP))
	assertBlockedAudit(t, h, res, body, http.StatusUnprocessableEntity, "policy", "policy:rule_match")
	h.noUpstreamCall()
	if s := h.reg.Snapshot(); s.Passthrough != 0 || s.BlockedByReason["policy"] != 1 {
		t.Errorf("passthrough=%d by_reason=%v", s.Passthrough, s.BlockedByReason)
	}

	// Another failure hits first (an unparseable body, an image part, too
	// deep), with the value present: with blocking rules active it must block,
	// not forward the original.
	for name, raw := range map[string]string{
		"unparseable": "not json " + realIP,
		"image part":  `{"model":"m","input":[{"type":"message","role":"user","content":[{"type":"input_image","image_url":"x"},{"type":"input_text","text":"host ` + realIP + `"}]}]}`,
	} {
		res, body := h.post(raw)
		if res.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("%s: status %d body %s; want a block", name, res.StatusCode, body)
		}
		h.noUpstreamCall()
		h.nextAudit()
	}
	if s := h.reg.Snapshot(); s.Passthrough != 0 {
		t.Errorf("a request was passed through despite blocking rules: %d", s.Passthrough)
	}
}

// Unsupported content is refused even under on_error: passthrough (ADR 0001:
// policy.unsupported is always block). What passthrough still forwards is a
// failure of the sanitizer itself.
func TestPassthroughNeverForwardsUnsupportedContent(t *testing.T) {
	h := newHarness(t, nil)
	h.cfg.Policy.OnError = "passthrough"
	for name, raw := range map[string]string{
		"unparseable": "not json " + realIP,
		"image part":  `{"model":"m","input":[{"type":"message","role":"user","content":[{"type":"input_image","image_url":"x"}]}]}`,
	} {
		res, _ := h.post(raw)
		if res.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("%s: status %d; want a block", name, res.StatusCode)
		}
		h.noUpstreamCall()
		h.nextAudit()
	}
	// A sanitizer failure (no synthetic available) is what passthrough is for.
	fa := &fakeAdapter{Adapter: codex.New(), sanitize: func([]byte, *transform.Sanitizer) ([]byte, error) {
		return nil, fmt.Errorf("x: %w: rule=builtin.ipv4 class=ipv4", transform.ErrPseudonym)
	}}
	h2 := newHarness(t, nil, withAdapter(fa))
	h2.cfg.Policy.OnError = "passthrough"
	if res, _ := h2.post(userBody("host " + realIP)); res.StatusCode != http.StatusOK {
		t.Errorf("a sanitizer failure under passthrough: status %d; want the forward", res.StatusCode)
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

// The defense in depth on its own: a failure that passthrough WOULD forward
// (a sanitizer failure) is blocked when any rule blocks, whatever the config
// says. Without this, a failure hitting before the blocking rule's value is
// scanned would carry the value upstream.
func TestPassthroughRefusedWhileAnyRuleBlocks(t *testing.T) {
	fa := &fakeAdapter{Adapter: codex.New(), sanitize: func([]byte, *transform.Sanitizer) ([]byte, error) {
		return nil, fmt.Errorf("x: %w: rule=builtin.ipv4 class=ipv4", transform.ErrPseudonym)
	}}
	h := newHarness(t, nil, withAdapter(fa))
	h.cfg.Policy.OnError = "passthrough" // a code-built config skips validation
	useProfile(t, h, "sanitize", "block")
	res, body := h.post(userBody("host " + realIP))
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status %d body %s; want a block", res.StatusCode, body)
	}
	h.noUpstreamCall()
	if h.reg.Snapshot().Passthrough != 0 {
		t.Error("the request was counted as passed through")
	}

	// The same failure with NO blocking rule is what passthrough is for.
	h2 := newHarness(t, nil, withAdapter(fa))
	h2.cfg.Policy.OnError = "passthrough"
	useProfile(t, h2, "sanitize", "")
	if res, _ := h2.post(userBody("host " + realIP)); res.StatusCode != http.StatusOK {
		t.Errorf("without blocking rules: status %d; want the forward", res.StatusCode)
	}
}
