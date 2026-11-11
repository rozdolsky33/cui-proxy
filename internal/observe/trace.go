// Package observe records bounded, request-local timing information.
package observe

import (
	"context"
	"sync"
	"time"
)

// Stage identifies a fixed request-processing stage. Request values must never
// be used as stage names.
type Stage uint8

const (
	IngressRead Stage = iota
	MetadataInspect
	BodyInspect
	ExtractionWait
	ExtractionRun
	AuditWrite
	UpstreamWait
	Restore
	stageCount
)

var Names = [...]string{"ingress_read", "metadata_inspect", "body_inspect", "extraction_wait", "extraction_run", "audit_write", "upstream_wait", "restore"}

// ExtractorGauges is the narrow process-wide gauge sink carried by a trace.
type ExtractorGauges interface {
	ExtractorQueued(int64)
	ExtractorActive(int64)
}

// Snapshot distinguishes an unobserved stage from an observed zero duration.
type Snapshot struct {
	Duration [stageCount]time.Duration
	Observed [stageCount]bool
}

// Trace accumulates timings for one request without retaining request data.
// Extraction spans within a request are sequential; Body excludes their full
// duration, and Extraction excludes time spent waiting for an extraction slot.
type Trace struct {
	Gauges          ExtractorGauges
	mu              sync.Mutex
	now             func() time.Time
	stages          Snapshot
	extractionTotal time.Duration
}

func NewTrace() *Trace { return newTrace(time.Now) }

func newTrace(now func() time.Time) *Trace { return &Trace{now: now} }

// Measure begins a stage span. Its returned function records at most once.
// A nil trace is an inexpensive no-op for callers without instrumentation.
func (t *Trace) Measure(stage Stage) func() {
	if t == nil || stage >= stageCount {
		return func() {}
	}
	start := t.now()
	var once sync.Once
	return func() {
		once.Do(func() {
			duration := nonnegative(t.now().Sub(start))
			t.mu.Lock()
			defer t.mu.Unlock()
			t.add(stage, duration)
		})
	}
}

// Body measures body inspection excluding completed extraction spans nested
// inside this span. End extraction spans before ending the body span.
func (t *Trace) Body() func() {
	if t == nil {
		return func() {}
	}
	start := t.now()
	t.mu.Lock()
	extractionStart := t.extractionTotal
	t.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			elapsed := t.now().Sub(start)
			t.mu.Lock()
			defer t.mu.Unlock()
			t.add(BodyInspect, nonnegative(elapsed-(t.extractionTotal-extractionStart)))
		})
	}
}

// Extraction measures extraction work excluding nested ExtractionWait spans.
func (t *Trace) Extraction() func() {
	if t == nil {
		return func() {}
	}
	start := t.now()
	t.mu.Lock()
	waitStart := t.stages.Duration[ExtractionWait]
	t.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			elapsed := nonnegative(t.now().Sub(start))
			t.mu.Lock()
			defer t.mu.Unlock()
			t.extractionTotal += elapsed
			t.add(ExtractionRun, nonnegative(elapsed-(t.stages.Duration[ExtractionWait]-waitStart)))
		})
	}
}

func (t *Trace) add(stage Stage, duration time.Duration) {
	t.stages.Duration[stage] += duration
	t.stages.Observed[stage] = true
}

func (t *Trace) Snapshot() Snapshot {
	if t == nil {
		return Snapshot{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.stages
}

func nonnegative(duration time.Duration) time.Duration {
	if duration < 0 {
		return 0
	}
	return duration
}

type traceKey struct{}

func WithTrace(ctx context.Context, trace *Trace) context.Context {
	return context.WithValue(ctx, traceKey{}, trace)
}

func FromContext(ctx context.Context) *Trace {
	trace, _ := ctx.Value(traceKey{}).(*Trace)
	return trace
}
