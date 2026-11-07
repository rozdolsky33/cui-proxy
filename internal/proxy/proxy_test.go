package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cui-proxy/internal/adapter"
	"cui-proxy/internal/adapter/codex"
	"cui-proxy/internal/audit"
	"cui-proxy/internal/config"
	"cui-proxy/internal/detect"
	"cui-proxy/internal/metrics"
	"cui-proxy/internal/sse"
	"cui-proxy/internal/transform"
	"cui-proxy/internal/vault"
)

// Every wait in these tests is bounded by this, only so that a broken proxy
// fails instead of hanging; synchronization itself uses channels.
const waitLimit = 10 * time.Second

var testKey = []byte("0123456789abcdef0123456789abcdef")

// Real values seeded into requests. Only these literals are asserted on.
const (
	realIP    = "10.20.30.15"
	realOCID  = "ocid1.instance.oc1.phx.anyhqljrabcdefghijklmnop"
	realEmail = "alice.smith@corp-internal.com"
	authToken = "Bearer tok-SECRET-0123456789abcdefXYZ"
	accountID = "acct-SECRET-42"
)

// ---- environment ----

type set struct {
	v *vault.Vault
	s *transform.Sanitizer
	r *transform.Restorer
}

func newSet(t testing.TB) *set {
	t.Helper()
	ds, err := detect.Build([]config.Rule{{Name: "builtin", Type: "builtin"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	v := vault.New(testKey)
	return &set{v: v, s: transform.NewSanitizer(ds, v, nil), r: transform.NewRestorer(v)}
}

func (s *set) pseudo(t testing.TB, class, real string) string {
	t.Helper()
	syn, err := s.v.Pseudonym(class, real)
	if err != nil {
		t.Fatal(err)
	}
	return syn
}

// capture is what the fake upstream received.
type capture struct {
	Method, URI, Host string
	Header            http.Header
	ContentLength     int64
	Body              []byte
}

// dump renders every received byte the upstream could see.
func (c capture) dump() string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "%s %s\nHost: %s\n", c.Method, c.URI, c.Host)
	_ = c.Header.Write(&b)
	b.Write(c.Body)
	return b.String()
}

type logBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *logBuf) logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(&l.b, format+"\n", args...)
}

func (l *logBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

type harness struct {
	t         *testing.T
	cfg       *config.Config
	cur       atomic.Pointer[set]
	snapCalls atomic.Int64
	reg       *metrics.Registry
	aud       *audit.Log
	auditCh   <-chan audit.Entry
	logs      *logBuf
	reqs      chan capture
	handle    func(w http.ResponseWriter, r *http.Request)
	up, px    *httptest.Server
	upTr      *http.Transport // proxy -> upstream
	cliTr     *http.Transport // test client -> proxy
	client    *http.Client
}

type option func(*harness, *Deps)

func withAdapter(a adapter.Adapter) option { return func(_ *harness, d *Deps) { d.Adapter = a } }

func newHarness(t *testing.T, handle func(w http.ResponseWriter, r *http.Request), opts ...option) *harness {
	t.Helper()
	h := &harness{
		t:      t,
		cfg:    &config.Config{Policy: config.Policy{OnError: "block", MaxBodyBytes: 1 << 20}},
		reg:    metrics.New(),
		aud:    audit.New(100),
		logs:   &logBuf{},
		reqs:   make(chan capture, 16),
		handle: handle,
	}
	h.cur.Store(newSet(t))
	ch, cancel := h.aud.Subscribe()
	h.auditCh = ch
	h.up = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		h.reqs <- capture{r.Method, r.RequestURI, r.Host, r.Header.Clone(), r.ContentLength, body}
		if h.handle != nil {
			h.handle(w, r)
		} else {
			defaultStream(w)
		}
	}))
	base, err := url.Parse(h.up.URL + "/backend-api/codex")
	if err != nil {
		t.Fatal(err)
	}
	h.upTr = &http.Transport{}
	d := Deps{
		Adapter:  codex.New(),
		Upstream: base,
		Snapshot: func() Snapshot {
			h.snapCalls.Add(1)
			s := h.cur.Load()
			return Snapshot{Cfg: h.cfg, Sanitizer: s.s, Restorer: s.r}
		},
		VaultLen:  func() int { return h.cur.Load().v.Len() },
		Metrics:   h.reg,
		Audit:     h.aud,
		Transport: h.upTr,
		Logf:      h.logs.logf,
	}
	for _, o := range opts {
		o(h, &d)
	}
	h.px = httptest.NewServer(New(d))
	h.cliTr = &http.Transport{DisableCompression: true}
	h.client = &http.Client{Transport: h.cliTr, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse // observe what the proxy relays
	}}
	t.Cleanup(func() { h.close(); cancel() })
	return h
}

// close shuts down both servers and every idle connection (idempotent).
func (h *harness) close() {
	h.cliTr.CloseIdleConnections()
	h.px.Close()
	h.upTr.CloseIdleConnections()
	h.up.Close()
}

func (h *harness) set() *set { return h.cur.Load() }

func (h *harness) do(req *http.Request) (*http.Response, []byte) {
	h.t.Helper()
	res, err := h.client.Do(req)
	if err != nil {
		h.t.Fatalf("request: %v", err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		h.t.Fatalf("read response: %v", err)
	}
	return res, b
}

func (h *harness) req(method, path, body string) *http.Request {
	h.t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, h.px.URL+path, rd)
	if err != nil {
		h.t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "text/event-stream")
	return req
}

func (h *harness) post(body string) (*http.Response, []byte) {
	h.t.Helper()
	return h.do(h.req("POST", "/v1/responses", body))
}

func (h *harness) upstreamGot() capture {
	h.t.Helper()
	select {
	case c := <-h.reqs:
		return c
	case <-time.After(waitLimit):
		h.t.Fatal("upstream received no request")
	}
	return capture{}
}

func (h *harness) noUpstreamCall() {
	h.t.Helper()
	select {
	case c := <-h.reqs:
		h.t.Fatalf("upstream was called: %s %s", c.Method, c.URI)
	default:
	}
}

// nextAudit waits for the next audit entry (added after the response ends).
func (h *harness) nextAudit() audit.Entry {
	h.t.Helper()
	select {
	case e := <-h.auditCh:
		return e
	case <-time.After(waitLimit):
		h.t.Fatal("no audit entry")
	}
	return audit.Entry{}
}

func writeEvent(w http.ResponseWriter, name, data string) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, data)
	w.(http.Flusher).Flush()
}

func sseHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
}

const (
	evCreated   = `{"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`
	evCompleted = `{"type":"response.completed","response":{"id":"resp_1","status":"completed"}}`
)

func defaultStream(w http.ResponseWriter) {
	sseHeaders(w)
	writeEvent(w, "response.created", evCreated)
	writeEvent(w, "response.completed", evCompleted)
}

func textDelta(d string) string {
	b, _ := json.Marshal(d)
	return `{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":` + string(b) + `}`
}

func userBody(text string) string {
	b, _ := json.Marshal(text)
	return `{"model":"gpt-5-codex","stream":true,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":` + string(b) + `}]}]}`
}

func readEvents(t *testing.T, b []byte) []sse.Event {
	t.Helper()
	rd := sse.NewReader(bytes.NewReader(b))
	var out []sse.Event
	for {
		e, err := rd.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
}

func deltas(t *testing.T, evs []sse.Event, typ string) string {
	t.Helper()
	var b strings.Builder
	for _, e := range evs {
		var m struct{ Type, Delta string }
		if json.Unmarshal(e.Data, &m) == nil && m.Type == typ {
			b.WriteString(m.Delta)
		}
	}
	return b.String()
}

type apiError struct {
	Error struct{ Message, Type, Code string }
}

func assertBlocked(t *testing.T, h *harness, res *http.Response, body []byte, status int, code string) {
	t.Helper()
	if res.StatusCode != status {
		t.Errorf("status = %d, want %d", res.StatusCode, status)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
	var e apiError
	if err := json.Unmarshal(body, &e); err != nil {
		t.Fatalf("error body is not JSON: %q", body)
	}
	if e.Error.Code != code || e.Error.Type != "cui_proxy_blocked" || !strings.HasPrefix(e.Error.Message, "cui-proxy: ") {
		t.Errorf("error = %+v, want code %q", e.Error, code)
	}
	if got := h.reg.Snapshot().BlockedByReason[code]; got != 1 {
		t.Errorf("blocked_by_reason[%s] = %d, want 1", code, got)
	}
	a := h.nextAudit()
	if a.Blocked != code || a.Status != status {
		t.Errorf("audit = %+v, want blocked %q status %d", a, code, status)
	}
	h.noUpstreamCall()
}

// ---- the plan's tests ----

func TestUpstreamNeverSeesRealValue(t *testing.T) {
	h := newHarness(t, nil)
	body, err := os.ReadFile("../adapter/codex/testdata/codex/request_turn1.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, real := range []string{realIP, realOCID, realEmail} {
		if !bytes.Contains(body, []byte(real)) {
			t.Fatalf("fixture lacks %q", real)
		}
	}
	res, _ := h.post(string(body))
	if res.StatusCode != 200 {
		t.Fatalf("status = %d", res.StatusCode)
	}
	got := h.upstreamGot()
	if got.URI != "/backend-api/codex/responses" || got.Method != "POST" {
		t.Errorf("upstream request = %s %s", got.Method, got.URI)
	}
	dump := got.dump()
	for _, real := range []string{realIP, realOCID, realEmail, "corp-internal.com"} {
		if strings.Contains(dump, real) {
			t.Errorf("upstream saw real value %q", real)
		}
	}
	if !json.Valid(got.Body) {
		t.Error("upstream body is not valid JSON")
	}
	a := h.nextAudit()
	if a.Rules["builtin.ipv4"] == 0 || a.Rules["builtin.ocid"] == 0 || a.Rules["builtin.email"] == 0 {
		t.Errorf("audit rules = %v", a.Rules)
	}
	if a.Adapter != "codex" || a.Method != "POST" || a.Path != "/v1/responses" || a.Status != 200 || a.Blocked != "" {
		t.Errorf("audit = %+v", a)
	}
	if a.BytesIn != len(body) || a.BytesOut == 0 {
		t.Errorf("audit bytes in/out = %d/%d", a.BytesIn, a.BytesOut)
	}
	s := h.reg.Snapshot()
	if s.Requests != 1 || s.Replacements == 0 || s.OverheadMS.Samples != 1 || s.VaultSize == 0 {
		t.Errorf("metrics = %+v", s)
	}
}

func TestClientReceivesRestoredStream(t *testing.T) {
	var h *harness
	h = newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		syn := h.set().pseudo(t, "ipv4", realIP)
		fx, _ := os.ReadFile("../adapter/codex/testdata/codex/stream_toolcall.sse")
		raw := strings.NewReplacer("{{A1}}", syn[:5], "{{A2}}", syn[5:], "{{IP}}", syn).Replace(string(fx))
		w.Header().Set("Content-Length", fmt.Sprint(len(raw)))
		sseHeaders(w)
		io.WriteString(w, raw)
	})
	res, body := h.post(userBody("hi"))
	if res.StatusCode != 200 {
		t.Fatalf("status = %d", res.StatusCode)
	}
	if res.Header.Get("Content-Length") != "" || res.ContentLength != -1 {
		t.Errorf("Content-Length kept on rewritten SSE: %q", res.Header.Get("Content-Length"))
	}
	evs := readEvents(t, body)
	want := `{"cmd":"echo host=` + realIP + ` done"}`
	if got := deltas(t, evs, "response.function_call_arguments.delta"); got != want {
		t.Errorf("deltas = %q, want %q", got, want)
	}
	syn := h.set().pseudo(t, "ipv4", realIP)
	if bytes.Contains(body, []byte(syn)) {
		t.Errorf("synthetic %q reached the client", syn)
	}
	if n := len(evs); n < 6 || evs[n-1].Name != "response.completed" {
		t.Errorf("events: %d, last %q", n, evs[n-1].Name)
	}
	a := h.nextAudit()
	if a.BytesOut != len(body) {
		t.Errorf("audit BytesOut = %d, want %d", a.BytesOut, len(body))
	}
	if h.reg.Snapshot().Restores == 0 {
		t.Error("no restores counted")
	}
}

func TestAuthHeadersForwardedAndNotLogged(t *testing.T) {
	var std bytes.Buffer
	var stdMu sync.Mutex
	log.SetOutput(lockedWriter{&stdMu, &std})
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	h := newHarness(t, nil)
	sent := http.Header{
		"Authorization":         {authToken},
		"Chatgpt-Account-Id":    {accountID},
		"Session-Id":            {"sess-SECRET-1"},
		"Thread-Id":             {"thr-SECRET-2"},
		"X-Codex-Turn-Metadata": {`{"turn_id":"t-SECRET-3","sandbox":"workspace-write"}`},
		"X-Codex-Beta-Features": {"feat-a,feat-b"},
		"Originator":            {"codex_exec"},
		"User-Agent":            {"codex_exec/0.166.0 (Mac OS 26.5; arm64)"},
		"Cookie":                {"session=cookie-SECRET-4"},
	}
	secrets := []string{"tok-SECRET", accountID, "sess-SECRET", "thr-SECRET", "t-SECRET-3", "cookie-SECRET"}

	req := h.req("POST", "/v1/responses?trace=q-SECRET-5", userBody("ping "+realIP))
	for k, v := range sent {
		req.Header[k] = v
	}
	h.do(req)
	got := h.upstreamGot()
	for k, v := range sent {
		if g := got.Header[k]; len(g) != 1 || g[0] != v[0] {
			t.Errorf("header %s = %q, want %q", k, g, v)
		}
	}
	if got.Header.Get("Content-Type") != "application/json" || got.Header.Get("Accept") != "text/event-stream" {
		t.Errorf("content headers not forwarded: %v", got.Header)
	}
	for _, k := range []string{"X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "Forwarded"} {
		if got.Header.Get(k) != "" {
			t.Errorf("proxy added %s", k)
		}
	}
	if got.URI != "/backend-api/codex/responses?trace=q-SECRET-5" {
		t.Errorf("query not preserved: %q", got.URI)
	}
	entries := []audit.Entry{h.nextAudit()}

	// A blocked request, a sanitize failure and an upstream error with the
	// same headers: none of them may log or audit header values.
	for _, rq := range []*http.Request{
		h.req("POST", "/v1/files?x=q-SECRET-5", "x"),
		h.req("POST", "/v1/responses?x=q-SECRET-5", "not json "+realIP),
	} {
		for k, v := range sent {
			rq.Header[k] = v
		}
		h.do(rq)
		entries = append(entries, h.nextAudit())
	}
	h.up.Close() // upstream now unreachable
	rq := h.req("POST", "/v1/responses?x=q-SECRET-5", userBody("hi"))
	for k, v := range sent {
		rq.Header[k] = v
	}
	if res, _ := h.do(rq); res.StatusCode != http.StatusBadGateway {
		t.Errorf("unreachable upstream: status %d", res.StatusCode)
	}
	entries = append(entries, h.nextAudit())

	stdMu.Lock()
	all := h.logs.String() + std.String() + fmt.Sprintf("%+v", entries)
	stdMu.Unlock()
	for _, s := range append(secrets, "q-SECRET-5", realIP, "trace=") {
		if strings.Contains(all, s) {
			t.Errorf("logs or audit contain %q:\n%s", s, all)
		}
	}
	if h.logs.String() == "" {
		t.Error("expected some log output from the failing requests")
	}
}

type lockedWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (l lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func TestUnknownRouteBlocked(t *testing.T) {
	for _, tc := range []struct{ method, path string }{
		{"POST", "/v1/responses/compact"},
		{"GET", "/v1/responses"},
		{"POST", "/v1/files"},
		{"DELETE", "/v1/responses"},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			h := newHarness(t, nil)
			res, body := h.do(h.req(tc.method, tc.path+"?q=1", userBody(realIP)))
			assertBlocked(t, h, res, body, http.StatusForbidden, "route")
			if strings.Contains(string(body), realIP) {
				t.Error("error body echoes request data")
			}
		})
	}
}

func TestOversizeBodyBlocked(t *testing.T) {
	h := newHarness(t, nil)
	h.cfg.Policy.MaxBodyBytes = 200
	res, body := h.post(userBody(strings.Repeat("a", 300)))
	assertBlocked(t, h, res, body, http.StatusRequestEntityTooLarge, "body_too_large")

	// Exactly at the limit is accepted.
	exact := userBody(strings.Repeat("a", 200-len(userBody(""))))
	if len(exact) != 200 {
		t.Fatalf("test body is %d bytes", len(exact))
	}
	if res, _ := h.post(exact); res.StatusCode != 200 {
		t.Errorf("body at limit: status %d", res.StatusCode)
	}
}

func TestCompressedRequestBlocked(t *testing.T) {
	for _, enc := range []string{"gzip", "br", "zstd", "identity, gzip", "deflate"} {
		t.Run(enc, func(t *testing.T) {
			h := newHarness(t, nil)
			req := h.req("POST", "/v1/responses", userBody(realIP))
			req.Header.Set("Content-Encoding", enc)
			res, body := h.do(req)
			assertBlocked(t, h, res, body, http.StatusUnsupportedMediaType, "compressed")
		})
	}
	t.Run("identity", func(t *testing.T) {
		h := newHarness(t, nil)
		req := h.req("POST", "/v1/responses", userBody(realIP))
		req.Header.Set("Content-Encoding", "identity")
		if res, _ := h.do(req); res.StatusCode != 200 {
			t.Errorf("identity: status %d", res.StatusCode)
		}
		if got := h.upstreamGot(); strings.Contains(got.dump(), realIP) {
			t.Error("real value forwarded")
		}
	})
}

func TestModelsPassthrough(t *testing.T) {
	const models = `{"models":[{"slug":"gpt-5-codex","note":"10.1.2.3 stays as is"}]}`
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Etag", `"abc"`)
		io.WriteString(w, models)
	})
	res, body := h.do(h.req("GET", "/v1/models?client_version=0.166.0", ""))
	if res.StatusCode != 200 || string(body) != models {
		t.Errorf("got %d %q", res.StatusCode, body)
	}
	if res.Header.Get("Etag") != `"abc"` || res.Header.Get("Content-Type") != "application/json" {
		t.Errorf("headers not passed: %v", res.Header)
	}
	got := h.upstreamGot()
	if got.Method != "GET" || got.URI != "/backend-api/codex/models?client_version=0.166.0" {
		t.Errorf("upstream got %s %s", got.Method, got.URI)
	}
	if len(got.Body) != 0 || got.ContentLength != 0 {
		t.Errorf("GET forwarded with a body")
	}
	a := h.nextAudit()
	if a.Path != "/v1/models" || strings.Contains(fmt.Sprintf("%+v", a), "client_version") {
		t.Errorf("audit path = %q (%+v)", a.Path, a)
	}
	if a.Blocked != "" || a.Status != 200 || a.BytesOut != len(models) {
		t.Errorf("audit = %+v", a)
	}
	if s := h.reg.Snapshot(); s.OverheadMS.Samples != 0 {
		t.Errorf("passthrough route observed overhead: %+v", s.OverheadMS)
	}
}

// A passthrough route is never scanned, so it must not carry a body.
func TestPassthroughRouteWithBodyBlocked(t *testing.T) {
	h := newHarness(t, nil)
	res, body := h.do(h.req("GET", "/v1/models", "ping "+realIP))
	assertBlocked(t, h, res, body, http.StatusBadRequest, "bad_request")
}

func TestUpstream500PassedThrough(t *testing.T) {
	for _, tc := range []struct {
		status int
		ct     string
		body   string
	}{
		{500, "application/json", `{"error":{"message":"boom 192.0.2.1"}}`},
		{429, "application/json", `{"error":{"message":"slow down"}}`},
		{401, "text/plain", "unauthorized"},
		{503, "text/html", "<html>down</html>"},
	} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.ct)
				w.Header().Set("Retry-After", "7")
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			})
			res, body := h.post(userBody("hi"))
			if res.StatusCode != tc.status || string(body) != tc.body {
				t.Errorf("got %d %q", res.StatusCode, body)
			}
			if res.Header.Get("Retry-After") != "7" || res.Header.Get("Content-Type") != tc.ct {
				t.Errorf("headers: %v", res.Header)
			}
			a := h.nextAudit()
			if a.Status != tc.status || a.Blocked != "" {
				t.Errorf("audit = %+v", a)
			}
			if s := h.reg.Snapshot(); s.Blocked != 0 {
				t.Errorf("upstream error counted as blocked")
			}
		})
	}
}

func TestClientDisconnectCancelsUpstream(t *testing.T) {
	base := runtime.NumGoroutine()
	upCancelled := make(chan struct{})
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		sseHeaders(w)
		writeEvent(w, "response.created", evCreated)
		select {
		case <-r.Context().Done():
			close(upCancelled)
		case <-time.After(waitLimit):
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	res, err := h.client.Do(h.req("POST", "/v1/responses", userBody("hi")).WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sse.NewReader(res.Body).Next(); err != nil {
		t.Fatalf("first event: %v", err)
	}
	cancel()
	res.Body.Close()
	select {
	case <-upCancelled:
	case <-time.After(waitLimit):
		t.Fatal("upstream request was not cancelled")
	}
	h.nextAudit()
	if s := h.reg.Snapshot(); s.Errors != 0 {
		t.Errorf("client disconnect counted as error: %d", s.Errors)
	}
	h.close()
	assertGoroutines(t, base)
}

func TestGoroutinesReturnToBaselineAfterCompletion(t *testing.T) {
	base := runtime.NumGoroutine()
	h := newHarness(t, nil)
	for i := 0; i < 5; i++ {
		if res, _ := h.post(userBody("hi " + realIP)); res.StatusCode != 200 {
			t.Fatalf("status %d", res.StatusCode)
		}
		h.nextAudit()
	}
	h.close()
	assertGoroutines(t, base)
}

// assertGoroutines polls (there is nothing to wait on: exiting goroutines
// signal nobody) until the count is back to base, failing after 1 s.
func assertGoroutines(t *testing.T, base int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		n := runtime.NumGoroutine()
		if n <= base {
			return
		}
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<16)
			t.Fatalf("goroutines: %d, baseline %d\n%s", n, base, buf[:runtime.Stack(buf, true)])
		}
		runtime.Gosched()
		time.Sleep(5 * time.Millisecond)
	}
}

func TestFailClosedOnSanitizerError(t *testing.T) {
	t.Run("sanitize", func(t *testing.T) {
		h := newHarness(t, nil)
		res, body := h.post("not json " + realIP)
		assertBlocked(t, h, res, body, http.StatusUnprocessableEntity, "sanitize")
		if strings.Contains(string(body), realIP) {
			t.Error("error echoes request data")
		}
	})
	t.Run("unrestorable", func(t *testing.T) {
		fa := &fakeAdapter{Adapter: codex.New(), sanitize: func([]byte, *transform.Sanitizer) ([]byte, error) {
			return nil, fmt.Errorf("x: %w: rule=builtin.ipv4 class=ipv4", transform.ErrUnrestorable)
		}}
		h := newHarness(t, nil, withAdapter(fa))
		res, body := h.post(userBody(realIP))
		assertBlocked(t, h, res, body, http.StatusUnprocessableEntity, "unrestorable")
	})
}

func TestPassthroughModeForwardsOriginal(t *testing.T) {
	h := newHarness(t, nil)
	h.cfg.Policy.OnError = "passthrough"
	orig := "not json " + realIP
	res, _ := h.post(orig)
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	if got := h.upstreamGot(); string(got.Body) != orig || got.ContentLength != int64(len(orig)) {
		t.Errorf("upstream body = %q", got.Body)
	}
	a := h.nextAudit()
	if a.Blocked != "" || a.Rules["passthrough:sanitize"] != 1 {
		t.Errorf("audit = %+v", a)
	}
	s := h.reg.Snapshot()
	if s.Passthrough != 1 || s.Blocked != 0 {
		t.Errorf("metrics passthrough=%d blocked=%d", s.Passthrough, s.Blocked)
	}

	// Passthrough mode never relaxes route, size or compression checks.
	h.cfg.Policy.MaxBodyBytes = 10
	if res, _ := h.post(orig); res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("oversize in passthrough mode: %d", res.StatusCode)
	}
	h.cfg.Policy.MaxBodyBytes = 1 << 20
	if res, _ := h.do(h.req("POST", "/v1/other", orig)); res.StatusCode != http.StatusForbidden {
		t.Errorf("route in passthrough mode: %d", res.StatusCode)
	}
	req := h.req("POST", "/v1/responses", orig)
	req.Header.Set("Content-Encoding", "gzip")
	if res, _ := h.do(req); res.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("compressed in passthrough mode: %d", res.StatusCode)
	}
	h.noUpstreamCall()
}

func TestStreamFlushedPerEvent(t *testing.T) {
	release := make(chan struct{})
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		sseHeaders(w)
		writeEvent(w, "response.created", evCreated)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		case <-time.After(waitLimit):
			return
		}
		writeEvent(w, "response.completed", evCompleted)
	})
	ctx, cancel := context.WithTimeout(context.Background(), waitLimit)
	defer cancel()
	res, err := h.client.Do(h.req("POST", "/v1/responses", userBody("hi")).WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	rd := sse.NewReader(res.Body)
	e, err := rd.Next()
	if err != nil || e.Name != "response.created" {
		t.Fatalf("first event before upstream finished: %q %v", e.Name, err)
	}
	close(release)
	e, err = rd.Next()
	if err != nil || e.Name != "response.completed" {
		t.Fatalf("second event: %q %v", e.Name, err)
	}
}

// ---- extra tests (rulings P3-P11) ----

func TestAcceptEncodingIdentityUpstream(t *testing.T) {
	h := newHarness(t, nil)
	req := h.req("POST", "/v1/responses", userBody("hi"))
	req.Header.Set("Accept-Encoding", "gzip, br, zstd")
	h.do(req)
	if got := h.upstreamGot().Header["Accept-Encoding"]; len(got) != 1 || got[0] != "identity" {
		t.Errorf("Accept-Encoding upstream = %q", got)
	}
	h.do(h.req("GET", "/v1/models", ""))
	if got := h.upstreamGot().Header["Accept-Encoding"]; len(got) != 1 || got[0] != "identity" {
		t.Errorf("Accept-Encoding upstream (models) = %q", got)
	}
}

func TestContentLengthRecomputed(t *testing.T) {
	h := newHarness(t, nil)
	orig := userBody("look at " + realOCID)
	h.post(orig)
	got := h.upstreamGot()
	if string(got.Body) == orig {
		t.Fatal("body was not sanitized")
	}
	if len(got.Body) == len(orig) {
		t.Fatal("test needs a length-changing replacement")
	}
	if got.ContentLength != int64(len(got.Body)) || got.Header.Get("Content-Length") != "" && got.Header.Get("Content-Length") != fmt.Sprint(len(got.Body)) {
		t.Errorf("Content-Length %d (%q), body %d", got.ContentLength, got.Header.Get("Content-Length"), len(got.Body))
	}
	if len(got.Header["Transfer-Encoding"]) != 0 {
		t.Errorf("chunked upstream request: %v", got.Header["Transfer-Encoding"])
	}
}

func TestHopByHopNotForwarded(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Connection", "X-Resp-Hop")
		w.Header().Set("X-Resp-Hop", "1")
		w.Header().Set("Keep-Alive", "timeout=5")
		w.Header().Set("Proxy-Authenticate", "Basic")
		w.Header().Set("X-Kept", "yes")
		defaultStream(w)
	})
	req := h.req("POST", "/v1/responses", userBody("hi"))
	req.Header.Set("Connection", "keep-alive, X-Custom-Hop")
	req.Header.Set("X-Custom-Hop", "1")
	req.Header.Set("Keep-Alive", "timeout=5")
	req.Header.Set("Proxy-Authorization", "Basic cHJveHk6c2VjcmV0")
	req.Header.Set("Proxy-Connection", "keep-alive")
	req.Header.Set("Te", "trailers")
	req.Header.Set("Trailer", "X-T")
	req.Header.Set("Upgrade", "h2c")
	req.Header.Set("X-Kept", "yes")
	res, _ := h.do(req)
	got := h.upstreamGot()
	for _, k := range []string{"Connection", "X-Custom-Hop", "Keep-Alive", "Proxy-Authorization", "Proxy-Connection", "Te", "Trailer", "Upgrade", "Transfer-Encoding"} {
		if v := got.Header.Values(k); len(v) != 0 {
			t.Errorf("upstream got hop-by-hop %s: %q", k, v)
		}
	}
	if got.Header.Get("X-Kept") != "yes" {
		t.Error("end-to-end header dropped")
	}
	for _, k := range []string{"X-Resp-Hop", "Keep-Alive", "Proxy-Authenticate"} {
		if v := res.Header.Values(k); len(v) != 0 {
			t.Errorf("client got hop-by-hop %s: %q", k, v)
		}
	}
	if res.Header.Get("X-Kept") != "yes" {
		t.Error("end-to-end response header dropped")
	}
}

func TestPanicReturns500NoUpstream(t *testing.T) {
	fa := &fakeAdapter{Adapter: codex.New(), sanitize: func(b []byte, _ *transform.Sanitizer) ([]byte, error) {
		panic(fmt.Sprintf("bad value %s", b))
	}}
	h := newHarness(t, nil, withAdapter(fa))
	res, body := h.post(userBody(realIP))
	assertBlocked(t, h, res, body, http.StatusInternalServerError, "internal")
	if l := h.logs.String(); strings.Contains(l, realIP) || strings.Contains(l, "bad value") || !strings.Contains(l, "panic") {
		t.Errorf("panic log = %q", l)
	}
	// The proxy keeps serving.
	h2 := newHarness(t, nil)
	if res, _ := h2.post(userBody("hi")); res.StatusCode != 200 {
		t.Errorf("status after panic: %d", res.StatusCode)
	}
}

func TestRewriterErrorAbortsClient(t *testing.T) {
	upClosed := make(chan struct{})
	fa := &fakeAdapter{Adapter: codex.New(), rewriter: func(r *transform.Restorer) adapter.ResponseRewriter {
		return &failingRewriter{inner: codex.New().NewResponseRewriter(r), failAt: 2}
	}}
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		sseHeaders(w)
		writeEvent(w, "response.created", evCreated)
		writeEvent(w, "response.output_text.delta", textDelta("hello"))
		writeEvent(w, "response.output_text.delta", textDelta(" world"))
		select {
		case <-r.Context().Done():
			close(upClosed)
		case <-time.After(waitLimit):
		}
	}, withAdapter(fa))
	res, err := h.client.Do(h.req("POST", "/v1/responses", userBody("hi")))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err == nil {
		t.Fatalf("client saw a clean end of stream after a rewrite error: %q", body)
	}
	if bytes.Contains(body, []byte("world")) {
		t.Error("events after the failure reached the client")
	}
	select {
	case <-upClosed:
	case <-time.After(waitLimit):
		t.Fatal("upstream body not closed after rewrite error")
	}
	a := h.nextAudit()
	if a.Blocked != "stream_rewrite" {
		t.Errorf("audit = %+v", a)
	}
	if s := h.reg.Snapshot(); s.Errors != 1 {
		t.Errorf("errors = %d", s.Errors)
	}
}

func TestConcurrentRequestsKeepRuleCountsSeparate(t *testing.T) {
	var arrived atomic.Int32
	both := make(chan struct{})
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if arrived.Add(1) == 2 {
			close(both)
		}
		select {
		case <-both:
		case <-time.After(waitLimit):
		}
		defaultStream(w)
	})
	bodies := []string{
		userBody("ping " + realIP),
		userBody("mail bob@corp-internal.com and carol@corp-internal.com about 10.9.8.7"),
	}
	var wg sync.WaitGroup
	for _, b := range bodies {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest("POST", h.px.URL+"/v1/responses", strings.NewReader(b))
			if res, err := h.client.Do(req); err == nil {
				io.Copy(io.Discard, res.Body)
				res.Body.Close()
			}
		}()
	}
	wg.Wait()
	select {
	case <-both:
	default:
		t.Fatal("requests did not overlap")
	}
	var got []map[string]int
	for i := 0; i < 2; i++ {
		got = append(got, h.nextAudit().Rules)
	}
	want1 := map[string]int{"builtin.ipv4": 1}
	want2 := map[string]int{"builtin.ipv4": 1, "builtin.email": 2}
	ok := func(a, b map[string]int) bool { return fmt.Sprint(a) == fmt.Sprint(b) }
	if !(ok(got[0], want1) && ok(got[1], want2) || ok(got[0], want2) && ok(got[1], want1)) {
		t.Errorf("rules = %v, want %v and %v", got, want1, want2)
	}
	if s := h.reg.Snapshot(); s.ReplacementsByRule["builtin.ipv4"] != 2 || s.ReplacementsByRule["builtin.email"] != 2 {
		t.Errorf("metrics rules = %v", s.ReplacementsByRule)
	}
}

func TestSnapshotEvaluatedOncePerRequest(t *testing.T) {
	swapped := make(chan struct{})
	var syn string
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		sseHeaders(w)
		writeEvent(w, "response.created", evCreated)
		select {
		case <-swapped:
		case <-time.After(waitLimit):
		}
		writeEvent(w, "response.output_text.delta", textDelta("host "+syn+" ok"))
		writeEvent(w, "response.completed", evCompleted)
	})
	syn = h.set().pseudo(t, "ipv4", realIP)
	res, err := h.client.Do(h.req("POST", "/v1/responses", userBody("hi")))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	rd := sse.NewReader(res.Body)
	if _, err := rd.Next(); err != nil {
		t.Fatal(err)
	}
	h.cur.Store(newSet(t)) // a fresh vault that does not know syn
	close(swapped)
	rest, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got := deltas(t, readEvents(t, rest), "response.output_text.delta"); got != "host "+realIP+" ok" {
		t.Errorf("in-flight request lost its restorer: %q", got)
	}
	if n := h.snapCalls.Load(); n != 1 {
		t.Errorf("Snapshot called %d times, want 1", n)
	}
}

// TestSnapshotKeepsPolicyAndRulesTogether swaps the whole snapshot (policy
// and rules) while a request is being sanitized: the request finishes with
// the old policy (passthrough) and the old restorer, and the next request
// gets the new policy (block).
func TestSnapshotKeepsPolicyAndRulesTogether(t *testing.T) {
	oldSet, nextSet := newSet(t), newSet(t)
	oldCfg := &config.Config{Policy: config.Policy{OnError: "passthrough", MaxBodyBytes: 1 << 20}}
	newCfg := &config.Config{Policy: config.Policy{OnError: "block", MaxBodyBytes: 1 << 20}}
	var snap atomic.Pointer[Snapshot]
	snap.Store(&Snapshot{Cfg: oldCfg, Sanitizer: oldSet.s, Restorer: oldSet.r})
	var calls atomic.Int64
	var once sync.Once
	fa := &fakeAdapter{Adapter: codex.New(), sanitize: func([]byte, *transform.Sanitizer) ([]byte, error) {
		// A reload lands mid-request: new policy and new rules at once.
		once.Do(func() { snap.Store(&Snapshot{Cfg: newCfg, Sanitizer: nextSet.s, Restorer: nextSet.r}) })
		return nil, errors.New("sanitize failed")
	}}
	syn := oldSet.pseudo(t, "ipv4", realIP)
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		sseHeaders(w)
		writeEvent(w, "response.output_text.delta", textDelta("host "+syn))
		writeEvent(w, "response.completed", evCompleted)
	}, withAdapter(fa), func(_ *harness, d *Deps) {
		d.Snapshot = func() Snapshot { calls.Add(1); return *snap.Load() }
	})
	orig := userBody("hi")
	res, body := h.post(orig)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: in-flight request lost its passthrough policy", res.StatusCode)
	}
	if got := h.upstreamGot(); string(got.Body) != orig {
		t.Errorf("upstream body = %q", got.Body)
	}
	if got := deltas(t, readEvents(t, body), "response.output_text.delta"); got != "host "+realIP {
		t.Errorf("in-flight request lost its restorer: %q", got)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("Snapshot called %d times for one request, want 1", n)
	}
	if a := h.nextAudit(); a.Rules["passthrough:sanitize"] != 1 {
		t.Errorf("audit = %+v", a)
	}
	res, body = h.post(orig)
	assertBlocked(t, h, res, body, http.StatusUnprocessableEntity, "sanitize")
	if n := calls.Load(); n != 2 {
		t.Errorf("Snapshot called %d times for two requests, want 2", n)
	}
}

func TestUpstreamUnreachable(t *testing.T) {
	h := newHarness(t, nil)
	h.up.Close()
	res, body := h.post(userBody("hi"))
	if res.StatusCode != http.StatusBadGateway {
		t.Errorf("status %d", res.StatusCode)
	}
	var e apiError
	if json.Unmarshal(body, &e) != nil || e.Error.Code != "upstream" {
		t.Errorf("body %q", body)
	}
	a := h.nextAudit()
	if a.Status != http.StatusBadGateway || a.Blocked != "" {
		t.Errorf("audit = %+v", a)
	}
	if s := h.reg.Snapshot(); s.Errors != 1 || s.Blocked != 0 {
		t.Errorf("metrics errors=%d blocked=%d", s.Errors, s.Blocked)
	}
	if l := h.logs.String(); strings.Contains(l, h.up.URL) || strings.Contains(l, "127.0.0.1") {
		t.Errorf("log contains upstream URL: %q", l)
	}
}

func TestDefaultTransport(t *testing.T) {
	tr := newTransport()
	if tr.TLSHandshakeTimeout != 10*time.Second || tr.ResponseHeaderTimeout != 10*time.Minute ||
		tr.IdleConnTimeout != 90*time.Second || !tr.ForceAttemptHTTP2 || tr.Proxy == nil || tr.DialContext == nil {
		t.Errorf("transport = %+v", tr)
	}
}

// ---- fakes ----

type fakeAdapter struct {
	adapter.Adapter
	sanitize func([]byte, *transform.Sanitizer) ([]byte, error)
	rewriter func(*transform.Restorer) adapter.ResponseRewriter
}

func (f *fakeAdapter) SanitizeRequest(b []byte, s *transform.Sanitizer) ([]byte, error) {
	if f.sanitize != nil {
		return f.sanitize(b, s)
	}
	return f.Adapter.SanitizeRequest(b, s)
}

func (f *fakeAdapter) NewResponseRewriter(r *transform.Restorer) adapter.ResponseRewriter {
	if f.rewriter != nil {
		return f.rewriter(r)
	}
	return f.Adapter.NewResponseRewriter(r)
}

// failingRewriter fails on the failAt-th event (1-based).
type failingRewriter struct {
	inner  adapter.ResponseRewriter
	n      int
	failAt int
}

func (f *failingRewriter) Event(e sse.Event) ([]sse.Event, error) {
	if f.n++; f.n == f.failAt {
		return nil, errors.New("rewrite failed")
	}
	return f.inner.Event(e)
}

func (f *failingRewriter) Flush() []sse.Event { return f.inner.Flush() }

// I1: a value glued where its bounded synthetic would not restore is
// forwarded as a REDACTED token (not blocked) and counted as downgraded.
func TestGluedValueDowngradedNotBlocked(t *testing.T) {
	h := newHarness(t, nil)
	res, _ := h.post(userBody("route via:fe80::1 now"))
	if res.StatusCode != 200 {
		t.Fatalf("status = %d", res.StatusCode)
	}
	if dump := h.upstreamGot().dump(); strings.Contains(dump, "fe80::1") || !strings.Contains(dump, "via:REDACTED-") {
		t.Errorf("upstream body: %s", dump)
	}
	if a := h.nextAudit(); a.Blocked != "" || a.Rules["builtin.ipv6"] != 1 {
		t.Errorf("audit = %+v", a)
	}
	if s := h.reg.Snapshot(); s.Downgraded != 1 || s.Blocked != 0 {
		t.Errorf("metrics = %+v", s)
	}
}
