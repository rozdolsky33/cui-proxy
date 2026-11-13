package metrics

import (
	"cui-proxy/internal/observe"
	"testing"
)

func TestImageCacheHitMetrics(t *testing.T) {
	r := New()
	trace := observe.NewTrace()
	trace.ImageCacheHit()
	s := trace.Snapshot()
	r.ObserveStages(s, 100)
	if got := r.Snapshot(); got.ExtractionCacheHits != 1 || len(got.RecentRequests) != 0 {
		t.Fatal("aggregate counter or disabled correlation changed")
	}
	r.SetCorrelationEnabled(true)
	r.ObserveRecent(Identity{Session: "cache-session"}, s)
	got := r.Snapshot()
	if len(got.RecentRequests) != 1 || got.RecentRequests[0].ExtractionCacheHits != 1 {
		t.Fatal("request cache count missing")
	}
}
