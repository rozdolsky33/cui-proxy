package detect

import (
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type parallelProbe struct {
	active, peak *atomic.Int64
	entered      chan struct{}
	release      <-chan struct{}
	panicValue   any
}

func (*parallelProbe) Name() string         { return "parallel-test" }
func (*parallelProbe) Class() string        { return "secret" }
func (*parallelProbe) parallelCostly() bool { return true }
func (p *parallelProbe) Find(string) []Span {
	if p.panicValue != nil {
		panic(p.panicValue)
	}
	n := p.active.Add(1)
	defer p.active.Add(-1)
	for old := p.peak.Load(); n > old && !p.peak.CompareAndSwap(old, n); old = p.peak.Load() {
	}
	if p.entered != nil {
		p.entered <- struct{}{}
	}
	if p.release != nil {
		<-p.release
	}
	return nil
}

func TestParallelScanBoundAndFallback(t *testing.T) {
	if runtime.GOMAXPROCS(0) < 2 {
		t.Skip("single scheduler processor")
	}
	var active, peak atomic.Int64
	entered := make(chan struct{}, 128)
	release := make(chan struct{})
	ds := make([]Detector, 16)
	for i := range ds {
		ds[i] = &parallelProbe{active: &active, peak: &peak, entered: entered, release: release}
	}
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); Scan(ds, strings.Repeat("q", 8192)) }()
	}
	want := 2 + min(cap(extraScanWorkers), 2*(min(4, runtime.GOMAXPROCS(0))-1))
	for range want {
		select {
		case <-entered:
		case <-time.After(time.Second):
			close(release)
			wg.Wait()
			t.Fatal("parallel workers did not start")
		}
	}
	if active.Load() > int64(want) {
		t.Fatal("worker budget exceeded")
	}
	close(release)
	wg.Wait()
	if peak.Load() > int64(want) || len(extraScanWorkers) != 0 {
		t.Fatal("worker budget exceeded or leaked")
	}
	for range cap(extraScanWorkers) {
		extraScanWorkers <- struct{}{}
	}
	defer func() {
		for range cap(extraScanWorkers) {
			<-extraScanWorkers
		}
	}()
	peak.Store(0)
	for i := range ds {
		ds[i] = &parallelProbe{active: &active, peak: &peak}
	}
	Scan(ds, strings.Repeat("q", 8192))
	if peak.Load() != 1 {
		t.Fatal("busy worker budget did not fall back to sequential scan")
	}
}

func TestParallelScanPreservesOrderAndBlocking(t *testing.T) {
	ds := builtinSet("parallel")
	a, _ := newRegex("first", "secret", `marker-[0-9]+`, "")
	b, _ := newRegex("second", "secret", `marker-[0-9]+`, "")
	ds = append(ds, a, blocking{b})
	s := strings.Repeat("ordinary text ", 1000) + "marker-7"
	var spans []Span
	for _, d := range ds {
		spans = append(spans, d.Find(s)...)
	}
	want := resolve(spans)
	for range 20 {
		if got := Scan(ds, s); !reflect.DeepEqual(got, want) {
			t.Fatal("parallel scan changed tie ordering or blocking")
		}
	}
}

func TestParallelPanicReturnsToCallerAndReleasesWorkers(t *testing.T) {
	var active, peak atomic.Int64
	ds := make([]Detector, 16)
	for i := range ds {
		ds[i] = &parallelProbe{active: &active, peak: &peak}
	}
	tag := new(int)
	ds[1] = &parallelProbe{panicValue: tag}
	var recovered any
	func() { defer func() { recovered = recover() }(); Scan(ds, strings.Repeat("q", 8192)) }()
	if recovered != tag || len(extraScanWorkers) != 0 || active.Load() != 0 {
		t.Fatal("worker panic escaped caller or leaked capacity")
	}
}

func FuzzParallelScanDifferential(f *testing.F) {
	f.Add("ordinary text marker-7")
	f.Add("\xff Unicode 界 marker-8")
	ds := builtinSet("parallel-fuzz")
	d, _ := newRegex("marker", "secret", `marker-[0-9]+`, "")
	ds = append(ds, blocking{d})
	f.Fuzz(func(t *testing.T, value string) {
		if len(value) > 65536 {
			return
		}
		s := strings.Repeat("ordinary context ", 256) + value
		var spans []Span
		for _, d := range ds {
			spans = append(spans, d.Find(s)...)
		}
		if !reflect.DeepEqual(Scan(ds, s), resolve(spans)) {
			t.Fatal("parallel scan changed reference spans")
		}
	})
}

type cheapProbe struct{ parallelProbe }

func (*cheapProbe) parallelCostly() bool { return false }

type unknownProbe struct{ Detector }

func TestCheapAndUnknownScansStaySequential(t *testing.T) {
	if runtime.GOMAXPROCS(0) < 2 {
		t.Skip("single scheduler processor")
	}
	for _, unknown := range []bool{false, true} {
		var active, peak atomic.Int64
		entered := make(chan struct{}, 32)
		release := make(chan struct{})
		ds := make([]Detector, 8)
		for i := range ds {
			p := parallelProbe{active: &active, peak: &peak, entered: entered, release: release}
			if unknown {
				if i < 4 {
					ds[i] = &p
				} else {
					ds[i] = blocking{unknownProbe{&p}}
				}
			} else {
				ds[i] = &cheapProbe{p}
			}
		}
		done := make(chan struct{})
		go func() { defer close(done); Scan(ds, strings.Repeat("q", 8192)) }()
		<-entered
		parallel := false
		select {
		case <-entered:
			parallel = true
		case <-time.After(20 * time.Millisecond):
		}
		close(release)
		<-done
		if parallel {
			t.Fatal("cheap or unknown detectors borrowed workers")
		}
	}
}

// A slow detector must not strand the remainder of its fixed partition.
func TestParallelScanBalancesUnevenDetectors(t *testing.T) {
	if runtime.GOMAXPROCS(0) < 2 {
		t.Skip("single scheduler processor")
	}
	var active, peak atomic.Int64
	entered := make(chan struct{}, 32)
	release := make(chan struct{})
	ds := make([]Detector, 16)
	for i := range ds {
		ds[i] = &parallelProbe{active: &active, peak: &peak, entered: entered}
	}
	ds[0] = &parallelProbe{active: &active, peak: &peak, entered: entered, release: release}
	done := make(chan struct{})
	go func() { defer close(done); Scan(ds, strings.Repeat("q", 8192)) }()
	defer func() { close(release); <-done }()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for range len(ds) {
		select {
		case <-entered:
		case <-timer.C:
			t.Fatal("idle workers left detectors stranded behind a slow detector")
		}
	}
}
