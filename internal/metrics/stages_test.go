package metrics

import (
	"cui-proxy/internal/observe"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStageBoundsAndAbsent(t *testing.T) {
	r := New()
	tr := observe.NewTrace()
	tr.Measure(observe.IngressRead)()
	for i := 0; i < 5000; i++ {
		r.ObserveStages(tr.Snapshot(), 42)
	}
	s := r.Snapshot()
	if s.StagesMS["ingress_read"].Samples != 4096 || len(s.StagesMS) != 1 || s.RequestSizeBuckets["le_4096"] != 5000 {
		t.Fatal("bounds or absent stage")
	}
}
func TestGaugeConcurrency(t *testing.T) {
	r := New()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			done := r.BeginRequest()
			r.ExtractorQueued(1)
			r.ExtractorQueued(-1)
			r.ExtractorActive(1)
			r.ExtractorActive(-1)
			done()
		}()
	}
	wg.Wait()
	s := r.Snapshot()
	if s.ActiveRequests != 0 || s.ExtractorQueued != 0 || s.ExtractorActive != 0 {
		t.Fatal("gauge leak")
	}
}
func TestCorrelationPrivacyTTLAndDisable(t *testing.T) {
	now := time.Unix(100, 0)
	r := newWithClock(func() time.Time { return now })
	r.SetCorrelationEnabled(true)
	h := http.Header{"Session_id": []string{"session-123"}, "X-Cui-Workspace-Id": []string{"workspace-123"}}
	id := r.Identity(h)
	if id.Session == "" || id.Workspace == "" || id != r.Identity(h) {
		t.Fatal("identity unstable")
	}
	other := New()
	other.SetCorrelationEnabled(true)
	if other.Identity(h) == id {
		t.Fatal("keys reused")
	}
	r.ObserveRecent(id, observe.Snapshot{})
	b, _ := json.Marshal(r.Snapshot())
	if strings.Contains(string(b), "session-123") || strings.Contains(string(b), "workspace-123") {
		t.Fatal("raw identity exposed")
	}
	if len(r.Snapshot().RecentRequests) != 1 {
		t.Fatal("missing record")
	}
	now = now.Add(16 * time.Minute)
	if len(r.Snapshot().RecentRequests) != 0 {
		t.Fatal("expired records exposed")
	}
	r.ObserveRecent(id, observe.Snapshot{})
	r.SetCorrelationEnabled(false)
	r.SetCorrelationEnabled(true)
	if len(r.Snapshot().RecentRequests) != 0 {
		t.Fatal("disabled records retained")
	}
}
func TestCorrelationRejectsMissingInvalidAndDuplicate(t *testing.T) {
	r := New()
	r.SetCorrelationEnabled(true)
	for _, h := range []http.Header{nil, {"Session_id": []string{"a b"}}, {"Session_id": []string{"a", "b"}}} {
		if r.Identity(h).Session != "" {
			t.Fatal("invalid identity attributed")
		}
	}
	id := r.Identity(http.Header{"Session_id": []string{"valid-1"}})
	if id.Session == "" || id.Workspace != "" {
		t.Fatal("missing workspace inferred")
	}
}
func BenchmarkStageInstrumentation(b *testing.B) {
	r := New()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		done := r.BeginRequest()
		tr := observe.NewTrace()
		tr.Measure(observe.IngressRead)()
		tr.Body()()
		r.ObserveStages(tr.Snapshot(), 100)
		done()
	}
}

func TestCorrelationGenerationRejectsInflightAndBounds(t *testing.T) {
	r := New()
	r.SetCorrelationEnabled(true)
	token := r.Correlate(http.Header{"Session_id": []string{"valid"}})
	r.SetCorrelationEnabled(false)
	r.SetCorrelationEnabled(true)
	r.ObserveCorrelated(token, observe.Snapshot{})
	if len(r.Snapshot().RecentRequests) != 0 {
		t.Fatal("old in-flight record exposed")
	}
	token = r.Correlate(http.Header{"Session_id": []string{"valid"}})
	for i := 0; i < 300; i++ {
		r.ObserveCorrelated(token, observe.Snapshot{})
	}
	if len(r.Snapshot().RecentRequests) != 256 {
		t.Fatal("recent capacity")
	}
}
