package detect

import (
	"regexp"
	"regexp/syntax"
	"runtime"
	"sync"
	"sync/atomic"
)

// Only extra workers consume tokens: every caller can always scan sequentially.
// There is no worker queue and no request data is retained between scans.
var extraScanWorkers = make(chan struct{}, min(3, max(0, runtime.GOMAXPROCS(0)-1)))

// Only the immutable detector implementations owned by this package opt in.
// Unknown implementations preserve their existing sequential call behavior.
type parallelDetector interface{ parallelCostly() bool }

func parallelEligible(ds []Detector) bool {
	costly := 0
	for _, d := range ds {
	unwrap:
		for {
			switch b := d.(type) {
			case blocking:
				d = b.Detector
			case *blocking:
				if b == nil {
					return false
				}
				d = b.Detector
			default:
				break unwrap
			}
		}
		p, ok := d.(parallelDetector)
		if !ok {
			return false
		}
		if p.parallelCostly() {
			costly++
		}
	}
	return costly >= 4
}

func regexCostly(re *regexp.Regexp) bool {
	if prefix, _ := re.LiteralPrefix(); prefix != "" {
		return false
	}
	r, err := syntax.Parse(re.String(), syntax.Perl)
	if err != nil {
		return false
	}
	program, err := syntax.Compile(r.Simplify())
	return err == nil && program.StartCond()&syntax.EmptyBeginText == 0
}

func (d *regexDetector) parallelCostly() bool { return d.costly }
func (d *dictDetector) parallelCostly() bool  { return d.re != nil }

func scanCandidates(ds []Detector, s string) []Span {
	workers := 1
	if len(s) >= 4096 && len(ds) >= 8 && parallelEligible(ds) {
		limit := min(4, runtime.GOMAXPROCS(0), len(ds))
	acquire:
		for workers < limit {
			select {
			case extraScanWorkers <- struct{}{}:
				workers++
			default:
				break acquire
			}
		}
	}
	if workers == 1 {
		var all []Span
		for _, d := range ds {
			all = append(all, d.Find(s)...)
		}
		return all
	}
	results := make([][]Span, len(ds))
	type failure struct {
		index  int
		value  any
		failed bool
	}
	failures := make([]failure, workers)
	var next atomic.Int64
	run := func(worker int) {
		index, complete := 0, false
		defer func() {
			if !complete {
				failures[worker] = failure{index: index, value: recover(), failed: true}
			}
		}()
		for {
			index = int(next.Add(1) - 1)
			if index >= len(ds) {
				break
			}
			results[index] = ds[index].Find(s)
		}
		complete = true
	}
	var wg sync.WaitGroup
	wg.Add(workers - 1)
	for worker := 1; worker < workers; worker++ {
		go func() {
			defer wg.Done()
			defer func() { <-extraScanWorkers }()
			run(worker)
		}()
	}
	run(0)
	wg.Wait()
	// A detector panic must reach the request's existing fail-closed recovery,
	// never escape a worker and terminate the process. Wait before re-panicking
	// so no worker or token outlives the failed scan.
	first := len(ds)
	var value any
	for _, f := range failures {
		if f.failed && f.index < first {
			first, value = f.index, f.value
		}
	}
	if first < len(ds) {
		panic(value)
	}
	var all []Span
	// Preserve detector order: stable overlap resolution uses it for equal ties.
	for _, spans := range results {
		all = append(all, spans...)
	}
	return all
}
