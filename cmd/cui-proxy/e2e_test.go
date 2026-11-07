package main

// End-to-end tests: the real App (config file, rules, vault, proxy and
// dashboard) on real loopback listeners, driven over HTTP like Codex and a
// browser would, against a fake upstream that records every byte it
// receives. They live in package main because App is not importable from
// another package; an exported wrapper package would add API surface only
// for tests.
//
// No test here contacts a real upstream or uses a real credential: the
// upstream is an httptest server and every value is synthetic test data.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"cui-proxy/internal/sse"
)

// Seeded real-looking values. The first five are already in the Codex
// request fixture; the rest are added to it as a user message.
const (
	e2eIP           = "10.20.30.15" // user text
	e2eIPTool       = "10.20.30.16" // tool description
	e2eIPMeta       = "10.20.30.17" // client_metadata turn metadata
	e2eOCID         = "ocid1.instance.oc1.phx.anyhqljrabcdefghijklmnop"
	e2eFixtureEmail = "alice.smith@corp-internal.com"
	e2eSSN          = "512-34-5678"
	e2eHost         = "db01.prod.agency.internal" // dictionary entry
	e2eHost2        = "billing.agency.internal"   // dictionary entry after a reload
	e2eEmail        = "bob.jones@corp-internal.com"
	e2eCard         = "4111 1111 1111 1111"
	e2eCardDigits   = "4111111111111111"
	e2eBearerTok    = "sk-live-SEEDED0123456789abcdef" // after "Bearer " in the body
	e2eAWS          = "AKIAIOSFODNN7SEEDED1"           // regex rule
	e2eJWT          = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJzZWVkIn0.c2VlZGVkc2lnbmF0dXJl"

	// Fake client credentials: forwarded to the upstream untouched, never
	// shown anywhere else.
	fakeAuthTok = "e2e-FAKE-credential-0123456789abcdef"
	fakeAuth    = "Bearer " + fakeAuthTok
	fakeAcct    = "e2e-FAKE-account-7f3a9c"
)

// seededReals are the values that must never reach the upstream.
var seededReals = []string{
	e2eIP, e2eIPTool, e2eIPMeta, e2eOCID, e2eFixtureEmail, e2eSSN, e2eHost,
	e2eEmail, e2eCard, e2eCardDigits, e2eBearerTok, e2eAWS, e2eJWT,
}

// secretsEverywhere are the values that must not appear on any output
// surface (logs, dashboard API, feed, CLI output).
var secretsEverywhere = append(append([]string{}, seededReals...), e2eHost2, fakeAuthTok, fakeAcct)

const seededText = "Ping " + e2eIP + " from " + e2eHost + "; SSN " + e2eSSN + "; mail " + e2eEmail +
	"; card " + e2eCard + "; header Authorization: Bearer " + e2eBearerTok +
	"; aws_access_key_id=" + e2eAWS + "; token " + e2eJWT + "."

// ---- environment ----

// capture is what the fake upstream received.
type capture struct {
	Method, URI, Host string
	Header            http.Header
	Body              []byte
}

// dump renders every byte of the request the upstream could see.
func (c capture) dump() string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "%s %s\nHost: %s\n", c.Method, c.URI, c.Host)
	_ = c.Header.Write(&b)
	b.Write(c.Body)
	return b.String()
}

// recUpstream is a fake Codex backend that records each request and answers
// with the current respond function.
type recUpstream struct {
	srv     *httptest.Server
	reqs    chan capture
	mu      sync.Mutex
	n       int
	respond func(w http.ResponseWriter, r *http.Request, c capture)
}

func (u *recUpstream) setRespond(f func(w http.ResponseWriter, r *http.Request, c capture)) {
	u.mu.Lock()
	u.respond = f
	u.mu.Unlock()
}

func (u *recUpstream) count() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.n
}

// next returns the next request the upstream received.
func (u *recUpstream) next(t *testing.T) capture {
	t.Helper()
	select {
	case c := <-u.reqs:
		return c
	case <-time.After(waitLimit):
		t.Fatal("upstream received no request")
		return capture{}
	}
}

type e2eEnv struct {
	t       *testing.T
	app     *App
	dir     string
	cfgPath string
	logs    *logBuf
	up      *recUpstream
	pxURL   string
	uiURL   string
	client  *http.Client
}

// e2eConfig renders the e2e config: builtin rules, a dictionary of hosts and
// a regex rule for AWS-style access key ids.
func e2eConfig(upstream, hostsFile, extra string) string {
	return fmt.Sprintf(`listen: 127.0.0.1:0
ui: { listen: 127.0.0.1:0 }
upstreams:
  codex: %s
policy: { on_error: block, max_body_bytes: 1048576 }
vault: { mode: memory }
%s
rules:
  - name: builtin
    type: builtin
  - name: hosts
    type: dictionary
    file: %s
    synth: hostname
  - name: aws-keys
    type: regex
    pattern: 'AKIA[0-9A-Z]{16}'
    synth: secret
`, upstream, extra, hostsFile)
}

func newE2E(t *testing.T) *e2eEnv {
	t.Helper()
	e := &e2eEnv{t: t, dir: t.TempDir(), logs: &logBuf{}}
	e.up = &recUpstream{reqs: make(chan capture, 16)}
	e.up.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		c := capture{r.Method, r.RequestURI, r.Host, r.Header.Clone(), body}
		e.up.mu.Lock()
		e.up.n++
		respond := e.up.respond
		e.up.mu.Unlock()
		e.up.reqs <- c
		if respond == nil {
			http.Error(w, "no responder", http.StatusInternalServerError)
			return
		}
		respond(w, r, c)
	}))
	t.Cleanup(e.up.srv.Close)

	writeFile(t, filepath.Join(e.dir, "hosts.txt"), "# hosts\n"+e2eHost+"\n")
	writeFile(t, filepath.Join(e.dir, "hosts2.txt"), "# hosts after reload\n"+e2eHost2+"\n")
	e.cfgPath = filepath.Join(e.dir, "config.yaml")
	writeFile(t, e.cfgPath, e2eConfig(e.upstreamBase(), "hosts.txt", ""))
	e.app = startApp(t, e.cfgPath, e.logs, nil)
	px, ui := e.app.Addrs()
	e.pxURL, e.uiURL = "http://"+px, "http://"+ui
	tr := &http.Transport{DisableCompression: true}
	t.Cleanup(tr.CloseIdleConnections)
	e.client = &http.Client{Transport: tr, Timeout: waitLimit, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse // observe exactly what the proxy relays
	}}
	return e
}

func (e *e2eEnv) upstreamBase() string { return e.up.srv.URL + "/backend-api/codex" }

// synFor returns the synthetic the app's vault holds for a real value,
// without creating one.
func (e *e2eEnv) synFor(real string) (string, bool) {
	for _, s := range e.app.vault.Synthetics() {
		if r, ok := e.app.vault.Real(s); ok && r == real {
			return s, true
		}
	}
	return "", false
}

func (e *e2eEnv) mustSyn(real string) string {
	e.t.Helper()
	s, ok := e.synFor(real)
	if !ok {
		e.t.Fatal("the vault holds no synthetic for a seeded value")
	}
	return s
}

// codexRequest sends body to the proxy's Responses endpoint with the
// headers Codex sends.
func (e *e2eEnv) codexRequest(body []byte, extra http.Header) (*http.Response, []byte) {
	e.t.Helper()
	req, err := http.NewRequest("POST", e.pxURL+"/v1/responses", bytes.NewReader(body))
	if err != nil {
		e.t.Fatal(err)
	}
	h := req.Header
	h.Set("Content-Type", "application/json")
	h.Set("Accept", "text/event-stream")
	h.Set("Authorization", fakeAuth)
	h.Set("Chatgpt-Account-Id", fakeAcct)
	h.Set("Originator", "codex_cli_rs")
	h.Set("Session_id", "00000000-0000-4000-8000-000000000001")
	h.Set("User-Agent", "codex_cli_rs/0.0.0-e2e")
	for k, vs := range extra {
		h[k] = vs
	}
	res, err := e.client.Do(req)
	if err != nil {
		e.t.Fatalf("proxy request: %v", err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		e.t.Fatalf("read proxy response: %v", err)
	}
	return res, b
}

// ui sends a dashboard request with a loopback Host.
func (e *e2eEnv) ui(method, path string, body string) (int, []byte) {
	e.t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, e.uiURL+path, rd)
	if err != nil {
		e.t.Fatal(err)
	}
	if method != "GET" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := e.client.Do(req)
	if err != nil {
		e.t.Fatalf("dashboard request: %v", err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		e.t.Fatal(err)
	}
	return res.StatusCode, b
}

type e2eMetrics struct {
	Requests           int64            `json:"requests"`
	Blocked            int64            `json:"blocked"`
	BlockedByReason    map[string]int64 `json:"blocked_by_reason"`
	Errors             int64            `json:"errors"`
	Restores           int64            `json:"restores"`
	ReplacementsByRule map[string]int64 `json:"replacements_by_rule"`
	VaultSize          int64            `json:"vault_size"`
}

func (e *e2eEnv) metrics() e2eMetrics {
	e.t.Helper()
	code, b := e.ui("GET", "/api/metrics", "")
	var m e2eMetrics
	if code != 200 || json.Unmarshal(b, &m) != nil {
		e.t.Fatalf("/api/metrics: %d %s", code, b)
	}
	return m
}

type e2eEvent struct {
	Path    string         `json:"path"`
	Status  int            `json:"status"`
	Rules   map[string]int `json:"rules"`
	Blocked string         `json:"blocked"`
}

func (e *e2eEnv) events() ([]e2eEvent, []byte) {
	e.t.Helper()
	code, b := e.ui("GET", "/api/events", "")
	var evs []e2eEvent
	if code != 200 || json.Unmarshal(b, &evs) != nil {
		e.t.Fatalf("/api/events: %d %s", code, b)
	}
	return evs, b
}

// ---- fixtures ----

// turn1Request is the Codex request fixture with a user message carrying
// every seeded value appended.
func turn1Request(t *testing.T) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "internal", "adapter", "codex", "testdata", "codex", "request_turn1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var req map[string]any
	if err := json.Unmarshal(b, &req); err != nil {
		t.Fatal(err)
	}
	req["input"] = append(req["input"].([]any), map[string]any{
		"type": "message", "id": "msg_e2e_seed", "role": "user",
		"content": []any{map[string]any{"type": "input_text", "text": seededText}},
	})
	return req
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// renderStream fills the stream template: {{K}} is the synthetic for K,
// {{K|1}} and {{K|2}} its first and second half (so a value is split
// across two deltas).
func renderStream(tmpl string, vals map[string]string) string {
	for k, v := range vals {
		h := len(v) / 2
		tmpl = strings.ReplaceAll(tmpl, "{{"+k+"|1}}", v[:h])
		tmpl = strings.ReplaceAll(tmpl, "{{"+k+"|2}}", v[h:])
		tmpl = strings.ReplaceAll(tmpl, "{{"+k+"}}", v)
	}
	return tmpl
}

// writeStream sends an SSE document event by event, flushing each.
func writeStream(w http.ResponseWriter, doc string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	for _, ev := range strings.Split(doc, "\n\n") {
		if strings.TrimSpace(ev) == "" {
			continue
		}
		io.WriteString(w, ev+"\n\n")
		w.(http.Flusher).Flush()
	}
}

// turn1Responder replays stream_turn1.sse with the synthetics the upstream
// actually received.
func (e *e2eEnv) turn1Responder() func(http.ResponseWriter, *http.Request, capture) {
	tmpl, err := os.ReadFile(filepath.Join("testdata", "e2e", "stream_turn1.sse"))
	if err != nil {
		e.t.Fatal(err)
	}
	return func(w http.ResponseWriter, r *http.Request, c capture) {
		vals := map[string]string{}
		for k, real := range map[string]string{"IP": e2eIP, "OCID": e2eOCID, "HOST": e2eHost, "EMAIL": e2eEmail} {
			s, ok := e.synFor(real)
			if !ok {
				http.Error(w, "unknown value", http.StatusInternalServerError)
				return
			}
			vals[k] = s
		}
		writeStream(w, renderStream(string(tmpl), vals))
	}
}

// clientEvents parses the SSE the client received.
func clientEvents(t *testing.T, b []byte) []sse.Event {
	t.Helper()
	rd := sse.NewReader(bytes.NewReader(b))
	var out []sse.Event
	for {
		ev, err := rd.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatalf("client stream: %v", err)
		}
		out = append(out, ev)
	}
}

// joined concatenates field f of every event of type typ.
func joined(t *testing.T, evs []sse.Event, typ, f string) string {
	t.Helper()
	var b strings.Builder
	for _, ev := range evs {
		var m map[string]any
		if err := json.Unmarshal(ev.Data, &m); err != nil {
			t.Fatalf("client event is not JSON: %v", err)
		}
		if m["type"] == typ {
			s, _ := m[f].(string)
			b.WriteString(s)
		}
	}
	return b.String()
}

// completedOutput returns response.completed's response.output items.
func completedOutput(t *testing.T, evs []sse.Event) []any {
	t.Helper()
	for _, ev := range evs {
		var m struct {
			Type     string `json:"type"`
			Response struct {
				Output []any `json:"output"`
			} `json:"response"`
		}
		if json.Unmarshal(ev.Data, &m) == nil && m.Type == "response.completed" {
			return m.Response.Output
		}
	}
	t.Fatal("no response.completed event")
	return nil
}

var (
	wantArgs    = `{"cmd":"ssh ops@` + e2eIP + ` oci describe ` + e2eOCID + ` on ` + e2eHost + `"}`
	wantText    = "Host " + e2eHost + " is owned by " + e2eEmail + "."
	wantSummary = "**Checking " + e2eIP + "** reachability"
)

// runTurn1 sends the seeded turn-1 request and checks what the upstream
// received and what the client got back. It returns the request sent and
// the client's events.
func (e *e2eEnv) runTurn1() (map[string]any, []sse.Event) {
	t := e.t
	t.Helper()
	e.up.setRespond(e.turn1Responder())
	req := turn1Request(t)
	res, body := e.codexRequest(mustJSON(t, req), nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("turn 1: status %d: %s", res.StatusCode, body)
	}
	c := e.up.next(t)

	// The upstream saw none of the seeded values, anywhere.
	assertNoValues(t, "upstream request (URL, headers, body)", c.dump(), seededReals...)
	if c.Method != "POST" || c.URI != "/backend-api/codex/responses" {
		t.Errorf("upstream request line = %s %s", c.Method, c.URI)
	}
	if !json.Valid(c.Body) {
		t.Error("upstream body is not valid JSON")
	}
	for _, real := range []string{e2eIP, e2eIPTool, e2eIPMeta, e2eOCID, e2eFixtureEmail, e2eHost, e2eEmail, e2eSSN, e2eCard} {
		if !bytes.Contains(c.Body, []byte(e.mustSyn(real))) {
			t.Error("upstream body lacks the synthetic of a seeded value")
		}
	}
	// Credentials pass through untouched (they are needed upstream).
	if c.Header.Get("Authorization") != fakeAuth || c.Header.Get("Chatgpt-Account-Id") != fakeAcct {
		t.Error("credential headers were not forwarded untouched")
	}

	// The client got every streamed and final field restored.
	evs := clientEvents(t, body)
	if got := joined(t, evs, "response.function_call_arguments.delta", "delta"); got != wantArgs {
		t.Errorf("tool-call argument deltas = %q, want %q", got, wantArgs)
	}
	if got := joined(t, evs, "response.function_call_arguments.done", "arguments"); got != wantArgs {
		t.Errorf("tool-call arguments.done = %q", got)
	}
	if got := joined(t, evs, "response.output_text.delta", "delta"); got != wantText {
		t.Errorf("text deltas = %q, want %q", got, wantText)
	}
	if got := joined(t, evs, "response.reasoning_summary_text.delta", "delta"); got != wantSummary {
		t.Errorf("reasoning summary deltas = %q, want %q", got, wantSummary)
	}
	out := completedOutput(t, evs)
	if len(out) != 3 {
		t.Fatalf("completed output has %d items", len(out))
	}
	if s := out[0].(map[string]any)["summary"].([]any)[0].(map[string]any)["text"]; s != wantSummary {
		t.Errorf("completed reasoning summary = %q", s)
	}
	if a := out[1].(map[string]any)["arguments"]; a != wantArgs {
		t.Errorf("completed function_call arguments = %q", a)
	}
	if s := out[2].(map[string]any)["content"].([]any)[0].(map[string]any)["text"]; s != wantText {
		t.Errorf("completed message text = %q", s)
	}
	for _, real := range []string{e2eIP, e2eOCID, e2eHost, e2eEmail} {
		if bytes.Contains(body, []byte(e.mustSyn(real))) {
			t.Error("client response still holds a synthetic")
		}
	}
	return req, evs
}

// ---- tests ----

func TestFullTurnThroughRealListeners(t *testing.T) {
	e := newE2E(t)
	e.runTurn1()

	m := e.metrics()
	if m.Requests != 1 || m.Blocked != 0 || m.Errors != 0 {
		t.Errorf("metrics requests=%d blocked=%d errors=%d", m.Requests, m.Blocked, m.Errors)
	}
	for rule, min := range map[string]int64{
		"builtin.ipv4": 3, "builtin.ocid": 1, "builtin.email": 2, "builtin.ssn": 1,
		"builtin.card": 1, "builtin.secret": 2, "hosts": 1, "aws-keys": 1,
	} {
		if m.ReplacementsByRule[rule] < min {
			t.Errorf("replacements_by_rule[%s] = %d, want >= %d (all: %v)", rule, m.ReplacementsByRule[rule], min, m.ReplacementsByRule)
		}
	}
	if m.Restores == 0 {
		t.Error("metrics show no restores")
	}

	evs, raw := e.events()
	if len(evs) != 1 {
		t.Fatalf("audit events = %d", len(evs))
	}
	ev := evs[0]
	if ev.Path != "/v1/responses" || ev.Status != 200 || ev.Blocked != "" {
		t.Errorf("audit event = %+v", ev)
	}
	for rule := range m.ReplacementsByRule {
		if ev.Rules[rule] == 0 {
			t.Errorf("audit event lacks rule %s: %v", rule, ev.Rules)
		}
	}
	assertNoValues(t, "/api/events", string(raw), secretsEverywhere...)
}

func TestSecondTurnResendsHistoryWithoutDoubleSanitizing(t *testing.T) {
	e := newE2E(t)
	req, evs := e.runTurn1()
	before := map[string]string{}
	for _, real := range []string{e2eIP, e2eIPTool, e2eIPMeta, e2eOCID, e2eFixtureEmail, e2eSSN, e2eHost, e2eEmail, e2eCard} {
		before[real] = e.mustSyn(real)
	}
	size1 := e.metrics().VaultSize

	// Codex resends the whole history: the turn-1 input, the restored
	// output items it received, and the tool's output (which mentions
	// the same real values again).
	input := req["input"].([]any)
	input = append(input, completedOutput(t, evs)...)
	input = append(input, map[string]any{
		"type": "function_call_output", "call_id": "call_e2e_0001",
		"output": "PING " + e2eIP + ": ok\n" + e2eOCID + " RUNNING on " + e2eHost + ", owner " + e2eEmail,
	})
	req["input"] = input
	req["client_metadata"].(map[string]any)["turn_id"] = "00000000-0000-4000-8000-000000000003"

	e.up.setRespond(func(w http.ResponseWriter, r *http.Request, c capture) {
		writeStream(w, "event: response.completed\ndata: "+`{"type":"response.completed","response":{"id":"resp_e2e_0002","status":"completed","output":[]}}`+"\n\n")
	})
	res, body := e.codexRequest(mustJSON(t, req), nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("turn 2: status %d: %s", res.StatusCode, body)
	}
	c := e.up.next(t)
	assertNoValues(t, "turn-2 upstream request", c.dump(), seededReals...)
	for real, syn := range before {
		if got, _ := e.synFor(real); got != syn {
			t.Error("a seeded value got a different synthetic in turn 2")
		}
		if !bytes.Contains(c.Body, []byte(syn)) {
			t.Error("turn-2 upstream body lacks a turn-1 synthetic")
		}
	}
	// Nothing new was coined: the resent restored history maps to the same
	// synthetics, and no synthetic was treated as a real value again.
	if size2 := e.metrics().VaultSize; size2 != size1 {
		t.Errorf("vault_size %d after turn 1, %d after turn 2", size1, size2)
	}
	for _, s := range e.app.vault.Synthetics() {
		if r, _ := e.app.vault.Real(s); e.app.vault.IsSynthetic(r) {
			t.Error("a synthetic was sanitized again (double sanitizing)")
		}
	}
}

// feed reads the dashboard's /api/stream until it has seen n data events or
// waitLimit passes, then hangs up.
func (e *e2eEnv) feed(n int) <-chan []byte {
	ctx, cancel := context.WithTimeout(context.Background(), waitLimit)
	out := make(chan []byte, 1)
	ready := make(chan struct{})
	req, _ := http.NewRequestWithContext(ctx, "GET", e.uiURL+"/api/stream", nil)
	go func() {
		defer cancel()
		var buf bytes.Buffer
		defer func() { out <- buf.Bytes() }()
		res, err := http.DefaultClient.Do(req)
		close(ready)
		if err != nil {
			return
		}
		defer res.Body.Close()
		sc := bufio.NewScanner(res.Body)
		seen := 0
		for seen < n && sc.Scan() {
			buf.WriteString(sc.Text() + "\n")
			if strings.HasPrefix(sc.Text(), "data: ") {
				seen++
			}
		}
	}()
	<-ready
	return out
}

func TestNoRealValueInAnyOutput(t *testing.T) {
	e := newE2E(t)
	feed := e.feed(2)

	e.runTurn1()
	hdr := http.Header{"X-Debug-Target": {e2eIP}}
	if res, body := e.codexRequest(mustJSON(t, turn1Request(t)), hdr); res.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("header block: status %d", res.StatusCode)
	} else {
		assertNoValues(t, "proxy error body", string(body), secretsEverywhere...)
	}

	surfaces := map[string]string{}
	for _, p := range []string{"/api/config", "/api/metrics", "/api/events", "/healthz", "/"} {
		_, b := e.ui("GET", p, "")
		surfaces["GET "+p] = string(b)
	}
	_, b := e.ui("POST", "/api/test", string(mustJSON(t, map[string]string{"text": seededText})))
	surfaces["POST /api/test"] = string(b)
	code, stdout, stderr := runCLI(seededText, "test-rules", "-config", e.cfgPath)
	if code != 0 {
		t.Fatalf("test-rules exit %d: %s", code, stderr)
	}
	surfaces["test-rules stdout"], surfaces["test-rules stderr"] = stdout, stderr
	writeFile(t, e.cfgPath, e2eConfig(e.upstreamBase(), "hosts2.txt", ""))
	_, b = e.ui("POST", "/api/reload", "{}")
	surfaces["POST /api/reload"] = string(b)

	select {
	case f := <-feed:
		if n := strings.Count(string(f), "data: "); n < 2 {
			t.Errorf("feed delivered %d events, want 2", n)
		}
		surfaces["/api/stream"] = string(f)
	case <-time.After(waitLimit):
		t.Fatal("feed did not finish")
	}

	ctx, cancel := context.WithTimeout(context.Background(), waitLimit)
	defer cancel()
	if err := e.app.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	surfaces["logs"] = e.logs.String()
	if !strings.Contains(surfaces["logs"], "request blocked (reason=header)") {
		t.Errorf("log lacks the block line: %q", surfaces["logs"])
	}
	for where, s := range surfaces {
		assertNoValues(t, where, s, secretsEverywhere...)
	}
}

func TestRefusesNonLoopbackBind(t *testing.T) {
	up := httptest.NewServer(http.NotFoundHandler())
	defer up.Close()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "hosts.txt"), e2eHost+"\n")
	cfg := filepath.Join(dir, "config.yaml")

	for _, field := range []string{"listen: 127.0.0.1:0", "ui: { listen: 127.0.0.1:0 }"} {
		s := strings.Replace(e2eConfig(up.URL, "hosts.txt", ""), field, strings.Replace(field, "127.0.0.1", "0.0.0.0", 1), 1)
		writeFile(t, cfg, s)
		a, err := NewApp(AppOptions{ConfigPath: cfg, Logf: (&logBuf{}).logf})
		if err == nil {
			a.Close()
			t.Fatalf("%s on 0.0.0.0 accepted without allow_non_loopback", field)
		}
		if !strings.Contains(err.Error(), "not loopback") {
			t.Errorf("err = %v", err)
		}
	}

	// With the override the app starts and warns. The test still binds
	// loopback (the hook) so nothing is exposed while it runs.
	writeFile(t, cfg, strings.Replace(e2eConfig(up.URL, "hosts.txt", "allow_non_loopback: true"), "listen: 127.0.0.1:0", "listen: 0.0.0.0:0", 1))
	logs := &logBuf{}
	a, err := NewApp(AppOptions{ConfigPath: cfg, Logf: logs.logf})
	if err != nil {
		t.Fatalf("allow_non_loopback: %v", err)
	}
	defer a.Close()
	var asked []string
	a.listen = func(network, addr string) (net.Listener, error) {
		asked = append(asked, addr)
		return net.Listen(network, "127.0.0.1:0")
	}
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	if len(asked) == 0 || asked[0] != "0.0.0.0:0" {
		t.Errorf("listen addresses = %q", asked)
	}
	if !strings.Contains(logs.String(), "WARN allow_non_loopback=true") {
		t.Errorf("no startup warning: %q", logs.String())
	}
}

func TestHeaderBlockedEndToEnd(t *testing.T) {
	e := newE2E(t)
	e.up.setRespond(e.turn1Responder())
	res, body := e.codexRequest(mustJSON(t, turn1Request(t)), http.Header{"X-Debug-Target": {"ping " + e2eIP}})
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status %d", res.StatusCode)
	}
	var apiErr struct{ Error struct{ Code string } }
	if json.Unmarshal(body, &apiErr) != nil || apiErr.Error.Code != "header" {
		t.Errorf("body %s", body)
	}
	assertNoValues(t, "error body", string(body), secretsEverywhere...)
	if n := e.up.count(); n != 0 {
		t.Fatalf("upstream received %d requests", n)
	}
	if m := e.metrics(); m.BlockedByReason["header"] != 1 {
		t.Errorf("blocked_by_reason = %v", m.BlockedByReason)
	}
	if n := e.up.count(); n != 0 {
		t.Fatalf("upstream received %d requests", n)
	}
}

func TestRedirectNeverRelayedEndToEnd(t *testing.T) {
	e := newE2E(t)
	e.up.setRespond(func(w http.ResponseWriter, r *http.Request, c capture) {
		w.Header().Set("Location", "https://exfil.example/collect?from=e2e")
		w.WriteHeader(http.StatusTemporaryRedirect)
		io.WriteString(w, `<a href="https://exfil.example/collect">moved</a>`)
	})
	res, body := e.codexRequest(mustJSON(t, turn1Request(t)), nil)
	if res.StatusCode != http.StatusBadGateway {
		t.Fatalf("status %d", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); loc != "" {
		t.Errorf("Location relayed: %q", loc)
	}
	if bytes.Contains(body, []byte("exfil.example")) {
		t.Errorf("redirect body relayed: %s", body)
	}
	var apiErr struct{ Error struct{ Code string } }
	if json.Unmarshal(body, &apiErr) != nil || apiErr.Error.Code != "upstream_redirect" {
		t.Errorf("body %s", body)
	}
	c := e.up.next(t)
	assertNoValues(t, "upstream request", c.dump(), seededReals...)
	if n := e.up.count(); n != 1 {
		t.Errorf("upstream received %d requests (redirect followed?)", n)
	}
}

func TestReloadMidSessionEndToEnd(t *testing.T) {
	e := newE2E(t)
	gate := make(chan struct{})
	// The upstream echoes the synthetic it got for e2eHost, after the gate.
	e.up.setRespond(func(w http.ResponseWriter, r *http.Request, c capture) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		writeEvent(w, "response.created", `{"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`)
		select {
		case <-gate:
		case <-r.Context().Done():
			return
		}
		syn, _ := e.synFor(e2eHost)
		writeEvent(w, "response.output_text.delta", textDelta("Host "+syn))
		writeEvent(w, "response.completed", `{"type":"response.completed","response":{"id":"resp_1","status":"completed"}}`)
	})

	type result struct {
		status int
		text   string
	}
	done := make(chan result, 1)
	go func() {
		res, err := e.client.Post(e.pxURL+"/v1/responses", "application/json", strings.NewReader(userBody("deploy to "+e2eHost)))
		if err != nil {
			done <- result{-1, err.Error()}
			return
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		var out strings.Builder
		rd := sse.NewReader(bytes.NewReader(b))
		for {
			ev, err := rd.Next()
			if err != nil {
				break
			}
			var d struct{ Type, Delta string }
			if json.Unmarshal(ev.Data, &d) == nil && d.Type == "response.output_text.delta" {
				out.WriteString(d.Delta)
			}
		}
		done <- result{res.StatusCode, out.String()}
	}()
	c := e.up.next(t)
	assertNoValues(t, "in-flight upstream request", c.dump(), e2eHost)
	synHost := e.mustSyn(e2eHost)

	// Mid-stream: swap the dictionary (hosts -> hosts2) through the dashboard.
	writeFile(t, e.cfgPath, e2eConfig(e.upstreamBase(), "hosts2.txt", ""))
	if code, b := e.ui("POST", "/api/reload", "{}"); code != 200 {
		t.Fatalf("reload: %d %s", code, b)
	}
	close(gate)
	select {
	case r := <-done:
		if r.status != 200 || r.text != "Host "+e2eHost {
			t.Fatalf("in-flight stream after reload: status %d, text %q", r.status, r.text)
		}
	case <-time.After(waitLimit):
		t.Fatal("in-flight request did not finish")
	}

	// The next request (the gate is open now) runs with the new rules: hosts2 is sanitized, the old
	// dictionary entry no longer is. The old mapping is kept: the response
	// naming its synthetic is still restored.
	sizeBefore := e.metrics().VaultSize
	_, got := ask(t, strings.TrimPrefix(e.pxURL, "http://"), "move "+e2eHost2+" next to "+e2eHost)
	c = e.up.next(t)
	assertNoValues(t, "post-reload upstream request", c.dump(), e2eHost2)
	if !bytes.Contains(c.Body, []byte(e2eHost)) {
		t.Error("post-reload request still sanitized the removed dictionary entry")
	}
	if got != "Host "+e2eHost {
		t.Errorf("old mapping lost after reload: %q", got)
	}
	if s, _ := e.synFor(e2eHost); s != synHost {
		t.Error("old mapping changed after reload")
	}
	if m := e.metrics(); m.VaultSize != sizeBefore+1 {
		t.Errorf("vault_size %d -> %d, want one new mapping", sizeBefore, m.VaultSize)
	}
}

func TestDashboardServesAndRejectsForeignHost(t *testing.T) {
	e := newE2E(t)
	code, b := e.ui("GET", "/", "")
	if code != 200 || !bytes.Contains(bytes.ToLower(b), []byte("<html")) {
		t.Fatalf("GET / = %d", code)
	}
	for _, host := range []string{"attacker.example", "attacker.example:" + strings.Split(e.uiURL, ":")[2], "127.0.0.1.attacker.example"} {
		for _, p := range []string{"/", "/api/metrics", "/api/events"} {
			req, _ := http.NewRequest("GET", e.uiURL+p, nil)
			req.Host = host
			res, err := e.client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			res.Body.Close()
			if res.StatusCode != http.StatusMisdirectedRequest {
				t.Errorf("Host %q %s: status %d, want 421", host, p, res.StatusCode)
			}
		}
	}
}
