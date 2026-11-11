// Package metrics holds the proxy's in-memory counters. It never sees real
// values: only rule names, block reasons and counts.
package metrics

import (
	"sort"

	"sync"
	"sync/atomic"
	"time"

	"cui-proxy/internal/transform"
)

const (
	reservoirSize = 4096 // latency samples kept (ring buffer)
	rpmSlots      = 60   // one slot per second
)

// Snapshot is the JSON view served by the dashboard. Field names are stable
// snake_case: requests, blocked, blocked_by_reason, errors, restores,
// passthrough, replacements, replacements_by_rule, downgraded, requests_per_minute,
// overhead_ms{p50,p95,samples}, vault_size, uptime_seconds, audit_write_failed.
type Snapshot struct {
	StagesMS           map[string]StageLatency `json:"stages_ms"`
	ActiveRequests     int64                   `json:"active_requests"`
	ExtractorQueued    int64                   `json:"extractor_queued"`
	ExtractorActive    int64                   `json:"extractor_active"`
	RequestSizeBuckets map[string]int64        `json:"request_size_buckets"`
	RecentRequests     []RecentRequest         `json:"recent_requests,omitempty"`
	Requests           int64                   `json:"requests"`
	Blocked            int64                   `json:"blocked"`
	Passthrough        int64                   `json:"passthrough"`
	BlockedByReason    map[string]int64        `json:"blocked_by_reason"`
	Errors             int64                   `json:"errors"`
	Restores           int64                   `json:"restores"`
	Replacements       int64                   `json:"replacements"`
	ReplacementsByRule map[string]int64        `json:"replacements_by_rule"`
	// Downgraded counts replacements whose span was masked as class secret
	// because its own class's synthetic would not have restored in place
	// (also counted in Replacements).
	Downgraded        int64    `json:"downgraded"`
	RequestsPerMinute int64    `json:"requests_per_minute"`
	OverheadMS        Overhead `json:"overhead_ms"`
	VaultSize         int64    `json:"vault_size"`
	UptimeSeconds     int64    `json:"uptime_seconds"`
	// AuditWriteFailed counts decision records that could not be written
	// (each such request was blocked, or forwarded under audit.on_error warn).
	AuditWriteFailed int64 `json:"audit_write_failed"`
}

// Overhead is proxy-added latency in milliseconds over the last <=4096 samples.
type Overhead struct {
	P50     float64 `json:"p50"`
	P95     float64 `json:"p95"`
	Samples int     `json:"samples"`
}

type rpmSlot struct {
	sec int64
	n   int64
}

// Registry is safe for concurrent use. It implements transform.Sink.
type Registry struct {
	stageRings                                       [8]stageRing
	sizeBuckets                                      [5]int64
	activeRequests, extractorQueued, extractorActive atomic.Int64
	correlation                                      correlation
	now                                              func() time.Time // injectable clock; see newWithClock
	start                                            time.Time

	requests, blocked, passthrough, errs, restores, replacements, downgraded, vaultSize, auditFailed atomic.Int64

	mu       sync.Mutex
	byRule   map[string]int64
	byReason map[string]int64
	slots    [rpmSlots]rpmSlot
	lat      [reservoirSize]time.Duration
	latN     int // total samples written (ring position = latN % size)
}

// New returns a Registry using the wall clock.
func New() *Registry { return newWithClock(time.Now) }

// newWithClock is the unexported test constructor that injects the clock.
func newWithClock(now func() time.Time) *Registry {
	r := &Registry{now: now, start: now(), byRule: map[string]int64{}, byReason: map[string]int64{}}
	r.initCorrelation()
	return r
}

// Record implements transform.Sink: counts replacements by rule, and
// downgraded replacements in one counter.
func (r *Registry) Record(e transform.Event) {
	if e.Count <= 0 {
		return
	}
	r.replacements.Add(int64(e.Count))
	if e.Downgraded {
		r.downgraded.Add(int64(e.Count))
	}
	r.mu.Lock()
	r.byRule[e.Rule] += int64(e.Count)
	r.mu.Unlock()
}

// IncRequest counts one proxied request.
func (r *Registry) IncRequest() {
	r.requests.Add(1)
	sec := r.now().Unix()
	r.mu.Lock()
	s := &r.slots[sec%rpmSlots]
	if s.sec != sec {
		*s = rpmSlot{sec: sec}
	}
	s.n++
	r.mu.Unlock()
}

// IncBlocked counts one blocked request under reason. Callers must take
// reason from a small closed set of constant codes (never request data), so
// the per-reason map cannot grow without bound.
func (r *Registry) IncBlocked(reason string) {
	r.blocked.Add(1)
	r.mu.Lock()
	r.byReason[reason]++
	r.mu.Unlock()
}

// IncPassthrough counts one request forwarded unsanitized because
// policy.on_error is "passthrough". It does not count as blocked.
func (r *Registry) IncPassthrough() { r.passthrough.Add(1) }

// IncAuditFailed counts one decision record that could not be written.
func (r *Registry) IncAuditFailed() { r.auditFailed.Add(1) }

// IncError counts one internal error.
func (r *Registry) IncError() { r.errs.Add(1) }

// IncRestore adds n restored synthetic values (n <= 0 is ignored).
func (r *Registry) IncRestore(n int) {
	if n > 0 {
		r.restores.Add(int64(n))
	}
}

// ObserveOverhead records proxy-added latency.
func (r *Registry) ObserveOverhead(d time.Duration) {
	r.mu.Lock()
	r.lat[r.latN%reservoirSize] = d
	r.latN++
	r.mu.Unlock()
}

// SetVaultSize records the current vault entry count.
func (r *Registry) SetVaultSize(n int) { r.vaultSize.Store(int64(n)) }

// Snapshot returns a consistent-enough copy of all counters.
func (r *Registry) Snapshot() Snapshot {
	now := r.now()
	sec := now.Unix()
	s := Snapshot{
		Requests:       r.requests.Load(),
		ActiveRequests: r.activeRequests.Load(), ExtractorQueued: r.extractorQueued.Load(), ExtractorActive: r.extractorActive.Load(),
		Blocked:      r.blocked.Load(),
		Passthrough:  r.passthrough.Load(),
		Errors:       r.errs.Load(),
		Restores:     r.restores.Load(),
		Replacements: r.replacements.Load(),
		Downgraded:   r.downgraded.Load(),
		VaultSize:    r.vaultSize.Load(),

		AuditWriteFailed: r.auditFailed.Load(),
	}
	if up := now.Sub(r.start); up > 0 {
		s.UptimeSeconds = int64(up / time.Second)
	}
	r.mu.Lock()
	stageRings := r.stageRings
	r.snapshotSizesAndRecent(&s)
	s.ReplacementsByRule = make(map[string]int64, len(r.byRule))
	for k, v := range r.byRule {
		s.ReplacementsByRule[k] = v
	}
	s.BlockedByReason = make(map[string]int64, len(r.byReason))
	for k, v := range r.byReason {
		s.BlockedByReason[k] = v
	}
	for _, sl := range r.slots {
		if sl.sec > sec-rpmSlots && sl.sec <= sec {
			s.RequestsPerMinute += sl.n
		}
	}
	n := r.latN
	if n > reservoirSize {
		n = reservoirSize
	}
	samples := make([]time.Duration, n)
	copy(samples, r.lat[:n])
	r.mu.Unlock()

	snapshotStages(stageRings, &s)
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	s.OverheadMS = Overhead{P50: pct(samples, 50), P95: pct(samples, 95), Samples: n}
	return s
}

// pct is nearest-rank over sorted samples, in ms; 0 when empty.
func pct(sorted []time.Duration, p int) float64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := (p*len(sorted) + 99) / 100 // ceil(p/100 * n)
	if rank < 1 {
		rank = 1
	}
	return float64(sorted[rank-1]) / float64(time.Millisecond)
}
