// Package proxy is the fail-closed sanitizing reverse proxy: it checks each
// request against the adapter's routes, sanitizes the body, forwards it
// upstream with the client's headers, and restores synthetic values in the
// SSE response stream.
//
// The handler is written by hand rather than with httputil.ReverseProxy so
// that every failure path is explicit: ReverseProxy's default error handler
// logs transport errors (which carry the upstream URL and query string), and
// rewriting the stream in ModifyResponse would need a pipe and a second
// goroutine per response. Here the response is read, rewritten and written
// on the handler's own goroutine, so a client disconnect ends everything.
//
// Nothing here logs or audits request or response bodies, headers, tokens or
// query strings; log lines carry only fixed text, reason codes and type names.
package proxy

import (
	"bytes"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"cui-proxy/internal/adapter"
	"cui-proxy/internal/audit"
	"cui-proxy/internal/config"
	"cui-proxy/internal/metrics"
	"cui-proxy/internal/sse"
	"cui-proxy/internal/synth"
	"cui-proxy/internal/transform"
)

// Snapshot is the hot-reloadable state one request runs with: the policy
// and the rules come from the same reload, so a request can never combine
// new rules with an old policy (or the reverse).
type Snapshot struct {
	Cfg       *config.Config
	Sanitizer *transform.Sanitizer
	Restorer  *transform.Restorer
}

// Deps are the proxy's collaborators. Adapter, Upstream, Snapshot, Metrics
// and Audit are required.
type Deps struct {
	Adapter  adapter.Adapter
	Upstream *url.URL // base, e.g. https://chatgpt.com/backend-api/codex
	// Snapshot is evaluated exactly once per request, so a reload never
	// changes an in-flight request's policy or rules.
	Snapshot  func() Snapshot
	VaultLen  func() int // reported via Metrics.SetVaultSize after each request; nil ok
	Metrics   *metrics.Registry
	Audit     *audit.Log
	Transport http.RoundTripper                // nil: newTransport()
	Logf      func(format string, args ...any) // nil: log.Printf; never given values, headers or bodies
}

// Block reasons: a closed set, used as metrics.IncBlocked keys, the JSON
// error code and audit.Entry.Blocked.
const (
	reasonRoute        = "route"
	reasonTooLarge     = "body_too_large"
	reasonCompressed   = "compressed"
	reasonSanitize     = "sanitize"
	reasonUnrestorable = "unrestorable"
	reasonBadRequest   = "bad_request"
	reasonInternal     = "internal"
	// reasonHeader: a request header or the query string holds a detected
	// value. Headers are never rewritten, so this always blocks, whatever
	// policy.on_error says.
	reasonHeader = "header"
	// reasonStreamRewrite marks a response stream cut because it could not be
	// rewritten. It is counted with Metrics.IncError, not IncBlocked: the
	// request was already forwarded.
	reasonStreamRewrite = "stream_rewrite"
	// reasonUpstreamRedirect: the upstream answered with a 3xx (other than
	// 304). Relaying it would let a redirect-following client re-send the
	// original, unsanitized request elsewhere. Counted with IncError.
	reasonUpstreamRedirect = "upstream_redirect"
)

var blockStatus = map[string]int{
	reasonRoute:         http.StatusForbidden,
	reasonTooLarge:      http.StatusRequestEntityTooLarge,
	reasonCompressed:    http.StatusUnsupportedMediaType,
	reasonSanitize:      http.StatusUnprocessableEntity,
	reasonUnrestorable:  http.StatusUnprocessableEntity,
	reasonBadRequest:    http.StatusBadRequest,
	reasonInternal:      http.StatusInternalServerError,
	reasonHeader:        http.StatusUnprocessableEntity,
	reasonStreamRewrite: http.StatusBadGateway,
}

var blockMessage = map[string]string{
	reasonRoute:         "unsupported route",
	reasonTooLarge:      "request body too large",
	reasonCompressed:    "compressed request bodies are not supported",
	reasonSanitize:      "request could not be sanitized",
	reasonUnrestorable:  "request could not be sanitized reversibly",
	reasonBadRequest:    "malformed request",
	reasonInternal:      "internal error",
	reasonHeader:        "a request header or the query string contains a sensitive value",
	reasonStreamRewrite: "response could not be restored",
}

// Block kinds: a closed set naming why a body could not be sanitized. They
// appear in the log line, audit.Entry.Blocked ("sanitize:<kind>") and the
// 422 message, never as metrics keys.
const (
	kindBadBody       = "bad_body"       // adapter.ErrBadBody
	kindDepth         = "depth"          // adapter.ErrDepth
	kindPoolExhausted = "pool_exhausted" // no synthetic left for the class
	kindPseudonym     = "pseudonym"      // no synthetic for the value (e.g. invalid for its class)
	kindAmbiguous     = "ambiguous"      // value equals a synthetic it may not pass as
	kindUnrestorable  = "unrestorable"   // would not restore even downgraded
	kindOther         = "other"
)

// diag is the value-free diagnosis of a sanitize block: a closed-set kind
// and, when the error names a span, its rule and class (cleaned by safeName).
type diag struct{ kind, rule, class string }

// diagnose classifies a SanitizeRequest error. Only sentinel identities and
// the SpanError rule/class fields are read, never the error text (which may
// carry request data, e.g. a JSON key).
func diagnose(err error) diag {
	var d diag
	var se *transform.SpanError
	if errors.As(err, &se) {
		d.rule, d.class = safeName(se.Rule), safeName(se.Class)
	}
	switch {
	case errors.Is(err, transform.ErrUnrestorable):
		d.kind = kindUnrestorable
	case errors.Is(err, synth.ErrExhausted):
		d.kind = kindPoolExhausted
	case errors.Is(err, transform.ErrAmbiguous):
		d.kind = kindAmbiguous
	case errors.Is(err, transform.ErrPseudonym):
		d.kind = kindPseudonym
	case errors.Is(err, adapter.ErrDepth):
		d.kind = kindDepth
	case errors.Is(err, adapter.ErrBadBody):
		d.kind = kindBadBody
	default:
		d.kind = kindOther
	}
	return d
}

// maxNameLen caps a rule or class name in diagnostics.
const maxNameLen = 64

// safeName keeps a config-supplied name printable in a log line, an audit
// key and a JSON string without escaping: bytes outside [A-Za-z0-9._-]
// become '_', and the result is cut to maxNameLen.
func safeName(s string) string {
	if len(s) > maxNameLen {
		s = s[:maxNameLen]
	}
	b := []byte(s)
	for i, c := range b {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			b[i] = '_'
		}
	}
	return string(b)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// defaultMaxBody applies when the config carries no positive limit.
const defaultMaxBody = 20 << 20

// New returns the proxy handler. It panics if a required dependency is nil.
func New(d Deps) http.Handler {
	if d.Adapter == nil || d.Upstream == nil || d.Snapshot == nil || d.Metrics == nil || d.Audit == nil {
		panic("proxy: Adapter, Upstream, Snapshot, Metrics and Audit are required")
	}
	if d.Transport == nil {
		d.Transport = newTransport()
	}
	if d.Logf == nil {
		d.Logf = log.Printf
	}
	return &handler{d: d}
}

// newTransport is the upstream transport: bounded connection setup and time
// to first response byte, but no overall timeout (streams are long).
func newTransport() *http.Transport {
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
		ResponseHeaderTimeout: 10 * time.Minute,
		DisableCompression:    true, // Accept-Encoding is set explicitly
	}
}

type handler struct{ d Deps }

// exchange is the state of one request.
type exchange struct {
	w     *countingWriter
	entry audit.Entry
	// overhead is the time the proxy itself added: sanitizing the request
	// plus all rewriter.Event and rewriter.Flush calls on the response. It
	// excludes reading the body and waiting on upstream or client I/O. It is
	// observed only for sanitize routes that were forwarded.
	overhead time.Duration
	observe  bool
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	x := &exchange{
		w:     &countingWriter{rw: w},
		entry: audit.Entry{Time: start, Adapter: h.d.Adapter.Name(), Method: auditMethod(r.Method), Path: r.URL.Path},
	}
	h.d.Metrics.IncRequest()
	abort := h.serveSafe(x, w, r)

	x.entry.Status = x.w.status
	x.entry.BytesOut = x.w.n
	x.entry.LatencyMS = float64(time.Since(start)) / float64(time.Millisecond)
	if x.observe {
		h.d.Metrics.ObserveOverhead(x.overhead)
	}
	if h.d.VaultLen != nil {
		h.d.Metrics.SetVaultSize(h.d.VaultLen())
	}
	h.d.Audit.Add(x.entry)
	if abort {
		// Cut the client connection so a broken stream never looks complete.
		panic(http.ErrAbortHandler)
	}
}

// serveSafe runs serve and turns a panic into a blocked 500 (or, when the
// response has started, an aborted connection). Only the panic's type is
// logged: its value may hold request data.
func (h *handler) serveSafe(x *exchange, w http.ResponseWriter, r *http.Request) (abort bool) {
	defer func() {
		p := recover()
		if p == nil {
			return
		}
		if p == http.ErrAbortHandler {
			abort = true
			return
		}
		h.d.Logf("cui-proxy: panic (%T) while serving a request; request blocked", p)
		if x.w.status != 0 {
			x.entry.Blocked = reasonInternal
			h.d.Metrics.IncBlocked(reasonInternal)
			abort = true
			return
		}
		clear(x.w.Header())
		h.block(x, reasonInternal)
	}()
	return h.serve(x, w, r)
}

// serve applies the checks in order (route, compression, size, route kind,
// sanitizing) and forwards the request. It reports whether the client
// connection must be aborted.
func (h *handler) serve(x *exchange, w http.ResponseWriter, r *http.Request) bool {
	snap := h.d.Snapshot()
	cfg, san, rest := snap.Cfg, snap.Sanitizer, snap.Restorer
	if cfg == nil || san == nil || rest == nil {
		return h.block(x, reasonInternal)
	}
	route, ok := h.d.Adapter.Match(r.Method, r.URL.Path)
	if !ok {
		// The path is client-chosen and unscanned: keep it out of the audit log.
		x.entry.Path = "(unmatched)"
		return h.block(x, reasonRoute)
	}
	if encoded(r.Header) {
		return h.block(x, reasonCompressed)
	}
	// Before reading the body: headers and query are never rewritten, so a
	// detected value blocks outright (on_error: passthrough does not apply).
	if requestMetaSensitive(r, san) {
		return h.block(x, reasonHeader)
	}
	limit := cfg.Policy.MaxBodyBytes
	if limit <= 0 {
		limit = defaultMaxBody
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return h.block(x, reasonTooLarge)
		}
		return h.block(x, reasonBadRequest)
	}
	x.entry.BytesIn = len(body)

	switch route.Kind {
	case adapter.KindPassthrough:
		if len(body) > 0 {
			return h.block(x, reasonBadRequest) // a passthrough route is never scanned
		}
	case adapter.KindSanitize:
		out, reason, d := h.sanitize(x, san, body)
		if reason != "" {
			if cfg.Policy.OnError != "passthrough" {
				return h.blockDiag(x, reason, d)
			}
			h.d.Metrics.IncPassthrough()
			x.entry.Rules = map[string]int{"passthrough:" + reason: 1}
			h.d.Logf("cui-proxy: forwarding an unsanitized request (policy.on_error=passthrough, reason=%s kind=%s rule=%s class=%s)",
				reason, d.kind, orDash(d.rule), orDash(d.class))
			out = body
		}
		body = out
	default:
		return h.block(x, reasonInternal)
	}
	return h.forward(x, r, route, body, rest)
}

// collector gathers one request's sanitizer events.
type collector struct {
	mu  sync.Mutex
	evs []transform.Event
}

func (c *collector) Record(e transform.Event) {
	c.mu.Lock()
	c.evs = append(c.evs, e)
	c.mu.Unlock()
}

// sanitize runs the adapter with a per-request sink. Events reach Metrics
// and the audit entry only when the whole request sanitized, so a blocked
// request counts no replacements. On failure it returns a reason code and
// the value-free diagnosis.
func (h *handler) sanitize(x *exchange, san *transform.Sanitizer, body []byte) ([]byte, string, diag) {
	c := &collector{}
	t0 := time.Now()
	out, err := h.d.Adapter.SanitizeRequest(body, san.WithSink(c))
	x.overhead += time.Since(t0)
	x.observe = true
	if err != nil {
		d := diagnose(err)
		if d.kind == kindUnrestorable {
			return nil, reasonUnrestorable, d
		}
		return nil, reasonSanitize, d
	}
	if len(c.evs) > 0 {
		x.entry.Rules = map[string]int{}
		for _, e := range c.evs {
			h.d.Metrics.Record(e)
			x.entry.Rules[e.Rule] += e.Count
		}
	}
	return out, "", diag{}
}

// forward sends the request upstream and relays the response.
func (h *handler) forward(x *exchange, r *http.Request, route adapter.Route, body []byte, rest *transform.Restorer) bool {
	u := *h.d.Upstream
	u.Path = strings.TrimRight(u.Path, "/") + route.UpstreamPath
	u.RawPath = ""
	u.RawQuery = r.URL.RawQuery
	u.Fragment = ""
	var rd io.Reader
	if len(body) > 0 {
		rd = bytes.NewReader(body)
	}
	out, err := http.NewRequestWithContext(r.Context(), r.Method, u.String(), rd)
	if err != nil {
		return h.block(x, reasonInternal)
	}
	out.Header = upstreamHeader(r.Header)

	res, err := h.d.Transport.RoundTrip(out)
	if err != nil {
		if r.Context().Err() != nil {
			return false // client went away; nobody to answer
		}
		// err carries the URL and query string: log only its kind.
		h.d.Metrics.IncError()
		h.d.Logf("cui-proxy: upstream request failed (%s)", errKind(err))
		writeError(x.w, http.StatusBadGateway, "upstream", "cui_proxy_error", "upstream unavailable")
		return false
	}
	defer res.Body.Close()

	if res.StatusCode >= 300 && res.StatusCode < 400 && res.StatusCode != http.StatusNotModified {
		// Never relay Location or the redirect body.
		h.d.Metrics.IncError()
		x.entry.Blocked = reasonUpstreamRedirect
		h.d.Logf("cui-proxy: upstream answered with a redirect (%d); response blocked", res.StatusCode)
		writeError(x.w, http.StatusBadGateway, reasonUpstreamRedirect, "cui_proxy_error", "upstream redirected the request")
		return false
	}
	if !isSSE(res.Header) {
		copyResponseHeader(x.w.Header(), res.Header)
		x.w.WriteHeader(res.StatusCode)
		if _, err := io.Copy(x.w, res.Body); err != nil && r.Context().Err() == nil {
			h.d.Metrics.IncError()
			h.d.Logf("cui-proxy: response copy failed; connection aborted")
			return true
		}
		return false
	}
	if encoded(res.Header) {
		// Identity was requested; an encoded stream cannot be restored.
		h.d.Metrics.IncError()
		x.entry.Blocked = reasonStreamRewrite
		h.d.Logf("cui-proxy: upstream sent an encoded event stream; response blocked")
		writeError(x.w, blockStatus[reasonStreamRewrite], reasonStreamRewrite, "cui_proxy_blocked", blockMessage[reasonStreamRewrite])
		return false
	}
	copyResponseHeader(x.w.Header(), res.Header)
	x.w.Header().Del("Content-Length")
	x.w.WriteHeader(res.StatusCode)
	return h.stream(x, r, res.Body, rest)
}

// stream restores the event stream body into the client, flushing after
// every event. It reports whether the client connection must be aborted.
// The caller closes body.
func (h *handler) stream(x *exchange, r *http.Request, body io.Reader, rest *transform.Restorer) bool {
	rw := h.d.Adapter.NewResponseRewriter(rest)
	rc := http.NewResponseController(x.w)
	rd := sse.NewReader(body)
	// changed counts output events whose payload differs from their input
	// event (including re-chunked held-back deltas): an approximation of
	// restored events, reported via Metrics.IncRestore.
	changed := 0
	emit := func(evs []sse.Event, in []byte, fromFlush bool) error {
		for _, e := range evs {
			if fromFlush || !bytes.Equal(e.Data, in) {
				changed++
			}
			if err := sse.Write(x.w, e); err != nil {
				return err
			}
			if err := rc.Flush(); err != nil {
				return err
			}
		}
		return nil
	}
	defer func() { h.d.Metrics.IncRestore(changed) }()

	fail := func(what string) bool {
		if r.Context().Err() != nil {
			return false // the client went away: not an error
		}
		h.d.Metrics.IncError()
		x.entry.Blocked = reasonStreamRewrite
		h.d.Logf("cui-proxy: response stream cut (%s); connection aborted", what)
		return true
	}
	for {
		ev, err := rd.Next()
		if errors.Is(err, io.EOF) {
			t0 := time.Now()
			evs := rw.Flush()
			x.overhead += time.Since(t0)
			if err := emit(evs, nil, true); err != nil {
				return fail("write")
			}
			return false
		}
		if errors.Is(err, sse.ErrEventTooLarge) {
			return fail("event too large")
		}
		if err != nil {
			if r.Context().Err() != nil {
				return false
			}
			// The upstream broke the stream: abort so the client sees it broken.
			h.d.Metrics.IncError()
			h.d.Logf("cui-proxy: upstream stream read failed (%s); connection aborted", errKind(err))
			return true
		}
		if rd.Unterminated() {
			// The upstream closed mid-event: drop the partial event and
			// abort so the client never mistakes truncation for a clean end.
			h.d.Metrics.IncError()
			h.d.Logf("cui-proxy: upstream stream ended mid-event; connection aborted")
			return true
		}
		t0 := time.Now()
		evs, err := rw.Event(ev)
		x.overhead += time.Since(t0)
		if err != nil {
			return fail("rewrite")
		}
		if err := emit(evs, ev.Data, false); err != nil {
			return fail("write")
		}
	}
}

// block answers with the reason's error and counts it.
func (h *handler) block(x *exchange, reason string) bool {
	x.entry.Blocked = reason
	h.d.Metrics.IncBlocked(reason)
	h.d.Logf("cui-proxy: request blocked (reason=%s)", reason)
	writeError(x.w, blockStatus[reason], reason, "cui_proxy_blocked", blockMessage[reason])
	return false
}

// blockDiag blocks a request whose body could not be sanitized. Beyond
// block, it names the closed-set kind and the rule (config name, cleaned)
// in the log line, the audit entry (Blocked "<reason>:<kind>" and Rules
// "blocked:<rule>") and the 422 message. Metrics keys stay the reason.
func (h *handler) blockDiag(x *exchange, reason string, d diag) bool {
	x.entry.Blocked = reason
	if d.kind != reason {
		x.entry.Blocked = reason + ":" + d.kind
	}
	if d.rule != "" {
		x.entry.Rules = map[string]int{"blocked:" + d.rule: 1}
	}
	h.d.Metrics.IncBlocked(reason)
	h.d.Logf("cui-proxy: request blocked (reason=%s kind=%s rule=%s class=%s)", reason, d.kind, orDash(d.rule), orDash(d.class))
	msg := "request blocked (reason=" + reason + " kind=" + d.kind
	if d.rule != "" {
		msg += " rule=" + d.rule
	}
	writeError(x.w, blockStatus[reason], reason, "cui_proxy_blocked", msg+")")
	return false
}

// writeError writes an OpenAI-style JSON error. Its text is fixed per code
// (plus, from blockDiag, closed-set kinds and safeName-cleaned rule names)
// and never contains request data.
func writeError(w http.ResponseWriter, status int, code, typ, msg string) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	// The values are constants or safeName output: no characters that need escaping.
	_, _ = io.WriteString(w, `{"error":{"message":"cui-proxy: `+msg+`","type":"`+typ+`","code":"`+code+`"}}`)
}

// errKind names an error's category without its text (which may carry the
// URL or query string).
func errKind(err error) string {
	var ne net.Error
	switch {
	case errors.As(err, &ne) && ne.Timeout():
		return "timeout"
	case errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.EOF):
		return "unexpected eof"
	}
	return "connection error"
}

// auditMethods is the closed set of methods recorded as is; any other
// (client-chosen) method token is audited as "(other)".
var auditMethods = map[string]bool{
	"GET": true, "POST": true, "HEAD": true, "OPTIONS": true, "PUT": true, "DELETE": true, "PATCH": true,
}

func auditMethod(m string) string {
	if auditMethods[m] {
		return m
	}
	return "(other)"
}

// countingWriter records the status and body bytes sent to the client.
type countingWriter struct {
	rw     http.ResponseWriter
	status int
	n      int
}

func (c *countingWriter) Header() http.Header { return c.rw.Header() }

func (c *countingWriter) WriteHeader(code int) {
	if c.status == 0 {
		c.status = code
	}
	c.rw.WriteHeader(code)
}

func (c *countingWriter) Write(b []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	n, err := c.rw.Write(b)
	c.n += n
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying Flusher.
func (c *countingWriter) Unwrap() http.ResponseWriter { return c.rw }
