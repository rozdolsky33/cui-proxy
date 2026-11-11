package metrics

import (
	"net/http"
	"testing"
	"time"

	"cui-proxy/internal/observe"
)

func BenchmarkRequestObservation(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		name := "aggregate_only"
		if enabled {
			name = "with_correlation"
		}
		b.Run(name, func(b *testing.B) {
			r := New()
			r.SetCorrelationEnabled(enabled)
			headers := make(http.Header)
			headers.Set("session_id", "benchmark-session")
			headers.Set("X-Cui-Workspace-ID", "benchmark-workspace")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				endRequest := r.BeginRequest()
				token := r.Correlate(headers)
				tr := observe.NewTrace()
				for _, stage := range []observe.Stage{observe.IngressRead, observe.MetadataInspect, observe.AuditWrite, observe.UpstreamWait, observe.Restore} {
					tr.Measure(stage)()
				}
				endBody := tr.Body()
				endExtraction := tr.Extraction()
				tr.Measure(observe.ExtractionWait)()
				endExtraction()
				endBody()
				snapshot := tr.Snapshot()
				r.ObserveStages(snapshot, 1024)
				r.ObserveCorrelated(token, snapshot)
				endRequest()
			}
		})
	}
}

func BenchmarkFullStageSnapshot(b *testing.B) {
	r := New()
	s := observe.Snapshot{}
	for i := range s.Observed {
		s.Observed[i], s.Duration[i] = true, time.Microsecond
	}
	for range reservoirSize {
		r.ObserveStages(s, 1024)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = r.Snapshot()
	}
}
