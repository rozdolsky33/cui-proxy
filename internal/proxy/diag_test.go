package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"cui-proxy/internal/adapter/codex"
	"cui-proxy/internal/synth"
	"cui-proxy/internal/transform"
)

func spanErr(kind error, rule, class string, cause error) error {
	return fmt.Errorf("codex: input: %w", &transform.SpanError{Kind: kind, Rule: rule, Class: class, Cause: cause})
}

// I2: a sanitize block carries a closed-set kind and the rule name (never a
// value) in the log line, the audit entry and the 422 body; metrics keys
// stay the closed reason set.
func TestBlockDiagnosticsCarryKindAndRule(t *testing.T) {
	deep := `{"input":` + strings.Repeat("[", 80) + strings.Repeat("]", 80) + `}`
	for _, c := range []struct {
		name, body         string
		err                error // nil: the real codex adapter runs
		reason, kind, rule string
		class, wantBlocked string
	}{
		{name: "bad_body", body: "not json " + realIP, reason: "sanitize", kind: "bad_body"},
		{name: "depth", body: deep, reason: "sanitize", kind: "depth"},
		{name: "pool", err: spanErr(transform.ErrPseudonym, "builtin.ipv4", "ipv4", fmt.Errorf("vault: ipv4: %w", synth.ErrExhausted)), reason: "sanitize", kind: "pool_exhausted", rule: "builtin.ipv4", class: "ipv4"},
		{name: "pseudonym", err: spanErr(transform.ErrPseudonym, "nets", "cidr", errors.New("vault: cidr: synth: invalid cidr")), reason: "sanitize", kind: "pseudonym", rule: "nets", class: "cidr"},
		{name: "ambiguous", err: spanErr(transform.ErrAmbiguous, "builtin.ipv4", "ipv4", nil), reason: "sanitize", kind: "ambiguous", rule: "builtin.ipv4", class: "ipv4"},
		{name: "unrestorable", err: spanErr(transform.ErrUnrestorable, "builtin.ssn", "ssn", nil), reason: "unrestorable", kind: "unrestorable", rule: "builtin.ssn", class: "ssn"},
		{name: "other", err: errors.New("boom " + realIP), reason: "sanitize", kind: "other"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var opts []option
			if c.err != nil {
				err := c.err
				opts = append(opts, withAdapter(&fakeAdapter{Adapter: codex.New(), sanitize: func([]byte, *transform.Sanitizer) ([]byte, error) { return nil, err }}))
			}
			h := newHarness(t, nil, opts...)
			body := c.body
			if body == "" {
				body = userBody(realIP)
			}
			res, rb := h.post(body)
			if res.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d", res.StatusCode)
			}
			var e apiError
			if err := json.Unmarshal(rb, &e); err != nil {
				t.Fatalf("body %q", rb)
			}
			msg := "cui-proxy: request blocked (reason=" + c.reason + " kind=" + c.kind
			if c.rule != "" {
				msg += " rule=" + c.rule
			}
			msg += ")"
			if e.Error.Code != c.reason || e.Error.Type != "cui_proxy_blocked" || e.Error.Message != msg+" request_id="+res.Header.Get("X-Cui-Proxy-Request-Id") {
				t.Errorf("error = %+v, want message %q", e.Error, msg)
			}
			a := h.nextAudit()
			wantBlocked := c.reason + ":" + c.kind
			if c.reason == c.kind {
				wantBlocked = c.reason
			}
			if a.Blocked != wantBlocked {
				t.Errorf("audit blocked = %q, want %q", a.Blocked, wantBlocked)
			}
			if c.rule != "" && (len(a.Rules) != 1 || a.Rules["blocked:"+c.rule] != 1) {
				t.Errorf("audit rules = %v", a.Rules)
			}
			if c.rule == "" && len(a.Rules) != 0 {
				t.Errorf("audit rules = %v", a.Rules)
			}
			rule, class := c.rule, c.class
			if rule == "" {
				rule, class = "-", "-"
			}
			line := fmt.Sprintf("cui-proxy: request blocked (reason=%s kind=%s rule=%s class=%s)\n", c.reason, c.kind, rule, class)
			if logs := h.logs.String(); !strings.Contains(logs, line) {
				t.Errorf("logs = %q, want line %q", logs, line)
			}
			s := h.reg.Snapshot()
			if len(s.BlockedByReason) != 1 || s.BlockedByReason[c.reason] != 1 {
				t.Errorf("blocked_by_reason = %v", s.BlockedByReason)
			}
			for _, out := range []string{string(rb), h.logs.String(), fmt.Sprint(a)} {
				if strings.Contains(out, realIP) {
					t.Errorf("real value in output %q", out)
				}
			}
			h.noUpstreamCall()
		})
	}
}

// A hostile rule or class name cannot inject log lines or break the JSON
// error body: the diagnostic keeps only [A-Za-z0-9._-].
func TestBlockDiagnosticsSanitizeNames(t *testing.T) {
	err := spanErr(transform.ErrAmbiguous, "evil\nrule=x \"}<b> "+strings.Repeat("a", 100), "ipv4\r\n", nil)
	h := newHarness(t, nil, withAdapter(&fakeAdapter{Adapter: codex.New(), sanitize: func([]byte, *transform.Sanitizer) ([]byte, error) { return nil, err }}))
	res, rb := h.post(userBody("hi"))
	if res.StatusCode != http.StatusUnprocessableEntity || !json.Valid(rb) {
		t.Fatalf("status %d body %q", res.StatusCode, rb)
	}
	logs := h.logs.String()
	if strings.Count(logs, "\n") != 1 || strings.ContainsAny(strings.TrimSuffix(logs, "\n"), "\r\n\"<>} ") {
		t.Errorf("logs = %q", logs)
	}
	a := h.nextAudit()
	for k := range a.Rules {
		if strings.ContainsAny(k, "\r\n\"<> } ") || len(k) > len("blocked:")+64 {
			t.Errorf("audit rule key %q", k)
		}
	}
	var e apiError
	_ = json.Unmarshal(rb, &e)
	if strings.ContainsAny(e.Error.Message, "\r\n\"<} ") {
		t.Errorf("message = %q", e.Error.Message)
	}
}
