package observe

import (
	"context"
	"testing"
	"time"
)

func TestExtractionExcludedFromBodyInspection(t *testing.T) {
	now := time.Unix(0, 0)
	tr := newTrace(func() time.Time { return now })
	endBody := tr.Body()
	now = now.Add(2 * time.Millisecond)
	endExtraction := tr.Extraction()
	now = now.Add(3 * time.Millisecond)
	endWait := tr.Measure(ExtractionWait)
	now = now.Add(5 * time.Millisecond)
	endWait()
	now = now.Add(7 * time.Millisecond)
	endExtraction()
	now = now.Add(11 * time.Millisecond)
	endBody()
	s := tr.Snapshot()
	if s.Duration[BodyInspect] != 13*time.Millisecond || s.Duration[ExtractionWait] != 5*time.Millisecond || s.Duration[ExtractionRun] != 10*time.Millisecond {
		t.Fatal("nested extraction was not accounted for exclusively")
	}
}

func TestRepeatedStagesAccumulateAndAbsentStagesStayAbsent(t *testing.T) {
	now := time.Unix(0, 0)
	tr := newTrace(func() time.Time { return now })
	for range 2 {
		end := tr.Measure(Restore)
		now = now.Add(3 * time.Millisecond)
		end()
		end() // A deferred callback must not double-count if also closed early.
	}
	s := tr.Snapshot()
	if s.Duration[Restore] != 6*time.Millisecond || !s.Observed[Restore] || s.Observed[UpstreamWait] {
		t.Fatal("repeated or absent stage accounting is incorrect")
	}
}

func TestContextKeepsIndependentRequestTraces(t *testing.T) {
	a, b := NewTrace(), NewTrace()
	ctx := WithTrace(context.Background(), a)
	child := WithTrace(ctx, b)
	if FromContext(ctx) != a || FromContext(child) != b || FromContext(context.Background()) != nil {
		t.Fatal("request traces crossed context boundaries")
	}
	FromContext(context.Background()).Measure(BodyInspect)()
	FromContext(context.Background()).Extraction()()
	FromContext(context.Background()).Body()()
}

func TestSequentialExtractionsKeepTheirOwnWaitTime(t *testing.T) {
	now := time.Unix(0, 0)
	tr := newTrace(func() time.Time { return now })
	endBody := tr.Body()
	for range 2 {
		endExtraction := tr.Extraction()
		endWait := tr.Measure(ExtractionWait)
		now = now.Add(4 * time.Millisecond)
		endWait()
		now = now.Add(6 * time.Millisecond)
		endExtraction()
	}
	endBody()
	s := tr.Snapshot()
	if s.Duration[ExtractionRun] != 12*time.Millisecond || s.Duration[ExtractionWait] != 8*time.Millisecond || s.Duration[BodyInspect] != 0 || !s.Observed[BodyInspect] {
		t.Fatal("extraction waits leaked across attachments")
	}
}
