package ui

import (
	"cui-proxy/internal/config"
	"cui-proxy/internal/preflight"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPreflightUsesRunningServicePolicy(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	d := testDeps(t)
	cfg := &config.Config{}
	cfg.Policy.Attachments = "extract_text"
	d.Cfg = func() *config.Config { return cfg }
	d.Version = "running-test-build"
	h := New(d)
	res := do(h, http.MethodGet, "http://localhost/api/preflight", "", nil)
	if res.Code != 200 {
		t.Fatal("preflight endpoint unavailable")
	}
	var report preflight.Report
	if json.Unmarshal(res.Body.Bytes(), &report) != nil {
		t.Fatal("invalid preflight")
	}
	if report.Ready || report.Version != "running-test-build" || len(report.Tools) != 5 || report.Tools[3].Code != "missing_tool" {
		t.Fatal("service dependency not diagnosed")
	}
	cfg.Policy.Attachments = "block"
	res = do(h, http.MethodGet, "http://localhost/api/preflight", "", nil)
	if json.Unmarshal(res.Body.Bytes(), &report) != nil || !report.Ready || report.Policy.Attachments != "block" {
		t.Fatal("policy reload not reflected")
	}
	denied := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://untrusted.invalid/api/preflight", nil)
	h.ServeHTTP(denied, req)
	if denied.Code != 421 {
		t.Fatal("preflight bypassed host guard")
	}
}
