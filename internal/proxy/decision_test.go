package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"

	"cui-proxy/internal/adapter"
	"cui-proxy/internal/adapter/codex"
	"cui-proxy/internal/config"
	"cui-proxy/internal/decisionlog"
	"cui-proxy/internal/detect"
	"cui-proxy/internal/transform"
	"cui-proxy/internal/vault"
)

// records reads every decision record the harness wrote.
func (h *harness) records() []decisionlog.Record {
	h.t.Helper()
	b, err := os.ReadFile(h.decPath)
	if err != nil {
		h.t.Fatal(err)
	}
	var out []decisionlog.Record
	for _, ln := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
		if ln == "" {
			continue
		}
		var r decisionlog.Record
		if err := json.Unmarshal([]byte(ln), &r); err != nil {
			h.t.Fatalf("record %q: %v", ln, err)
		}
		out = append(out, r)
	}
	return out
}

// lastRecord is the only record for the response's request id.
func (h *harness) recordFor(res *http.Response) decisionlog.Record {
	h.t.Helper()
	id := res.Header.Get(requestIDHeader)
	if _, err := decisionlog.ParseID(id); err != nil {
		h.t.Fatalf("response request id %q: %v", id, err)
	}
	var got []decisionlog.Record
	for _, r := range h.records() {
		if r.RequestID == id {
			got = append(got, r)
		}
	}
	if len(got) != 1 {
		h.t.Fatalf("%d records for request %s, want 1", len(got), id)
	}
	return got[0]
}

// failRecorder fails every Record, counting the attempts.
type failRecorder struct {
	err error
	n   atomic.Int64
}

func (f *failRecorder) Record(decisionlog.Entry) error { f.n.Add(1); return f.err }

func TestForwardedRequestIsRecordedBeforeItLeaves(t *testing.T) {
	var h *harness
	seen := make(chan string, 1)
	h = newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		// At the moment the upstream sees the request, its record is on disk.
		b, _ := os.ReadFile(h.decPath)
		seen <- string(b)
		defaultStream(w)
	})
	res, _ := h.post(userBody("host " + realIP + " and " + realEmail))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	id := res.Header.Get(requestIDHeader)
	if onDisk := <-seen; !strings.Contains(onDisk, `"request_id":"`+id+`"`) {
		t.Fatalf("the upstream was called before the decision was recorded; log then: %q", onDisk)
	}
	r := h.recordFor(res)
	if r.Decision != decisionlog.Sanitized || r.Reason != "" || r.Adapter != "codex" || r.Profile != "sanitize" || r.ApprovalID != "" {
		t.Errorf("record %+v", r)
	}
	if strings.Join(r.RuleIDs, ",") != "builtin.email,builtin.ipv4" || r.Counts["builtin.ipv4"] != 1 || r.Counts["builtin.email"] != 1 {
		t.Errorf("rules %v counts %v", r.RuleIDs, r.Counts)
	}
	if a := h.nextAudit(); a.RequestID != id {
		t.Errorf("dashboard entry request id %q, want %q", a.RequestID, id)
	}
	if rep, err := decisionlog.Verify(h.decPath); err != nil || !rep.OK() {
		t.Errorf("verify: %+v %v", rep, err)
	}
}

func TestDecisionsForEachOutcome(t *testing.T) {
	h := newHarness(t, nil)
	// Clean text: forwarded, no rules.
	res, _ := h.post(userBody("explain the build"))
	if r := h.recordFor(res); r.Decision != decisionlog.Forwarded || len(r.RuleIDs) != 0 || len(r.Counts) != 0 {
		t.Errorf("clean: %+v", r)
	}
	h.upstreamGot()
	// A passthrough route: forwarded.
	res, _ = h.do(h.req("GET", "/v1/models", ""))
	if r := h.recordFor(res); r.Decision != decisionlog.Forwarded {
		t.Errorf("models: %+v", r)
	}
	h.upstreamGot()

	for _, c := range []struct {
		name   string
		req    func() *http.Request
		reason string
		rule   string
	}{
		{"header", func() *http.Request {
			q := h.req("POST", "/v1/responses", userBody("hi"))
			q.Header.Set("X-Custom", realIP)
			return q
		}, "header", ""},
		{"query", func() *http.Request { return h.req("POST", "/v1/responses?x="+realIP, userBody("hi")) }, "header", ""},
		{"route", func() *http.Request { return h.req("POST", "/v1/nope", userBody("hi")) }, "route", ""},
		{"compressed", func() *http.Request {
			q := h.req("POST", "/v1/responses", userBody("hi"))
			q.Header.Set("Content-Encoding", "gzip")
			return q
		}, "compressed", ""},
		{"too large", func() *http.Request { return h.req("POST", "/v1/responses", strings.Repeat("x", 2<<20)) }, "body_too_large", ""},
		{"body on a passthrough route", func() *http.Request { return h.req("GET", "/v1/models", "x") }, "bad_request", ""},
		{"unparseable", func() *http.Request { return h.req("POST", "/v1/responses", "not json") }, "sanitize", ""},
	} {
		res, _ := h.do(c.req())
		r := h.recordFor(res)
		if r.Decision != decisionlog.Blocked || r.Reason != c.reason {
			t.Errorf("%s: record %+v", c.name, r)
		}
		h.noUpstreamCall()
	}

	// A policy block names its rule.
	useProfile(t, h, "strict", "")
	res, _ = h.post(userBody("host " + realIP))
	if r := h.recordFor(res); r.Decision != decisionlog.Blocked || r.Reason != "policy" || r.Profile != "strict" ||
		strings.Join(r.RuleIDs, ",") != "builtin.ipv4" || r.Counts["builtin.ipv4"] != 1 {
		t.Errorf("policy: %+v", r)
	}
	h.noUpstreamCall()
	if rep, err := decisionlog.Verify(h.decPath); err != nil || !rep.OK() || rep.Records != 10 {
		t.Errorf("verify: %+v %v", rep, err)
	}
}

// A request forwarded unsanitized under policy.on_error: passthrough is
// recorded as such, never as a clean forward.
func TestPassthroughForwardIsRecordedAsPassthrough(t *testing.T) {
	fa := &fakeAdapter{Adapter: codex.New(), sanitize: func([]byte, *transform.Sanitizer) ([]byte, error) {
		return nil, fmt.Errorf("x: %w", transform.ErrPseudonym)
	}}
	h := newHarness(t, nil, withAdapter(fa))
	h.cfg.Policy.OnError = "passthrough"
	res, _ := h.post(userBody("host " + realIP))
	r := h.recordFor(res)
	if r.Decision != decisionlog.Forwarded || r.Counts["passthrough:sanitize"] != 1 || len(r.RuleIDs) != 0 {
		t.Errorf("record %+v", r)
	}
}

// Every reason the proxy can block with before forwarding is one the
// decision log accepts (the two sets are kept in step).
func TestEveryBlockReasonIsRecordable(t *testing.T) {
	for reason := range blockStatus {
		if reason == reasonStreamRewrite { // after forwarding: the record already exists
			continue
		}
		if !decisionlog.KnownReason(reason) {
			t.Errorf("reason %q is not recordable", reason)
		}
	}
}

func TestRequestIDOnEveryResponse(t *testing.T) {
	h := newHarness(t, nil)
	ok, _ := h.post(userBody("hi"))
	h.upstreamGot()
	blocked, body := h.do(h.req("POST", "/v1/nope", "x"))
	a, b := ok.Header.Get(requestIDHeader), blocked.Header.Get(requestIDHeader)
	if len(a) != 16 || len(b) != 16 || a == b {
		t.Fatalf("ids %q %q", a, b)
	}
	var e struct {
		Error struct {
			Message   string
			RequestID string `json:"request_id"`
		}
	}
	if err := json.Unmarshal(body, &e); err != nil || e.Error.RequestID != b || !strings.HasSuffix(e.Error.Message, " request_id="+b) {
		t.Errorf("error body %s", body)
	}
}

// ---- fail closed ----

// A write failure under audit.on_error: block (the default for a config
// built in code, and always under strict) blocks with reason audit and
// never reaches the upstream, on every forwarding path.
func TestRecordFailureBlocksBeforeForwarding(t *testing.T) {
	sanitizerFails := &fakeAdapter{Adapter: codex.New(), sanitize: func([]byte, *transform.Sanitizer) ([]byte, error) {
		return nil, fmt.Errorf("x: %w", transform.ErrPseudonym)
	}}
	for _, c := range []struct {
		name  string
		setup func(*harness)
		req   func(*harness) *http.Request
		opts  []option
	}{
		{"sanitized forward", nil, func(h *harness) *http.Request { return h.req("POST", "/v1/responses", userBody("host "+realIP)) }, nil},
		{"clean forward", nil, func(h *harness) *http.Request { return h.req("POST", "/v1/responses", userBody("hi")) }, nil},
		{"passthrough route", nil, func(h *harness) *http.Request { return h.req("GET", "/v1/models", "") }, nil},
		{"on_error passthrough forward", func(h *harness) { h.cfg.Policy.OnError = "passthrough" },
			func(h *harness) *http.Request { return h.req("POST", "/v1/responses", userBody("host "+realIP)) },
			[]option{withAdapter(sanitizerFails)}},
		{"strict ignores on_error warn", func(h *harness) {
			useProfile(t, h, "strict", "")
			h.cfg.Audit.OnError = "warn" // a code-built config skips validation
		}, func(h *harness) *http.Request { return h.req("POST", "/v1/responses", userBody("hi")) }, nil},
		{"explicit block under sanitize", func(h *harness) { h.cfg.Audit.OnError = "block" },
			func(h *harness) *http.Request { return h.req("POST", "/v1/responses", userBody("hi")) }, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			fr := &failRecorder{err: fmt.Errorf("decision log: write: %w", syscall.ENOSPC)}
			h := newHarness(t, nil, append(c.opts, withDecisions(fr))...)
			if c.setup != nil {
				c.setup(h)
			}
			res, body := h.do(c.req(h))
			assertBlocked(t, h, res, body, http.StatusServiceUnavailable, "audit")
			h.noUpstreamCall()
			if !strings.Contains(string(body), "reason=audit kind=no_space") || strings.Contains(string(body), realIP) {
				t.Errorf("body %s", body)
			}
			if fr.n.Load() != 1 {
				t.Errorf("%d record attempts, want exactly 1", fr.n.Load())
			}
			s := h.reg.Snapshot()
			if s.AuditWriteFailed != 1 || s.Passthrough != 0 {
				t.Errorf("audit_write_failed %d passthrough %d", s.AuditWriteFailed, s.Passthrough)
			}
			if logs := h.logs.String(); !strings.Contains(logs, "WARN decision record not written (no_space); request blocked") || strings.Contains(logs, realIP) {
				t.Errorf("logs %q", logs)
			}
		})
	}
}

// The real log, closed underneath the proxy, fails closed the same way.
func TestClosedDecisionLogBlocks(t *testing.T) {
	h := newHarness(t, nil)
	h.dec.Close()
	res, body := h.post(userBody("hi"))
	assertBlocked(t, h, res, body, http.StatusServiceUnavailable, "audit")
	if !strings.Contains(string(body), "kind=closed") || strings.Contains(string(body), h.decPath) {
		t.Errorf("body %s", body)
	}
}

// Under audit.on_error: warn (sanitize only) the request proceeds, a WARN is
// logged at most once a minute, and the metric counts every missed record.
func TestRecordFailureUnderWarnForwards(t *testing.T) {
	fr := &failRecorder{err: fmt.Errorf("x: %w", syscall.EACCES)}
	h := newHarness(t, nil, withDecisions(fr))
	h.cfg.Audit.OnError = "warn"
	for range 3 {
		res, _ := h.post(userBody("hi"))
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status %d", res.StatusCode)
		}
		h.upstreamGot()
	}
	if s := h.reg.Snapshot(); s.AuditWriteFailed != 3 || s.Blocked != 0 {
		t.Errorf("audit_write_failed %d blocked %d", s.AuditWriteFailed, s.Blocked)
	}
	if logs := h.logs.String(); strings.Count(logs, "WARN decision record not written (permission); request forwarded anyway (audit.on_error=warn)") != 1 {
		t.Errorf("logs %q", logs)
	}
}

// A blocked decision stays blocked, with its own reason, when its record
// cannot be written; the failure is counted.
func TestRecordFailureOnABlockedRequest(t *testing.T) {
	for _, onError := range []string{"block", "warn"} {
		fr := &failRecorder{err: syscall.EIO}
		h := newHarness(t, nil, withDecisions(fr))
		h.cfg.Audit.OnError = onError
		req := h.req("POST", "/v1/responses", userBody("hi"))
		req.Header.Set("X-Custom", realIP)
		res, body := h.do(req)
		assertBlocked(t, h, res, body, http.StatusUnprocessableEntity, "header")
		if s := h.reg.Snapshot(); s.AuditWriteFailed != 1 {
			t.Errorf("%s: audit_write_failed %d", onError, s.AuditWriteFailed)
		}
	}
}

// Later failures (a redirect, a stream cut) never rewrite or add a record.
func TestOneRecordPerRequestWhateverHappensLater(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://elsewhere.example/")
		w.WriteHeader(http.StatusFound)
	})
	res, _ := h.post(userBody("host " + realIP))
	if res.StatusCode != http.StatusBadGateway {
		t.Fatalf("status %d", res.StatusCode)
	}
	if r := h.recordFor(res); r.Decision != decisionlog.Sanitized {
		t.Errorf("record %+v", r)
	}

	h2 := newHarness(t, nil, withAdapter(&fakeAdapter{Adapter: codex.New(), rewriter: func(r *transform.Restorer) adapter.ResponseRewriter {
		return &failingRewriter{inner: codex.New().NewResponseRewriter(r), failAt: 1}
	}}))
	if res, err := h2.client.Do(h2.req("POST", "/v1/responses", userBody("hi"))); err == nil {
		res.Body.Close() // the cut may come before or after the headers
	}
	h2.nextAudit()
	if recs := h2.records(); len(recs) != 1 || recs[0].Decision != decisionlog.Forwarded {
		t.Errorf("records %+v", recs)
	}
}

// ---- no values ----

// Sentinel secrets fired through every part of a request (body, headers,
// query, path, dictionary entries) never reach the decision log.
func TestDecisionLogHoldsNoValues(t *testing.T) {
	const (
		sentHost   = "sentinel-host-q7.agency.internal"
		sentIP     = "10.99.88.77"
		sentToken  = "Bearer sentinel-TOKEN-zz9"
		sentAcct   = "sentinel-acct-551"
		sentCookie = "sess=sentinel-cookie-4"
		sentPath   = "sentinelpath77"
		sentQuery  = "sentinelquery42"
		sentHeader = "sentinel-header-val"
		sentPrompt = "sentinel prompt words"
	)
	dir := t.TempDir()
	dict := filepath.Join(dir, "hosts.txt")
	if err := os.WriteFile(dict, []byte(sentHost+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	build := func(profile string) *set {
		ds, err := detect.BuildForProfile([]config.Rule{
			{Name: "builtin", Type: "builtin"},
			{Name: "hosts", Type: "dictionary", File: dict, Synth: "hostname"},
		}, dir, profile)
		if err != nil {
			t.Fatal(err)
		}
		v := vault.New(testKey)
		return &set{v: v, s: transform.NewSanitizer(ds, v, nil), r: transform.NewRestorer(v)}
	}
	h := newHarness(t, nil)
	h.cur.Store(build("sanitize"))
	creds := func(q *http.Request) *http.Request {
		q.Header.Set("Authorization", sentToken)
		q.Header.Set("Chatgpt-Account-Id", sentAcct)
		q.Header.Set("Cookie", sentCookie)
		q.Header.Set("X-Note", sentHeader)
		return q
	}
	body := userBody(sentPrompt + " " + sentHost + " " + sentIP)
	send := func(q *http.Request) { h.do(q); h.nextAudit() }
	send(creds(h.req("POST", "/v1/responses?x="+sentQuery, body))) // sanitized, forwarded
	h.upstreamGot()
	q := creds(h.req("POST", "/v1/responses", body))
	q.Header.Set("X-Leak", sentHost) // header block
	send(q)
	send(creds(h.req("POST", "/v1/responses?h="+sentIP, body)))           // query block
	send(creds(h.req("POST", "/v1/"+sentPath+"/"+sentHost, body)))        // route block
	send(creds(h.req("GET", "/v1/models?client_version="+sentQuery, ""))) // passthrough route
	h.upstreamGot()
	h.cur.Store(build("strict"))
	h.cfg.Policy.Profile = "strict"
	send(creds(h.req("POST", "/v1/responses", body))) // policy block

	raw, err := os.ReadFile(h.decPath)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(raw), "\n"); n != 6 {
		t.Fatalf("%d records, want 6", n)
	}
	for _, s := range []string{sentHost, sentIP, sentToken, "sentinel", sentAcct, sentCookie, sentPath, sentQuery, sentHeader, sentPrompt, "/v1/", "http"} {
		if strings.Contains(string(raw), s) {
			t.Errorf("the decision log contains %q", s)
		}
	}
	// Only the documented keys appear.
	for _, ln := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(ln), &m); err != nil {
			t.Fatal(err)
		}
		if len(m) != 11 {
			t.Errorf("record keys %v", m)
		}
	}
}

// Every recordable block reason has an explanation for `cui-proxy explain`.
func TestEveryBlockReasonIsExplained(t *testing.T) {
	for reason := range blockStatus {
		if reason == reasonStreamRewrite {
			continue
		}
		if msg, change, ok := Explain(reason); !ok || msg == "" || change == "" {
			t.Errorf("reason %q has no explanation", reason)
		}
	}
	if _, _, ok := Explain("nope"); ok {
		t.Error("an unknown reason is explained")
	}
}
