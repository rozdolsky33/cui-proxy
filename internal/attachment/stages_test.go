package attachment

import (
	"context"
	"cui-proxy/internal/metrics"
	"cui-proxy/internal/observe"
	"testing"
	"time"
)

func TestExtractionWaitCancellationGauges(t *testing.T) {
	slots <- struct{}{}
	slots <- struct{}{}
	defer func() { <-slots; <-slots }()
	r := metrics.New()
	tr := observe.NewTrace()
	tr.Gauges = r
	ctx, cancel := context.WithCancel(observe.WithTrace(context.Background(), tr))
	e := New(ctx)
	defer e.Close()
	done := make(chan Cause, 1)
	go func() { _, why := e.run("unused", nil, 1); done <- why }()
	deadline := time.Now().Add(time.Second)
	for r.Snapshot().ExtractorQueued != 1 {
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("never queued")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if <-done != Busy {
		t.Fatal("cancellation not respected")
	}
	s := r.Snapshot()
	if s.ExtractorQueued != 0 || s.ExtractorActive != 0 {
		t.Fatal("gauge leak")
	}
	if !tr.Snapshot().Observed[observe.ExtractionWait] {
		t.Fatal("wait absent")
	}
}
func TestFullExtractionIncludesEarlyFailure(t *testing.T) {
	tr := observe.NewTrace()
	e := New(observe.WithTrace(context.Background(), tr))
	defer e.Close()
	_, _ = e.Extract(Image, "invalid")
	s := tr.Snapshot()
	if !s.Observed[observe.ExtractionRun] || s.Observed[observe.ExtractionWait] {
		t.Fatal("incorrect extraction boundary")
	}
}
