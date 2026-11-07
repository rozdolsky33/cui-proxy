package metrics

import (
	"math"
	"sync"
	"testing"
	"time"

	"cui-proxy/internal/transform"
)

var _ transform.Sink = (*Registry)(nil)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newTest() (*Registry, *fakeClock) {
	c := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	return newWithClock(c.now), c
}

func TestSnapshotCountsByRule(t *testing.T) {
	r, c := newTest()
	r.Record(transform.Event{Rule: "email", Class: "email", Fingerprint: "abcd1234", Count: 2})
	r.Record(transform.Event{Rule: "email", Count: 1})
	r.Record(transform.Event{Rule: "ipv4", Count: 4})
	r.IncRequest()
	r.IncRequest()
	r.IncBlocked("oversize")
	r.IncBlocked("oversize")
	r.IncBlocked("unrestorable")
	r.IncError()
	r.IncRestore(3)
	r.IncRestore(0)
	r.IncRestore(-1)
	r.SetVaultSize(7)
	c.add(90 * time.Second)
	s := r.Snapshot()
	if s.Replacements != 7 || s.ReplacementsByRule["email"] != 3 || s.ReplacementsByRule["ipv4"] != 4 {
		t.Fatalf("replacements: %+v", s)
	}
	if s.Requests != 2 || s.Blocked != 3 || s.BlockedByReason["oversize"] != 2 || s.BlockedByReason["unrestorable"] != 1 {
		t.Fatalf("totals: %+v", s)
	}
	if s.Errors != 1 || s.Restores != 3 || s.VaultSize != 7 || s.UptimeSeconds != 90 {
		t.Fatalf("misc: %+v", s)
	}
	// Snapshot maps are copies.
	s.ReplacementsByRule["email"] = 99
	if r.Snapshot().ReplacementsByRule["email"] != 3 {
		t.Fatal("snapshot aliases internal map")
	}
}

func TestPercentilesOnKnownSamples(t *testing.T) {
	r, _ := newTest()
	if s := r.Snapshot(); s.OverheadMS.P50 != 0 || s.OverheadMS.P95 != 0 || s.OverheadMS.Samples != 0 {
		t.Fatalf("empty: %+v", s.OverheadMS)
	}
	for i := 100; i >= 1; i-- {
		r.ObserveOverhead(time.Duration(i) * time.Millisecond)
	}
	o := r.Snapshot().OverheadMS
	if math.Abs(o.P50-50) > 1 || math.Abs(o.P95-95) > 1 || o.Samples != 100 {
		t.Fatalf("got %+v", o)
	}
}

func TestReservoirBounded(t *testing.T) {
	r, _ := newTest()
	for i := 0; i < reservoirSize*2; i++ {
		r.ObserveOverhead(time.Millisecond)
	}
	if n := r.Snapshot().OverheadMS.Samples; n != reservoirSize {
		t.Fatalf("samples=%d", n)
	}
	// Old samples are evicted.
	for i := 0; i < reservoirSize; i++ {
		r.ObserveOverhead(10 * time.Millisecond)
	}
	if o := r.Snapshot().OverheadMS; o.P50 != 10 {
		t.Fatalf("p50=%v", o.P50)
	}
}

func TestRequestsPerMinuteWindow(t *testing.T) {
	r, c := newTest()
	for i := 0; i < 5; i++ {
		r.IncRequest()
	}
	c.add(30 * time.Second)
	for i := 0; i < 3; i++ {
		r.IncRequest()
	}
	if got := r.Snapshot().RequestsPerMinute; got != 8 {
		t.Fatalf("t+30: %d", got)
	}
	c.add(31 * time.Second) // first 5 are 61s old
	if got := r.Snapshot().RequestsPerMinute; got != 3 {
		t.Fatalf("t+61: %d", got)
	}
	c.add(10 * time.Minute)
	if got := r.Snapshot().RequestsPerMinute; got != 0 {
		t.Fatalf("t+10m: %d", got)
	}
	r.IncRequest()
	if got := r.Snapshot().RequestsPerMinute; got != 1 {
		t.Fatalf("after reuse: %d", got)
	}
}

func TestConcurrent(t *testing.T) {
	r, _ := newTest()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				r.IncRequest()
				r.Record(transform.Event{Rule: "x", Count: 1})
				r.ObserveOverhead(time.Millisecond)
				r.IncBlocked("b")
				_ = r.Snapshot()
			}
		}()
	}
	wg.Wait()
	if s := r.Snapshot(); s.Requests != 1600 || s.Replacements != 1600 {
		t.Fatalf("%+v", s)
	}
}

func TestIncPassthrough(t *testing.T) {
	r, _ := newTest()
	r.IncPassthrough()
	r.IncPassthrough()
	s := r.Snapshot()
	if s.Passthrough != 2 {
		t.Errorf("passthrough = %d, want 2", s.Passthrough)
	}
	if s.Blocked != 0 {
		t.Errorf("passthrough must not count as blocked: %d", s.Blocked)
	}
}
