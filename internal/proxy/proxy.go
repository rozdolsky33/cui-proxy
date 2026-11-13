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
//
// Every request's decision is written to the decision log (Deps.Decisions)
// before anything is sent upstream: forward records first and is the only
// place a request leaves the proxy.
package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
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
	"cui-proxy/internal/attachment"
	"cui-proxy/internal/audit"
	"cui-proxy/internal/config"
	"cui-proxy/internal/decisionlog"
	"cui-proxy/internal/metrics"
	"cui-proxy/internal/observe"
	"cui-proxy/internal/recovery"
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

// Deps are the proxy's collaborators. Adapter, Upstream, Snapshot, Metrics,
// Audit and Decisions are required.
type Deps struct {
	Adapter  adapter.Adapter
	Upstream *url.URL // base, e.g. https://chatgpt.com/backend-api/codex
	// Snapshot is evaluated exactly once per request, so a reload never
	// changes an in-flight request's policy or rules.
	Snapshot func() Snapshot
	// PersistMappings commits mappings before forwarding; nil for ephemeral callers.
	PersistMappings func(context.Context) error
	VaultLen        func() int // reported via Metrics.SetVaultSize after each request; nil ok
	Metrics         *metrics.Registry
	Audit           *audit.Log
	// Decisions is the persisted decision log (decisionlog.Discard when
	// audit.enabled is false). One record per request, written before the
	// request is forwarded; Snapshot().Cfg.AuditBlocks says whether a failed
	// write blocks the request.
	Decisions decisionlog.Recorder
	Transport http.RoundTripper                // nil: newTransport()
	Logf      func(format string, args ...any) // nil: log.Printf; never given values, headers or bodies
}

// Block reasons: a closed set, used as metrics.IncBlocked keys, the JSON
// error code and audit.Entry.Blocked.
const (
	reasonClient       = "client"
	reasonOverload     = "overload"
	reasonVault        = "vault"
	reasonRoute        = "route"
	reasonTooLarge     = "body_too_large"
	reasonCompressed   = "compressed"
	reasonSanitize     = "sanitize"
	reasonUnrestorable = "unrestorable"
	reasonBadRequest   = "bad_request"
	reasonInternal     = "internal"
	// reasonPolicy: a rule whose effective action is block (the strict
	// profile, or action: block) matched. It is a decision, not a failure, so
	// policy.on_error: passthrough never forwards it.
	reasonPolicy = "policy"
	// reasonHeader: a request header or the query string holds a detected
	// value that cannot be safely rewritten. Confirmed structured Codex
	// metadata fields may be sanitized; other detected metadata always blocks,
	// whatever policy.on_error says.
	reasonHeader = "header"
	// reasonStreamRewrite marks a response stream cut because it could not be
	// rewritten. It is counted with Metrics.IncError, not IncBlocked: the
	// request was already forwarded.
	reasonStreamRewrite = "stream_rewrite"
	// reasonUpstreamRedirect: the upstream answered with a 3xx (other than
	// 304). Relaying it would let a redirect-following client re-send the
	// original, unsanitized request elsewhere. Counted with IncError.
	reasonUpstreamRedirect = "upstream_redirect"
	// reasonAudit: the decision record could not be written and audit blocks
	// (audit.on_error: block, always under strict). The request is never
	// forwarded. It is never itself recorded: the write is what failed.
	reasonAudit = "audit"
)

// requestIDHeader carries the request id (the decision log's request_id) on
// every response, for `cui-proxy explain`.
const requestIDHeader = "X-Cui-Proxy-Request-Id"

var blockStatus = map[string]int{
	reasonClient:        http.StatusForbidden,
	reasonOverload:      http.StatusServiceUnavailable,
	reasonVault:         http.StatusServiceUnavailable,
	reasonRoute:         http.StatusForbidden,
	reasonTooLarge:      http.StatusRequestEntityTooLarge,
	reasonCompressed:    http.StatusUnsupportedMediaType,
	reasonSanitize:      http.StatusUnprocessableEntity,
	reasonPolicy:        http.StatusUnprocessableEntity,
	reasonUnrestorable:  http.StatusUnprocessableEntity,
	reasonBadRequest:    http.StatusBadRequest,
	reasonInternal:      http.StatusInternalServerError,
	reasonHeader:        http.StatusUnprocessableEntity,
	reasonStreamRewrite: http.StatusBadGateway,
	reasonAudit:         http.StatusServiceUnavailable,
}

var blockMessage = map[string]string{
	reasonClient:        "browser requests or non-local host names are not allowed",
	reasonOverload:      "proxy capacity is busy; retry with bounded backoff",
	reasonVault:         "mappings could not be persisted; retry after storage recovers",
	reasonRoute:         "unsupported route",
	reasonTooLarge:      "request body too large",
	reasonCompressed:    "compressed request bodies are not supported",
	reasonSanitize:      "request could not be sanitized",
	reasonPolicy:        "request blocked by policy",
	reasonUnrestorable:  "request could not be sanitized reversibly",
	reasonBadRequest:    "malformed request",
	reasonInternal:      "internal error",
	reasonHeader:        "a request header or the query string contains a sensitive value",
	reasonStreamRewrite: "response could not be restored",
	reasonAudit:         "the decision could not be recorded",
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
	kindRuleMatch     = "rule_match"     // a blocking rule (strict profile or action: block) matched
	kindOther         = "other"
)

// diag is the value-free diagnosis of a sanitize block: a closed-set kind
// and, when the error names a span, its rule and class (cleaned by safeName).
type diag struct{ kind, rule, class, detail string }

// diagnose classifies a SanitizeRequest error. Only sentinel identities and
// the SpanError rule/class fields are read, never the error text (which may
// carry request data, e.g. a JSON key).
func diagnose(err error) diag {
	var d diag
	var ae *attachment.Error
	if errors.As(err, &ae) {
		d.detail = ae.Error()
	}
	var se *transform.SpanError
	if errors.As(err, &se) {
		d.rule, d.class = safeName(se.Rule), safeName(se.Class)
	}
	switch {
	case errors.Is(err, transform.ErrBlocked):
		d.kind = kindRuleMatch
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

// safeName keeps a config-supplied name printable in a log line, an audit
// key and a JSON string without escaping: bytes outside [A-Za-z0-9._-]
// become '_', and the result is cut to 64 bytes. The decision log applies
// the same rule (one implementation).
func safeName(s string) string { return decisionlog.SafeName(s) }

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
	if d.Adapter == nil || d.Upstream == nil || d.Snapshot == nil || d.Metrics == nil || d.Audit == nil || d.Decisions == nil {
		panic("proxy: Adapter, Upstream, Snapshot, Metrics, Audit and Decisions are required")
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

type handler struct {
	d           Deps
	auditWarn   rateLimit
	recurrences recovery.Tracker
}

// exchange is the state of one request.
type exchange struct {
	recurrence   *recovery.Tracker
	recoveryKind string
	trace        *observe.Trace
	correlation  metrics.CorrelationToken
	w            *countingWriter
	id           decisionlog.ID
	cfg          *config.Config // the request's snapshot config; nil until loaded
	entry        audit.Entry
	// recorded: the decision record was attempted. There is at most one
	// attempt per request; later failures (a stream cut, a redirect) do not
	// rewrite it.
	detail     string // closed diagnostic code for a blocked attachment
	recorded   bool
	recordedAs decisionlog.Decision // what the record says (when recorded)
	// decision and counts are what forward records for a forwarded request.
	decision    decisionlog.Decision
	counts      map[string]int
	passthrough *passthroughFwd // set when policy.on_error: passthrough forwards the original
	// overhead is the time the proxy itself added: sanitizing the request
	// plus all rewriter.Event and rewriter.Flush calls on the response. It
	// excludes reading the body and waiting on upstream or client I/O. It is
	// observed only for sanitize routes that were forwarded.
	events   []transform.Event // committed only after headers and body pass
	overhead time.Duration
	observe  bool
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	trace := observe.NewTrace()
	trace.Gauges = h.d.Metrics
	r = r.WithContext(observe.WithTrace(r.Context(), trace))
	done := h.d.Metrics.BeginRequest()
	defer done()
	id := decisionlog.NewID()
	x := &exchange{
		trace:      trace,
		recurrence: &h.recurrences,
		w:          &countingWriter{rw: w},
		id:         id,
		entry: audit.Entry{Time: start, RequestID: id.String(), Adapter: h.d.Adapter.Name(),
			Method: auditMethod(r.Method), Path: r.URL.Path},
	}
	w.Header().Set(requestIDHeader, id.String())
	h.d.Metrics.IncRequest()
	defer func() {
		stages := trace.Snapshot()
		h.d.Metrics.ObserveStages(stages, x.entry.BytesIn)
		h.d.Metrics.ObserveCorrelated(x.correlation, stages)
	}()
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
		if x.recorded && x.recordedAs != decisionlog.Blocked {
			// The record (one per request, written at the moment of
			// forwarding) says forwarded or sanitized; the request may or
			// may not have reached the upstream. Say so, by id only.
			h.d.Logf("cui-proxy: request %s failed internally after its decision was recorded as %s; the client got an error", x.id, x.recordedAs)
		}
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
	x.cfg = cfg
	if cfg != nil && cfg.Observability.Correlate {
		x.correlation = h.d.Metrics.Correlate(r.Header)
	}
	if cfg == nil || san == nil || rest == nil {
		return h.block(x, reasonInternal)
	}
	route, ok := h.d.Adapter.Match(r.Method, r.URL.Path)
	if !ok {
		// The path is client-chosen and unscanned: keep it out of the audit log.
		x.entry.Path = "(unmatched)"
		return h.block(x, reasonRoute)
	}
	if !nativeClient(r, cfg.AllowNonLoopback) {
		return h.block(x, reasonClient)
	}
	if encoded(r.Header) {
		return h.block(x, reasonCompressed)
	}
	limit := cfg.Policy.MaxBodyBytes
	if limit <= 0 {
		limit = defaultMaxBody
	}
	if r.ContentLength > limit {
		return h.block(x, reasonTooLarge)
	}
	release, readLimit, admissionErr := admit(r, limit, x.trace)
	if admissionErr != nil {
		w.Header().Set("Retry-After", "1")
		// Avoid net/http draining an unread slow HTTP/1 body before the refusal.
		w.Header().Set("Connection", "close")
		return h.block(x, reasonOverload)
	}
	defer release()
	var metadataReason string
	var metadataDiag diag
	func() {
		defer x.trace.Measure(observe.MetadataInspect)()
		// Structured metadata is adapter-owned; all remaining headers and the
		// query retain their fail-closed inspection.
		var structuredHeaders []string
		if hs, ok := h.d.Adapter.(adapter.HeaderSanitizer); ok {
			r = r.Clone(r.Context())
			c := &collector{}
			if err := hs.SanitizeHeaders(r.Header, san.WithSink(c)); err != nil {
				metadataReason, metadataDiag = reasonHeader, diagnose(err)
				return
			}
			x.events = append(x.events, c.evs...)
			structuredHeaders = hs.StructuredHeaders()
		}
		// Before reading the body: other headers and query are never rewritten, so a
		// detected value blocks outright (on_error: passthrough does not apply).
		if requestMetaSensitive(r, san, structuredHeaders) {
			metadataReason = reasonHeader
			return
		}
	}()
	if metadataReason != "" {
		if metadataDiag.kind == "" {
			return h.block(x, metadataReason)
		}
		return h.blockDiag(x, metadataReason, metadataDiag)
	}

	var body []byte
	var err error
	func() {
		defer x.trace.Measure(observe.IngressRead)()
		body, err = io.ReadAll(http.MaxBytesReader(w, r.Body, readLimit))
	}()
	x.entry.BytesIn = len(body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return h.block(x, reasonTooLarge)
		}
		return h.block(x, reasonBadRequest)
	}
	x.decision = decisionlog.Forwarded
	switch route.Kind {
	case adapter.KindPassthrough:
		if len(body) > 0 {
			return h.block(x, reasonBadRequest) // a passthrough route is never scanned
		}
	case adapter.KindSanitize:
		out, reason, d := h.sanitize(r.Context(), x, san, body)
		if reason != "" {
			if !passthroughEligible(cfg, san, reason, d) {
				return h.blockDiag(x, reason, d)
			}
			// Counted and logged by forward, once the decision is recorded.
			x.passthrough = &passthroughFwd{reason, d}
			x.entry.Rules = map[string]int{"passthrough:" + reason: 1}
			out = body
		} else if len(x.entry.Rules) > 0 {
			x.decision, x.counts = decisionlog.Sanitized, x.entry.Rules
		}
		body = out
	default:
		return h.block(x, reasonInternal)
	}
	if len(x.events) > 0 {
		if x.entry.Rules == nil {
			x.entry.Rules = map[string]int{}
		}
		for _, e := range x.events {
			h.d.Metrics.Record(e)
			x.entry.Rules[e.Rule] += e.Count
		}
		x.counts = map[string]int{}
		for _, e := range x.events {
			x.counts[e.Rule] += e.Count
		}
		if x.passthrough == nil {
			x.decision = decisionlog.Sanitized
		}
	}
	return h.forward(x, r, route, body, rest)
}

// passthroughFwd is a failed sanitize that policy.on_error: passthrough
// forwards unsanitized.
type passthroughFwd struct {
	reason string
	d      diag
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
func (h *handler) sanitize(ctx context.Context, x *exchange, san *transform.Sanitizer, body []byte) ([]byte, string, diag) {
	c := &collector{}
	t0 := time.Now()
	endBody := x.trace.Body()
	defer endBody()
	var out []byte
	var err error
	if a, ok := h.d.Adapter.(adapter.PolicySanitizer); ok && x.cfg.Policy.Attachments == "extract_text" {
		out, err = a.SanitizeRequestWithPolicy(ctx, body, san.WithSink(c), x.cfg.Policy.Attachments)
	} else {
		out, err = h.d.Adapter.SanitizeRequest(body, san.WithSink(c))
	}
	x.overhead += time.Since(t0)
	x.observe = true
	if err != nil {
		d := diagnose(err)
		switch d.kind {
		case kindRuleMatch:
			return nil, reasonPolicy, d
		case kindUnrestorable:
			return nil, reasonUnrestorable, d
		}
		return nil, reasonSanitize, d
	}
	if x.passthrough == nil && !bytes.Equal(out, body) {
		x.decision = decisionlog.Sanitized
	}
	x.events = append(x.events, c.evs...)
	return out, "", diag{}
}

// passthroughEligible reports whether a failed sanitize may forward the
// original request (policy.on_error: passthrough). It is the exception, not
// the rule: never for a policy block (a decision, not a failure), never when
// any rule blocks (a failure can hit before a blocking rule's value is
// scanned), and never for content the proxy cannot inspect (policy.unsupported
// is always block). Only a failure of the sanitizer itself qualifies.
func passthroughEligible(cfg *config.Config, san *transform.Sanitizer, reason string, d diag) bool {
	if cfg.Policy.Attachments == "extract_text" || cfg.Policy.OnError != "passthrough" || reason == reasonPolicy || san.HasBlockingRules() {
		return false
	}
	switch d.kind {
	case kindBadBody, kindDepth:
		return false
	}
	return true
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

	// A model-visible synthetic must already be recoverable after a process
	// crash. Never treat a persistence failure as sanitization passthrough.
	if h.d.PersistMappings != nil {
		if err := h.d.PersistMappings(r.Context()); err != nil {
			return h.block(x, reasonVault)
		}
	}

	// The decision is recorded before the request leaves; a failed write
	// blocks it when audit blocks. Nothing below this line runs otherwise.
	if err := h.record(x, x.decision, "", x.counts); err != nil && x.cfg.AuditBlocks() {
		return h.blockAudit(x, err)
	}
	if p := x.passthrough; p != nil {
		h.d.Metrics.IncPassthrough()
		h.d.Logf("cui-proxy: forwarding an unsanitized request (policy.on_error=passthrough, reason=%s kind=%s rule=%s class=%s)",
			p.reason, p.d.kind, orDash(p.d.rule), orDash(p.d.class))
	}
	var res *http.Response
	func() { defer x.trace.Measure(observe.UpstreamWait)(); res, err = h.d.Transport.RoundTrip(out) }()
	if err != nil {
		if r.Context().Err() != nil {
			return false // client went away; nobody to answer
		}
		// err carries the URL and query string: log only its kind.
		h.d.Metrics.IncError()
		h.d.Logf("cui-proxy: upstream request failed (%s)", errKind(err))
		writeError(x, http.StatusBadGateway, "upstream", "cui_proxy_error", "upstream unavailable")
		return false
	}
	defer res.Body.Close()

	if res.StatusCode >= 300 && res.StatusCode < 400 && res.StatusCode != http.StatusNotModified {
		// Never relay Location or the redirect body.
		h.d.Metrics.IncError()
		x.entry.Blocked = reasonUpstreamRedirect
		h.d.Logf("cui-proxy: upstream answered with a redirect (%d); response blocked", res.StatusCode)
		writeError(x, http.StatusBadGateway, reasonUpstreamRedirect, "cui_proxy_error", "upstream redirected the request")
		return false
	}
	// The live Codex backend can omit Content-Type on SSE responses. Probe
	// only unencoded, unlabelled sanitize-route responses, retaining every
	// byte for either the rewriter or the ordinary response copy.
	var responseBody io.Reader = res.Body
	if route.Kind == adapter.KindSanitize && res.Header.Get("Content-Type") == "" && !encoded(res.Header) {
		buffered := bufio.NewReader(res.Body)
		prefix, _ := buffered.Peek(32)
		prefix = bytes.TrimPrefix(prefix, []byte{0xef, 0xbb, 0xbf})
		prefix = bytes.TrimLeft(prefix, "\r\n")
		for _, field := range []string{"data:", "event:", ":", "id:", "retry:"} {
			if bytes.HasPrefix(prefix, []byte(field)) {
				res.Header.Set("Content-Type", "text/event-stream")
				break
			}
		}
		responseBody = buffered
	}
	// Successful model responses must pass through restoration. Never relay
	// an encoded or unsupported success body as executable model output.
	needsRestore := route.Kind == adapter.KindSanitize && res.StatusCode >= 200 && res.StatusCode < 300
	if needsRestore && (!isSSE(res.Header) || encoded(res.Header)) {
		h.d.Metrics.IncError()
		x.entry.Blocked = reasonStreamRewrite
		h.d.Logf("cui-proxy: upstream response format cannot be restored; response blocked")
		writeError(x, blockStatus[reasonStreamRewrite], reasonStreamRewrite, "cui_proxy_blocked", blockMessage[reasonStreamRewrite])
		return false
	}
	if !isSSE(res.Header) {
		copyResponseHeader(x.w.Header(), res.Header)
		x.w.Header().Set(requestIDHeader, x.id.String())
		x.w.WriteHeader(res.StatusCode)
		if _, err := io.Copy(x.w, responseBody); err != nil && r.Context().Err() == nil {
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
		writeError(x, blockStatus[reasonStreamRewrite], reasonStreamRewrite, "cui_proxy_blocked", blockMessage[reasonStreamRewrite])
		return false
	}
	copyResponseHeader(x.w.Header(), res.Header)
	x.w.Header().Set(requestIDHeader, x.id.String())
	x.w.Header().Del("Content-Length")
	x.w.Header().Set("Cache-Control", "no-store")
	x.w.WriteHeader(res.StatusCode)
	return h.stream(x, r, responseBody, rest)
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
			var evs []sse.Event
			var err error
			func() { defer x.trace.Measure(observe.Restore)(); evs, err = rw.Flush() }()
			x.overhead += time.Since(t0)
			if err != nil {
				return fail("rewrite")
			}
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
		var evs []sse.Event
		func() { defer x.trace.Measure(observe.Restore)(); evs, err = rw.Event(ev) }()
		x.overhead += time.Since(t0)
		if err != nil {
			return fail("rewrite")
		}
		if err := emit(evs, ev.Data, false); err != nil {
			return fail("write")
		}
	}
}

// record writes the request's decision record, at most once per request.
// A failure is counted and logged (rate-limited, by class only); whether it
// blocks is the caller's call (cfg.AuditBlocks for a forward; a blocked
// request stays blocked either way).
func (h *handler) record(x *exchange, dec decisionlog.Decision, reason string, counts map[string]int) error {
	if x.recorded {
		return nil
	}
	x.recorded = true
	profile := "unknown"
	if x.cfg != nil {
		profile = x.cfg.Policy.Profile
		if profile == "" {
			profile = config.ProfileSanitize
		}
	}
	e := decisionlog.Entry{
		RequestID: x.id, Adapter: h.d.Adapter.Name(), Profile: profile,
		Decision: dec, Reason: reason, Counts: counts, Detail: x.detail,
	}
	if x.passthrough != nil && dec == decisionlog.Forwarded {
		e.Passthrough = x.passthrough.reason
	}
	x.recordedAs = dec
	var err error
	func() { defer x.trace.Measure(observe.AuditWrite)(); err = h.d.Decisions.Record(e) }()
	if err == nil {
		return nil
	}
	h.d.Metrics.IncAuditFailed()
	outcome := "request blocked"
	if dec != decisionlog.Blocked && x.cfg != nil && !x.cfg.AuditBlocks() {
		outcome = "request forwarded anyway (audit.on_error=warn)"
	}
	if n, ok := h.auditWarn.allow(time.Now()); ok {
		h.d.Logf("cui-proxy: WARN decision record not written (%s); %s; %d earlier failure(s) not logged", decisionlog.ErrClass(err), outcome, n)
	}
	return err
}

// blockAudit refuses a request whose decision could not be recorded. It is
// not recorded itself (record already ran, and failed).
func (h *handler) blockAudit(x *exchange, err error) bool {
	class := decisionlog.ErrClass(err)
	x.entry.Blocked = reasonAudit
	h.d.Metrics.IncBlocked(reasonAudit)
	h.d.Logf("cui-proxy: request blocked (reason=%s kind=%s)", reasonAudit, class)
	writeError(x, blockStatus[reasonAudit], reasonAudit, "cui_proxy_blocked", "request blocked (reason="+reasonAudit+" kind="+class+"): "+blockMessage[reasonAudit])
	return false
}

// rateLimit lets one log line through per auditWarnEvery and counts the rest.
type rateLimit struct {
	mu         sync.Mutex
	last       time.Time
	suppressed int
}

const auditWarnEvery = time.Minute

func (l *rateLimit) allow(now time.Time) (suppressed int, ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.last.IsZero() && now.Sub(l.last) < auditWarnEvery {
		l.suppressed++
		return 0, false
	}
	n := l.suppressed
	l.last, l.suppressed = now, 0
	return n, true
}

// block answers with the reason's error and counts it.
func (h *handler) block(x *exchange, reason string) bool {
	_ = h.record(x, decisionlog.Blocked, reason, nil)
	x.entry.Blocked = reason
	h.d.Metrics.IncBlocked(reason)
	h.d.Logf("cui-proxy: request blocked (reason=%s)", reason)
	writeError(x, blockStatus[reason], reason, "cui_proxy_blocked", blockMessage[reason])
	return false
}

// blockDiag blocks a request whose body could not be sanitized. Beyond
// block, it names the closed-set kind and the rule (config name, cleaned)
// in the log line, the audit entry (Blocked "<reason>:<kind>" and Rules
// "blocked:<rule>") and the 422 message. Metrics keys stay the reason.
func (h *handler) blockDiag(x *exchange, reason string, d diag) bool {
	x.detail = d.detail
	x.recoveryKind = d.kind
	var counts map[string]int
	if d.rule != "" {
		counts = map[string]int{d.rule: 1}
	}
	_ = h.record(x, decisionlog.Blocked, reason, counts)
	x.entry.Blocked = reason
	if d.kind != reason {
		x.entry.Blocked = reason + ":" + d.kind
	}
	if d.rule != "" {
		x.entry.Rules = map[string]int{"blocked:" + d.rule: 1}
	}
	h.d.Metrics.IncBlocked(reason)
	suffix := ""
	if d.detail != "" {
		suffix = " detail=" + d.detail + ". " + attachment.Explain(d.detail)
	}
	h.d.Logf("cui-proxy: request blocked (reason=%s kind=%s rule=%s class=%s)%s", reason, d.kind, orDash(d.rule), orDash(d.class), suffix)
	msg := "request blocked (reason=" + reason + " kind=" + d.kind
	if d.rule != "" {
		msg += " rule=" + d.rule
	}
	msg += ")"
	if d.detail != "" {
		msg += " detail=" + d.detail + ". " + attachment.Explain(d.detail)
	}
	writeError(x, blockStatus[reason], reason, "cui_proxy_blocked", msg)
	return false
}

// writeError writes an OpenAI-style JSON error. Its text is fixed per code
// (plus, from blockDiag, closed-set kinds and safeName-cleaned rule names)
// and never contains request data. The message ends with the request id, for
// `cui-proxy explain`, which is also in the request_id field and header.
func writeError(x *exchange, status int, code, typ, msg string) {
	id := x.id.String()
	h := x.w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set(requestIDHeader, id)
	guidance := recovery.Lookup(code, x.recoveryKind, x.detail)
	if x.recurrence != nil {
		guidance.Occurrences = x.recurrence.Record(guidance.Code, time.Now())
	}
	h.Set("X-Cui-Proxy-Recovery-Code", guidance.Code)
	h.Set("X-Cui-Proxy-Retry", guidance.Retry)
	x.w.WriteHeader(status)
	_ = json.NewEncoder(x.w).Encode(map[string]any{"error": map[string]any{
		"message": "cui-proxy: " + msg + " request_id=" + id, "type": typ, "code": code, "request_id": id, "recovery": guidance,
	}})
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
