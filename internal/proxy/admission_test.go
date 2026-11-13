package proxy

import (
	"bufio"
	"context"
	"cui-proxy/internal/admission"
	"cui-proxy/internal/decisionlog"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type countedBody struct {
	reads int
	io.Reader
}

func (b *countedBody) Read(p []byte) (int, error) { b.reads++; return b.Reader.Read(p) }
func (b *countedBody) Close() error               { return nil }

func saturatedAdmission(t *testing.T) func() {
	t.Helper()
	var release []func()
	for range 8 {
		r, err := admission.Requests.Acquire(context.Background(), 0)
		if err != nil {
			t.Fatal(err)
		}
		release = append(release, r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, _ := admission.Requests.Acquire(ctx, 0)
			if r != nil {
				r()
			}
		}()
	}
	deadline := time.Now().Add(time.Second)
	for admission.Requests.Snapshot().Queued != 8 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if admission.Requests.Snapshot().Queued != 8 {
		t.Fatal("queue did not saturate")
	}
	return func() {
		cancel()
		wg.Wait()
		for _, r := range release {
			r()
		}
	}
}

func TestAdmissionOverloadBeforeBodyAndPassthrough(t *testing.T) {
	var deps *Deps
	h := newHarness(t, nil, func(h *harness, d *Deps) { h.cfg.Policy.OnError = "passthrough"; deps = d })
	defer h.close()
	release := saturatedAdmission(t)
	defer release()
	body := &countedBody{Reader: strings.NewReader(userBody("ordinary text"))}
	r := h.req(http.MethodPost, "/v1/responses", "")
	r.Body = body
	r.ContentLength = -1
	w := httptest.NewRecorder()
	New(*deps).ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") != "1" {
		t.Fatalf("status/header: %d %q", w.Code, w.Header().Get("Retry-After"))
	}
	if body.reads != 0 {
		t.Fatalf("read overloaded body %d times", body.reads)
	}
	if !strings.Contains(w.Body.String(), `"code":"overload"`) {
		t.Fatal("missing overload code")
	}
	h.noUpstreamCall()
}

func TestAdmissionCapsFalseLength(t *testing.T) {
	var deps *Deps
	h := newHarness(t, nil, func(_ *harness, d *Deps) { deps = d })
	defer h.close()
	r := h.req(http.MethodPost, "/v1/responses", userBody("ordinary text"))
	r.ContentLength = 1
	w := httptest.NewRecorder()
	New(*deps).ServeHTTP(w, r)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("false length status: %d", w.Code)
	}
	h.noUpstreamCall()
	if s := admission.Requests.Snapshot(); s != (admission.State{}) {
		t.Fatalf("reservation leaked: %+v", s)
	}
}

func TestAdmissionSmallRequestProgressAtBulkBound(t *testing.T) {
	h := newHarness(t, nil)
	defer h.close()
	a, err := admission.Requests.Acquire(context.Background(), 28<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer a()
	b, err := admission.Requests.Acquire(context.Background(), 28<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer b()
	res, _ := h.post(userBody("ordinary text"))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("small request blocked: %d", res.StatusCode)
	}
	h.upstreamGot()
}

func TestAdmissionUnknownLengthAndCancellation(t *testing.T) {
	var deps *Deps
	h := newHarness(t, nil, func(_ *harness, d *Deps) { deps = d })
	defer h.close()
	r := h.req(http.MethodPost, "/v1/responses", userBody("ordinary text"))
	r.ContentLength = -1
	w := httptest.NewRecorder()
	New(*deps).ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("unknown length status: %d", w.Code)
	}
	h.upstreamGot()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r = h.req(http.MethodPost, "/v1/responses", "").WithContext(ctx)
	body := &countedBody{Reader: strings.NewReader(userBody("ordinary text"))}
	r.Body = body
	r.ContentLength = -1
	w = httptest.NewRecorder()
	New(*deps).ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable || body.reads != 0 {
		t.Fatalf("cancel status=%d reads=%d", w.Code, body.reads)
	}
	if s := admission.Requests.Snapshot(); s != (admission.State{}) {
		t.Fatalf("reservation leaked: %+v", s)
	}
}

func TestAdmissionHeldUntilStreamCancellation(t *testing.T) {
	started := make(chan struct{})
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		sseHeaders(w)
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	})
	defer h.close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := h.req(http.MethodPost, "/v1/responses", userBody("ordinary text")).WithContext(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		res, err := h.client.Do(r)
		if err == nil {
			defer res.Body.Close()
			_, _ = io.Copy(io.Discard, res.Body)
		}
	}()
	<-started
	if s := admission.Requests.Snapshot(); s.Active != 1 || s.Bytes <= 0 {
		t.Fatalf("stream not charged: %+v", s)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("client did not cancel")
	}
	deadline := time.Now().Add(time.Second)
	for admission.Requests.Snapshot() != (admission.State{}) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if s := admission.Requests.Snapshot(); s != (admission.State{}) {
		t.Fatalf("cancelled stream retained reservation: %+v", s)
	}
}

func TestAdmissionQueuedRequestCancellation(t *testing.T) {
	var release []func()
	for range 8 {
		r, _ := admission.Requests.Acquire(context.Background(), 0)
		release = append(release, r)
	}
	defer func() {
		for _, r := range release {
			r()
		}
	}()
	var deps *Deps
	h := newHarness(t, nil, func(_ *harness, d *Deps) { deps = d })
	defer h.close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := h.req(http.MethodPost, "/v1/responses", "").WithContext(ctx)
	body := &countedBody{Reader: strings.NewReader(userBody("ordinary text"))}
	r.Body = body
	r.ContentLength = -1
	done := make(chan struct{})
	go func() { defer close(done); New(*deps).ServeHTTP(httptest.NewRecorder(), r) }()
	deadline := time.Now().Add(time.Second)
	for admission.Requests.Snapshot().Queued != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if admission.Requests.Snapshot().Queued != 1 {
		t.Fatal("request did not queue")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancellation did not drain")
	}
	if body.reads != 0 || admission.Requests.Snapshot().Queued != 0 {
		t.Fatal("cancelled request read body or retained queue slot")
	}
	h.noUpstreamCall()
}

type failedAdmissionAudit struct{}

func (failedAdmissionAudit) Record(decisionlog.Entry) error { return errors.New("unavailable") }

func TestAdmissionAuditFailureStillDoesNotForward(t *testing.T) {
	var deps *Deps
	h := newHarness(t, nil, func(h *harness, d *Deps) {
		h.cfg.Policy.OnError = "passthrough"
		d.Decisions = failedAdmissionAudit{}
		deps = d
	})
	defer h.close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := h.req(http.MethodPost, "/v1/responses", userBody("ordinary text")).WithContext(ctx)
	w := httptest.NewRecorder()
	New(*deps).ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("audit failure changed overload: %d", w.Code)
	}
	h.noUpstreamCall()
	s := h.reg.Snapshot()
	if s.AuditWriteFailed != 1 || s.Admission != (admission.State{}) || s.StagesMS["admission_wait"].Samples != 1 {
		t.Fatal("missing safe audit/admission instrumentation")
	}
}

func TestAdmissionRefusesSlowHTTPBodyWithoutDraining(t *testing.T) {
	h := newHarness(t, nil)
	defer h.close()
	release := saturatedAdmission(t)
	defer release()
	r := h.req(http.MethodPost, "/v1/responses", "")
	conn, err := net.Dial("tcp", r.URL.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	// Announce a body but deliberately send none. A pre-read overload reply
	// must not wait for net/http's implicit keep-alive body drain.
	_, err = io.WriteString(conn, "POST /v1/responses HTTP/1.1\r\nHost: "+r.URL.Host+"\r\nContent-Length: 4096\r\n\r\n")
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.ReadResponse(bufio.NewReader(conn), r)
	if err != nil {
		t.Fatal("overload response waited for the unread body")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusServiceUnavailable || !res.Close {
		t.Fatalf("status=%d close=%v", res.StatusCode, res.Close)
	}
	h.noUpstreamCall()
}
