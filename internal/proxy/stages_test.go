package proxy

import (
	"context"
	"cui-proxy/internal/adapter"
	"cui-proxy/internal/audit"
	"cui-proxy/internal/config"
	"cui-proxy/internal/decisionlog"
	"cui-proxy/internal/metrics"
	"cui-proxy/internal/observe"
	"cui-proxy/internal/sse"
	"cui-proxy/internal/transform"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type stageAdapter struct{}

func (stageAdapter) Name() string                               { return "stage" }
func (stageAdapter) Match(string, string) (adapter.Route, bool) { return adapter.Route{}, false }
func (stageAdapter) SanitizeRequest(b []byte, _ *transform.Sanitizer) ([]byte, error) {
	time.Sleep(12 * time.Millisecond)
	return b, nil
}
func (stageAdapter) NewResponseRewriter(*transform.Restorer) adapter.ResponseRewriter {
	return stageRewriter{}
}

type stageRewriter struct{}

func (stageRewriter) Event(e sse.Event) ([]sse.Event, error) {
	time.Sleep(12 * time.Millisecond)
	return []sse.Event{e}, nil
}
func (stageRewriter) Flush() ([]sse.Event, error) { time.Sleep(12 * time.Millisecond); return nil, nil }

type stageRecorder struct{}

func (stageRecorder) Record(decisionlog.Entry) error { time.Sleep(12 * time.Millisecond); return nil }
func TestControlledBodyAuditAndRestoreAttribution(t *testing.T) {
	r := metrics.New()
	h := &handler{d: Deps{Adapter: stageAdapter{}, Metrics: r, Decisions: stageRecorder{}, Logf: func(string, ...any) {}}}
	x := &exchange{trace: observe.NewTrace(), cfg: &config.Config{}, w: &countingWriter{rw: httptest.NewRecorder()}}
	_, reason, _ := h.sanitize(context.Background(), x, nil, []byte("{}"))
	if reason != "" {
		t.Fatal("sanitize failed")
	}
	if err := h.record(x, decisionlog.Forwarded, "", nil); err != nil {
		t.Fatal("record failed")
	}
	req := httptest.NewRequest("POST", "http://localhost/", nil)
	if h.stream(x, req, strings.NewReader("data: {}\n\n"), nil) {
		t.Fatal("stream aborted")
	}
	s := x.trace.Snapshot()
	for _, stage := range []observe.Stage{observe.BodyInspect, observe.AuditWrite, observe.Restore} {
		if !s.Observed[stage] || s.Duration[stage] < 10*time.Millisecond {
			t.Fatal("delay not attributed")
		}
	}
	if s.Observed[observe.UpstreamWait] || s.Observed[observe.IngressRead] {
		t.Fatal("absent stages measured")
	}
}

type delayedMetadataAdapter struct{ stageAdapter }

func (delayedMetadataAdapter) Match(string, string) (adapter.Route, bool) {
	return adapter.Route{Kind: adapter.KindSanitize, UpstreamPath: "/test"}, true
}
func (delayedMetadataAdapter) SanitizeHeaders(http.Header, *transform.Sanitizer) error {
	time.Sleep(12 * time.Millisecond)
	return nil
}
func (delayedMetadataAdapter) StructuredHeaders() []string { return nil }

type delayedBody struct{ io.Reader }

func (b delayedBody) Read(p []byte) (int, error) {
	time.Sleep(12 * time.Millisecond)
	return b.Reader.Read(p)
}
func (delayedBody) Close() error { return nil }

type delayedTransport struct{}

func (delayedTransport) RoundTrip(*http.Request) (*http.Response, error) {
	time.Sleep(12 * time.Millisecond)
	return &http.Response{StatusCode: 400, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
}
func TestControlledIngressMetadataAndUpstreamAttribution(t *testing.T) {
	r := metrics.New()
	cfg := &config.Config{}
	cfg.AllowNonLoopback = true
	u, _ := url.Parse("http://localhost")
	h := New(Deps{Adapter: delayedMetadataAdapter{}, Upstream: u, Snapshot: func() Snapshot {
		return Snapshot{cfg, transform.NewSanitizer(nil, nil, nil), transform.NewRestorer(nil)}
	}, Metrics: r, Audit: audit.New(1), Decisions: stageRecorder{}, Transport: delayedTransport{}, Logf: func(string, ...any) {}})
	req := httptest.NewRequest("POST", "http://localhost/test", nil)
	req.Body = delayedBody{strings.NewReader("{}")}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatal("unexpected response")
	}
	s := r.Snapshot()
	for _, stage := range []string{"ingress_read", "metadata_inspect", "upstream_wait"} {
		if s.StagesMS[stage].Samples != 1 || s.StagesMS[stage].P50 < 10 {
			t.Fatal("missing controlled delay")
		}
	}
	if s.ActiveRequests != 0 {
		t.Fatal("request gauge leak")
	}
}

// Fault injection exercises the handler's recovery and cancellation paths while
// leaving the production adapters and policies unchanged.
type panicBodyStageAdapter struct{ delayedMetadataAdapter }

func (panicBodyStageAdapter) SanitizeRequest([]byte, *transform.Sanitizer) ([]byte, error) {
	time.Sleep(time.Millisecond)
	panic("controlled stage fault")
}

type faultStageTransport struct {
	cancel       context.CancelFunc
	panicInstead bool
}

func (tr faultStageTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	time.Sleep(time.Millisecond)
	if tr.panicInstead {
		panic("controlled upstream fault")
	}
	tr.cancel()
	return nil, r.Context().Err()
}

type cancelledStageBody struct {
	cancel context.CancelFunc
	ctx    context.Context
}

func (b cancelledStageBody) Read([]byte) (int, error) { b.cancel(); return 0, b.ctx.Err() }
func (cancelledStageBody) Close() error               { return nil }
func TestPartialStagesAndRequestGaugeAfterPanicOrCancellation(t *testing.T) {
	cases := []struct {
		name                                    string
		bodyPanic, upstreamPanic, ingressCancel bool
		stage                                   string
	}{
		{name: "body_panic", bodyPanic: true, stage: "body_inspect"},
		{name: "upstream_panic", upstreamPanic: true, stage: "upstream_wait"},
		{name: "upstream_cancel", stage: "upstream_wait"},
		{name: "ingress_cancel", ingressCancel: true, stage: "ingress_read"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg := metrics.New()
			cfg := &config.Config{}
			u, _ := url.Parse("http://localhost")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var a adapter.Adapter = delayedMetadataAdapter{}
			if tc.bodyPanic {
				a = panicBodyStageAdapter{}
			}
			handler := New(Deps{Adapter: a, Upstream: u, Snapshot: func() Snapshot {
				return Snapshot{cfg, transform.NewSanitizer(nil, nil, nil), transform.NewRestorer(nil)}
			}, Metrics: reg, Audit: audit.New(1), Decisions: decisionlog.Discard, Transport: faultStageTransport{cancel: cancel, panicInstead: tc.upstreamPanic}, Logf: func(string, ...any) {}})
			req := httptest.NewRequest("POST", "http://localhost/test", strings.NewReader("{}")).WithContext(ctx)
			if tc.ingressCancel {
				req.Body = cancelledStageBody{cancel, ctx}
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			s := reg.Snapshot()
			if s.ActiveRequests != 0 || s.Requests != 1 {
				t.Fatal("request lifecycle metrics incomplete")
			}
			if s.StagesMS[tc.stage].Samples != 1 || s.StagesMS["metadata_inspect"].Samples != 1 {
				t.Fatal("partial timing samples not published")
			}
			if s.StagesMS["restore"].Samples != 0 {
				t.Fatal("unreached stage was measured")
			}
			if tc.bodyPanic || tc.ingressCancel {
				if s.StagesMS["upstream_wait"].Samples != 0 {
					t.Fatal("upstream was reached after local failure")
				}
			}
			if tc.bodyPanic || tc.upstreamPanic {
				if w.Code != http.StatusInternalServerError {
					t.Fatal("panic recovery changed")
				}
			} else if ctx.Err() != context.Canceled {
				t.Fatal("request context was not cancelled")
			}
		})
	}
}
