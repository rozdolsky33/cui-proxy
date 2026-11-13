package observe

import "testing"

func TestImageCacheHitRecordsKnownZeroWait(t *testing.T) {
	trace := NewTrace()
	trace.ImageCacheHit()
	s := trace.Snapshot()
	if s.ExtractionCacheHits != 1 || !s.Observed[ExtractionWait] || s.Duration[ExtractionWait] != 0 {
		t.Fatal("cache hit not distinguished from missing wait instrumentation")
	}
	if s.Observed[ExtractionRun] {
		t.Fatal("cache counter invented extraction duration")
	}
	end := trace.Measure(ExtractionWait)
	end()
	before := trace.Snapshot().Duration[ExtractionWait]
	trace.ImageCacheHit()
	s = trace.Snapshot()
	if s.ExtractionCacheHits != 2 || s.Duration[ExtractionWait] != before {
		t.Fatal("cache hit erased prior helper wait")
	}
}
