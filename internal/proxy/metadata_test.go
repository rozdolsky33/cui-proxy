package proxy

import (
	"cui-proxy/internal/adapter/codex"
	"cui-proxy/internal/decisionlog"
	"cui-proxy/internal/transform"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestWorkspaceMetadataSanitized(t *testing.T) {
	for _, path := range []string{"/v1/responses", "/v1/models"} {
		t.Run(path, func(t *testing.T) {
			h := newHarness(t, nil)
			method, body := "POST", userBody("hi")
			if path == "/v1/models" {
				method, body = "GET", ""
			}
			req := h.req(method, path, body)
			req.Header.Set("X-Codex-Turn-Metadata", `{"session_id":"00000000-0000-4000-8000-000000000003","cwd":"/tmp/10.20.30.15","workspaces":{"/tmp/10.20.30.15":{"associated_remote_urls":{"origin":"git@github.com:example/demo.git"}}}}`)
			res, b := h.do(req)
			if res.StatusCode != 200 {
				t.Fatalf("status %d: %s", res.StatusCode, b)
			}
			got := h.upstreamGot().Header.Get("X-Codex-Turn-Metadata")
			if !json.Valid([]byte(got)) || strings.Contains(got, "git@github.com") || strings.Contains(got, "10.20.30.15") {
				t.Fatalf("unsafe metadata: %s", got)
			}
			if !strings.Contains(got, "00000000-0000-4000-8000-000000000003") {
				t.Fatal("session ID changed")
			}
			a := h.nextAudit()
			if a.Rules["builtin.email"] != 1 || a.Rules["builtin.ipv4"] != 2 {
				t.Fatalf("missing metadata accounting: %+v", a)
			}
		})
	}
}

func TestWorkspaceMetadataFailsClosed(t *testing.T) {
	for _, raw := range []string{`{`, `[]`, `{"workspaces":{}} {}`, `{"unknown":"alice\u0040example.com"}`, `{"workspaces":"wrong shape"}`, `{"cwd":123}`, `{"unknown":"{\"email\":\"alice\\u0040example.com\"}"}`, `{"unknown":4.111111111111111e15}`} {
		t.Run(raw, func(t *testing.T) {
			h := newHarness(t, nil)
			h.cfg.Policy.OnError = "passthrough"
			req := h.req("POST", "/v1/responses", userBody("hi"))
			req.Header.Set("X-Codex-Turn-Metadata", raw)
			res, _ := h.do(req)
			if res.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("status %d", res.StatusCode)
			}
			h.noUpstreamCall()
		})
	}
}

func TestWorkspaceMetadataHonorsBlockingRules(t *testing.T) {
	for _, profile := range []string{"strict", "sanitize"} {
		t.Run(profile, func(t *testing.T) {
			h := newHarness(t, nil)
			useProfile(t, h, profile, "block")
			h.cfg.Policy.OnError = "passthrough"
			req := h.req("POST", "/v1/responses", userBody("hi"))
			req.Header.Set("X-Codex-Turn-Metadata", `{"workspaces":{"demo":{"remote":"git@github.com:example/demo.git"}}}`)
			res, b := h.do(req)
			if res.StatusCode != 422 || !strings.Contains(string(b), "rule=builtin.email") {
				t.Fatalf("status %d: %s", res.StatusCode, b)
			}
			h.noUpstreamCall()
			if strings.Contains(h.logs.String(), "git@github.com") || strings.Contains(string(b), "git@github.com") {
				t.Fatal("diagnostics leaked value")
			}
		})
	}
}

func TestMetadataAccountingWaitsForBody(t *testing.T) {
	h := newHarness(t, nil)
	req := h.req("POST", "/v1/responses", "not json")
	req.Header.Set("X-Codex-Turn-Metadata", `{"cwd":"/tmp/10.20.30.15"}`)
	res, _ := h.do(req)
	if res.StatusCode != 422 {
		t.Fatalf("status %d", res.StatusCode)
	}
	h.noUpstreamCall()
	if h.reg.Snapshot().Replacements != 0 {
		t.Fatal("blocked request counted replacements")
	}
}

// A successful header rewrite must remain auditable even when the explicitly
// unsafe body passthrough policy is exercised by a sanitizer failure.
type metadataBodyFailure struct{ *codex.Adapter }

func (*metadataBodyFailure) SanitizeRequest([]byte, *transform.Sanitizer) ([]byte, error) {
	return nil, transform.ErrPseudonym
}
func TestMetadataPassthroughAudit(t *testing.T) {
	h := newHarness(t, nil, withAdapter(&metadataBodyFailure{codex.New()}))
	h.cfg.Policy.OnError = "passthrough"
	req := h.req("POST", "/v1/responses", userBody("hi"))
	req.Header.Set("X-Codex-Turn-Metadata", `{"cwd":"/tmp/10.20.30.15"}`)
	res, _ := h.do(req)
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	r := h.recordFor(res)
	if r.Decision != decisionlog.Forwarded || r.Counts["builtin.ipv4"] != 1 || r.Counts["passthrough:sanitize"] != 1 {
		t.Fatalf("record: %+v", r)
	}
}

func TestMetadataEscapedBoundaryHTTP(t *testing.T) {
	h := newHarness(t, nil)
	req := h.req("POST", "/v1/responses", userBody("hi"))
	req.Header.Set("X-Codex-Turn-Metadata", `{"cwd":"/tmp/\nalice@example.com"}`)
	res, b := h.do(req)
	if res.StatusCode != 200 {
		t.Fatalf("sanitized metadata rejected: %d %s", res.StatusCode, b)
	}
	got := h.upstreamGot().Header.Get("X-Codex-Turn-Metadata")
	if !json.Valid([]byte(got)) || strings.Contains(got, "alice@example.com") {
		t.Fatal("unsafe metadata")
	}
	a := h.nextAudit()
	if a.Rules["builtin.email"] != 1 {
		t.Fatalf("wrong accounting: %+v", a)
	}
}
