package metrics

import (
	"cui-proxy/internal/observe"
	"sort"
	"sync"
	"time"
)

type StageLatency struct {
	P50     float64 `json:"p50"`
	P95     float64 `json:"p95"`
	P99     float64 `json:"p99"`
	Samples int     `json:"samples"`
}
type stageRing struct {
	values  [reservoirSize]time.Duration
	next, n int
}

func (r *Registry) BeginRequest() func() {
	r.activeRequests.Add(1)
	var once sync.Once
	return func() { once.Do(func() { r.activeRequests.Add(-1) }) }
}
func (r *Registry) ExtractorQueued(delta int64) { r.extractorQueued.Add(delta) }
func (r *Registry) ExtractorActive(delta int64) { r.extractorActive.Add(delta) }

var sizeNames = [5]string{"le_4096", "le_65536", "le_1048576", "le_16777216", "gt_16777216"}

func (r *Registry) ObserveStages(s observe.Snapshot, bytes int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.extractionCacheHits += s.ExtractionCacheHits
	for i, seen := range s.Observed {
		if !seen {
			continue
		}
		v := &r.stageRings[i]
		v.values[v.next] = s.Duration[i]
		v.next = (v.next + 1) % reservoirSize
		if v.n < reservoirSize {
			v.n++
		}
	}
	bucket := 0
	for _, limit := range []int{4096, 65536, 1048576, 16777216} {
		if bytes <= limit {
			break
		}
		bucket++
	}
	r.sizeBuckets[bucket]++
}
func snapshotStages(rings [len(observe.Names)]stageRing, s *Snapshot) {
	s.StagesMS = make(map[string]StageLatency)
	for i, v := range rings {
		if v.n == 0 {
			continue
		}
		values := append([]time.Duration(nil), v.values[:v.n]...)
		sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
		s.StagesMS[observe.Names[i]] = StageLatency{pct(values, 50), pct(values, 95), pct(values, 99), v.n}
	}
}
func (r *Registry) snapshotSizesAndRecent(s *Snapshot) {
	s.ExtractionCacheHits = r.extractionCacheHits
	s.RequestSizeBuckets = make(map[string]int64, 5)
	for i, n := range r.sizeBuckets {
		s.RequestSizeBuckets[sizeNames[i]] = n
	}
	r.snapshotRecent(s)
}
