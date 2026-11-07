package ui

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"cui-proxy/internal/audit"
	"cui-proxy/internal/config"
	"cui-proxy/internal/detect"
	"cui-proxy/internal/metrics"
)

const realIP = "10.20.30.40"

func testDeps(t *testing.T) Deps {
	t.Helper()
	cfg := &config.Config{
		Listen:    "127.0.0.1:8787",
		Upstreams: map[string]string{"codex": "https://chatgpt.com"},
		Policy:    config.Policy{OnError: "block", MaxBodyBytes: 1 << 20},
		Vault:     config.VaultConfig{Mode: "file", KeyFile: "/tmp/vault.key", File: "/tmp/vault.bin"},
		Rules: []config.Rule{
			{Name: "ips", Type: "builtin", Synth: "ipv4"},
			{Name: "people", Type: "dictionary", File: "/tmp/people.txt", Synth: "email"},
		},
	}
	cfg.UI.Listen = "127.0.0.1:8788"
	ds, err := detect.Build([]config.Rule{{Name: "ips", Type: "builtin", Synth: "ipv4"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	return Deps{
		Cfg:       func() *config.Config { return cfg },
		Metrics:   metrics.New(),
		Audit:     audit.New(50),
		Detectors: func() []detect.Detector { return ds },
		Reload:    func() error { return nil },
		Version:   "test-1.0",
	}
}

func do(h http.Handler, method, target, body string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Host = "127.0.0.1:8788"
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

var jsonCT = map[string]string{"Content-Type": "application/json"}

func TestConfigEndpointOmitsKey(t *testing.T) {
	h := New(testDeps(t))
	rec := do(h, "GET", "/api/config", "", nil)
	if rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	b := rec.Body.String()
	for _, want := range []string{"/tmp/vault.key", "/tmp/people.txt", "127.0.0.1:8787", "chatgpt.com", "test-1.0"} {
		if !strings.Contains(b, want) {
			t.Errorf("missing %q in %s", want, b)
		}
	}
	if strings.Contains(strings.ToLower(b), "\"key\"") {
		t.Errorf("config exposes a key field: %s", b)
	}
}

func TestMetricsEndpointShape(t *testing.T) {
	d := testDeps(t)
	d.Metrics.IncRequest()
	rec := do(New(d), "GET", "/api/metrics", "", nil)
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatal(ct)
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"requests", "blocked", "blocked_by_reason", "errors", "restores", "replacements_by_rule", "requests_per_minute", "overhead_ms", "vault_size", "uptime_seconds"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing %s", k)
		}
	}
	if m["requests"].(float64) != 1 {
		t.Errorf("requests=%v", m["requests"])
	}
}

func TestEventsAreValueFree(t *testing.T) {
	d := testDeps(t)
	d.Audit.Add(audit.Entry{Time: time.Now(), Adapter: "codex", Method: "POST", Path: "/v1/responses", Status: 200, Rules: map[string]int{"ips": 2}})
	d.Audit.Add(audit.Entry{Time: time.Now(), Method: "POST", Path: "/x", Status: 403, Blocked: "route"})
	rec := do(New(d), "GET", "/api/events", "", nil)
	var es []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &es); err != nil {
		t.Fatal(err)
	}
	if len(es) != 2 || es[0]["path"] != "/v1/responses" || es[1]["blocked"] != "route" {
		t.Fatalf("events=%v", es)
	}
	if strings.Contains(rec.Body.String(), realIP) {
		t.Fatal("real value present")
	}
	// empty log is [] not null
	d.Audit = audit.New(5)
	if got := strings.TrimSpace(do(New(d), "GET", "/api/events", "", nil).Body.String()); got != "[]" {
		t.Fatalf("empty=%q", got)
	}
}

func TestStreamEmitsNewEntries(t *testing.T) {
	d := testDeps(t)
	old := keepaliveInterval
	keepaliveInterval = 10 * time.Millisecond
	defer func() { keepaliveInterval = old }()
	srv := httptest.NewServer(New(d))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatal(ct)
	}
	sc := bufio.NewScanner(resp.Body)
	sawKeep, sawData := false, false
	added := false
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, ": keepalive") {
			sawKeep = true
			if !added { // subscription is live once headers flushed and keepalive seen
				d.Audit.Add(audit.Entry{Method: "GET", Path: "/stream-test", Status: 200})
				added = true
			}
		}
		if strings.HasPrefix(line, "data: ") && strings.Contains(line, "/stream-test") {
			sawData = true
			break
		}
	}
	if !sawKeep || !sawData {
		t.Fatalf("keep=%v data=%v err=%v", sawKeep, sawData, sc.Err())
	}
}

func TestTestEndpointUsesThrowawayVault(t *testing.T) {
	d := testDeps(t)
	h := New(d)
	body := `{"text":"host ` + realIP + ` ok"}`
	rec := do(h, "POST", "/api/test", body, jsonCT)
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), realIP) {
		t.Fatalf("real value echoed: %s", rec.Body.String())
	}
	var r struct {
		Sanitized string
		Matches   []struct {
			Rule, Class string
			Count       int
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(r.Sanitized, "host ") || len(r.Matches) != 1 || r.Matches[0].Count != 1 || r.Matches[0].Class != "ipv4" {
		t.Fatalf("resp=%+v", r)
	}
	s := d.Metrics.Snapshot()
	if s.Replacements != 0 || len(s.ReplacementsByRule) != 0 {
		t.Fatalf("live metrics touched: %+v", s)
	}
	// no-match text round-trips unchanged with empty matches list
	rec = do(h, "POST", "/api/test", `{"text":"hello"}`, jsonCT)
	if !strings.Contains(rec.Body.String(), `"matches":[]`) {
		t.Fatal(rec.Body.String())
	}
}

func TestTestEndpointLimitsAndErrors(t *testing.T) {
	h := New(testDeps(t))
	big := `{"text":"` + strings.Repeat("a", 1<<20+10) + `"}`
	if rec := do(h, "POST", "/api/test", big, jsonCT); rec.Code != 413 {
		t.Fatal(rec.Code)
	}
	if rec := do(h, "POST", "/api/test", `{nope`, jsonCT); rec.Code != 400 || strings.Contains(rec.Body.String(), "nope") {
		t.Fatal(rec.Code, rec.Body.String())
	}
}

func TestNonLoopbackHostRejected(t *testing.T) {
	h := New(testDeps(t))
	for _, host := range []string{"evil.example.com", "evil.example.com:8788", "10.0.0.5:8788", "127.0.0.1.evil.com", ""} {
		req := httptest.NewRequest("GET", "/healthz", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 421 && rec.Code != 403 {
			t.Errorf("host %q: %d", host, rec.Code)
		}
	}
	for _, host := range []string{"localhost:8788", "localhost", "127.0.0.1:1", "[::1]:8788", "[::1]"} {
		req := httptest.NewRequest("GET", "/healthz", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Errorf("host %q: %d", host, rec.Code)
		}
	}
}

func TestPostGuards(t *testing.T) {
	h := New(testDeps(t))
	cases := []struct {
		name string
		hdr  map[string]string
		want int
	}{
		{"no content-type", nil, 415},
		{"form content-type", map[string]string{"Content-Type": "text/plain"}, 415},
		{"json ok", jsonCT, 200},
		{"json charset ok", map[string]string{"Content-Type": "application/json; charset=utf-8"}, 200},
		{"same origin ok", map[string]string{"Content-Type": "application/json", "Origin": "http://127.0.0.1:8788"}, 200},
		{"cross origin", map[string]string{"Content-Type": "application/json", "Origin": "http://evil.example"}, 403},
		{"null origin", map[string]string{"Content-Type": "application/json", "Origin": "null"}, 403},
		{"cross-site fetch", map[string]string{"Content-Type": "application/json", "Sec-Fetch-Site": "cross-site"}, 403},
		{"same-site fetch", map[string]string{"Content-Type": "application/json", "Sec-Fetch-Site": "same-site"}, 403},
		{"same-origin fetch", map[string]string{"Content-Type": "application/json", "Sec-Fetch-Site": "same-origin"}, 200},
	}
	for _, c := range cases {
		if rec := do(h, "POST", "/api/reload", "", c.hdr); rec.Code != c.want {
			t.Errorf("%s: %d want %d", c.name, rec.Code, c.want)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := New(testDeps(t))
	for _, p := range []string{"/", "/app.js", "/api/metrics", "/nope"} {
		rec := do(h, "GET", p, "", nil)
		for k, v := range map[string]string{
			"Content-Security-Policy": "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'",
			"X-Content-Type-Options":  "nosniff",
			"Referrer-Policy":         "no-referrer",
			"Cache-Control":           "no-store",
			"X-Frame-Options":         "DENY",
		} {
			if rec.Header().Get(k) != v {
				t.Errorf("%s %s=%q", p, k, rec.Header().Get(k))
			}
		}
		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Error("CORS header present")
		}
	}
}

func TestRoutesAndMethods(t *testing.T) {
	h := New(testDeps(t))
	if rec := do(h, "GET", "/nope", "", nil); rec.Code != 404 {
		t.Fatal(rec.Code)
	}
	rec := do(h, "GET", "/api/reload", "", nil)
	if rec.Code != 405 || rec.Header().Get("Allow") != "POST" {
		t.Fatal(rec.Code, rec.Header())
	}
	if rec := do(h, "POST", "/api/metrics", "", jsonCT); rec.Code != 405 {
		t.Fatal(rec.Code)
	}
	if rec := do(h, "GET", "/healthz", "", nil); rec.Body.String() != "ok" {
		t.Fatal(rec.Body.String())
	}
}

func TestReloadErrorSurfaced(t *testing.T) {
	d := testDeps(t)
	d.Reload = func() error { return errors.New("config: rules[0]: bad pattern") }
	rec := do(New(d), "POST", "/api/reload", "", jsonCT)
	if rec.Code != 422 || !strings.Contains(rec.Body.String(), "bad pattern") {
		t.Fatal(rec.Code, rec.Body.String())
	}
	d.Reload = func() error { return errors.New(strings.Repeat("x", 5000)) }
	rec = do(New(d), "POST", "/api/reload", "", jsonCT)
	var r map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &r)
	if len(r["error"].(string)) > 2048 {
		t.Fatal("error not capped")
	}
	rec = do(New(testDeps(t)), "POST", "/api/reload", "", jsonCT)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatal(rec.Code, rec.Body.String())
	}
}

func TestStaticIndexServed(t *testing.T) {
	h := New(testDeps(t))
	rec := do(h, "GET", "/", "", nil)
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") || !strings.Contains(rec.Body.String(), "app.js") {
		t.Fatal(rec.Code, rec.Header())
	}
	if strings.Contains(rec.Body.String(), "<script>") || strings.Contains(rec.Body.String(), "style=") {
		t.Fatal("inline script/style")
	}
	if rec := do(h, "GET", "/app.js", "", nil); rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Type"), "javascript") {
		t.Fatal(rec.Code, rec.Header())
	}
	if rec := do(h, "GET", "/style.css", "", nil); rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Type"), "css") {
		t.Fatal(rec.Code, rec.Header())
	}
}

func TestJSHasNoBannedSinks(t *testing.T) {
	b, err := os.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "eval(", "new Function", "setTimeout(\"", "setInterval(\""} {
		if strings.Contains(string(b), bad) {
			t.Errorf("banned sink %q in app.js", bad)
		}
	}
}
