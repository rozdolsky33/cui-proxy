// Package transform swaps detected sensitive values for vault synthetics
// (Sanitizer) and swaps them back in responses (Restorer, Stream).
package transform

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"cui-proxy/internal/detect"
	"cui-proxy/internal/synth"
	"cui-proxy/internal/vault"
)

// Event reports replacements of one value. It never contains the real value:
// only the detector rule, the class, the vault's keyed fingerprint
// (vault.Fingerprint: 8 hex chars of an HMAC under a vault-held audit subkey),
// and how many times it was replaced in one Text call. Downgraded marks
// spans masked as class secret because their own class's synthetic would not
// have restored in place (Class is then "secret").
type Event struct {
	Rule, Class, Fingerprint string
	Count                    int
	Downgraded               bool
}

// Sink receives audit events.
type Sink interface{ Record(Event) }

// ErrUnrestorable means the sanitized text would not restore to the input
// even after the offending spans were downgraded to class secret (a bounded
// synthetic glued to a letter is downgraded, not blocked; see Text). The
// caller must block. Returned errors wrap it with the rule and detected class
// of the nearest inserted synthetic, never the value.
var ErrUnrestorable = errors.New("transform: sanitized text would not restore faithfully")

// ErrPseudonym wraps a vault.Pseudonym failure (unknown class, invalid value
// for the class, exhausted pool). The caller must block.
var ErrPseudonym = errors.New("transform: no synthetic for detected value")

// ErrAmbiguous means a detected value equals a synthetic it may not pass as:
// one from the IPv4 overflow pool (198.18.0.0/15, a range that can carry
// real traffic), or one coined concurrently. Passing it as "already
// sanitized" could leak a real address and restore it to another real
// value. The caller must block; the vault then stops coining overflow
// addresses. It is vault.ErrAmbiguous.
var ErrAmbiguous = vault.ErrAmbiguous

// SpanError is the error Text returns for a detected span it cannot
// sanitize. Kind is ErrUnrestorable, ErrPseudonym or ErrAmbiguous; Rule and
// Class name the span's detector and detected class (config names and a
// closed set, never the value); Cause is the vault error, if any (its text
// names the class and the failure, never the value). errors.Is matches Kind
// and Cause.
type SpanError struct {
	Kind        error
	Rule, Class string
	Cause       error
}

func (e *SpanError) Error() string {
	s := fmt.Sprintf("%v: rule=%s class=%s", e.Kind, e.Rule, e.Class)
	if e.Cause != nil {
		s += ": " + e.Cause.Error()
	}
	return s
}

// Unwrap returns Kind and, when set, Cause.
func (e *SpanError) Unwrap() []error {
	if e.Cause == nil {
		return []error{e.Kind}
	}
	return []error{e.Kind, e.Cause}
}

// testHookBeforeAdmit runs after the spans are classified and before the
// reals are admitted to the vault (tests only; nil in production).
var testHookBeforeAdmit func()

// testHookBuilt rewrites each built output before the self-check (tests
// only; nil in production).
var testHookBuilt func(string) string

// Sanitizer replaces detected values with synthetics. Safe for concurrent use
// if the sink is.
type Sanitizer struct {
	ds   []detect.Detector
	v    *vault.Vault
	sink Sink // may be nil
	r    *Restorer
}

// NewSanitizer returns a sanitizer; sink may be nil.
func NewSanitizer(ds []detect.Detector, v *vault.Vault, sink Sink) *Sanitizer {
	return &Sanitizer{ds: ds, v: v, sink: sink, r: NewRestorer(v)}
}

// WithSink returns a shallow copy of s that shares its detectors, vault and
// restorer but records events to sink instead (nil records nothing). It is
// cheap enough to call once per request. A nil s returns nil.
func (s *Sanitizer) WithSink(sink Sink) *Sanitizer {
	if s == nil {
		return nil
	}
	c := *s
	c.sink = sink
	return &c
}

// passSynthetic reports whether detected text t is a vault synthetic that may
// pass as already sanitized. Overflow-range synthetics never do: they are
// indistinguishable from a real address in that range.
func (s *Sanitizer) passSynthetic(t string) bool {
	return s.v.IsSynthetic(t) && !synth.InOverflow(t)
}

// Detect returns the detected spans of in whose text is not already a vault
// synthetic (overflow-range synthetics are reported). It neither replaces nor
// records anything; use it to check text that must be blocked rather than
// rewritten (e.g. request headers).
func (s *Sanitizer) Detect(in string) []detect.Span {
	var out []detect.Span
	for _, sp := range detect.Scan(s.ds, in) {
		if !s.passSynthetic(in[sp.Start:sp.End]) {
			out = append(out, sp)
		}
	}
	return out
}

// Text returns in with every detected value replaced by its synthetic.
// Spans whose text already is a vault synthetic (from earlier turns) are left
// alone, so sanitizing is idempotent. On error the returned text is empty and
// the caller must block; events are recorded only on success, one per
// (rule, class, fingerprint) with its occurrence count.
//
// Before returning, Text checks that the Restorer would find exactly the
// inserted synthetics plus the synthetics that were already present, at the
// same places. If not, every offending bounded-class span (ipv4, ipv6, cidr,
// ssn, card: a synthetic the boundary rule refuses where it landed, e.g.
// "id:fe80::1", "ID123-45-6789") is downgraded to class secret, which gets
// its own vault mapping and restores without the boundary rule, and the text
// is rebuilt once. Only if the rebuilt text still fails the check does Text
// return ErrUnrestorable. The result is deterministic: the same text against
// the same vault downgrades the same spans.
func (s *Sanitizer) Text(in string) (string, error) {
	spans := detect.Scan(s.ds, in)
	if len(spans) == 0 {
		return in, nil
	}
	// Synthetics already present, found before any new one is coined.
	pre, _ := s.r.snapshot().scan(in, 0, true)

	todo := spans[:0:0]
	var reals []string
	for _, sp := range spans {
		if t := in[sp.Start:sp.End]; !s.passSynthetic(t) {
			todo = append(todo, sp)
			reals = append(reals, t)
		}
	}
	if len(todo) == 0 {
		return in, nil
	}
	if testHookBeforeAdmit != nil {
		testHookBeforeAdmit()
	}
	// One locked step: reserve every real (no synthetic coined below, here
	// or concurrently, may equal one), and refuse a real that already is a
	// synthetic (an overflow-range one, or one coined since the check above).
	items := make([]vault.Item, len(todo))
	for i, sp := range todo {
		items[i] = vault.Item{Class: sp.Class, Real: reals[i]}
	}
	if i, err := s.v.Admit(items); err != nil {
		return "", &SpanError{Kind: ErrAmbiguous, Rule: todo[i].Rule, Class: todo[i].Class}
	}

	// Coin and build; on a self-check mismatch, downgrade the offending
	// bounded-class spans to class secret (a self-delimiting REDACTED token
	// that restores without the boundary rule) and rebuild once.
	classes := make([]string, len(todo))
	for i, sp := range todo {
		classes[i] = sp.Class
	}
	down := make([]bool, len(todo))
	for pass := 0; ; pass++ {
		syns := make([]string, len(todo))
		for i := len(todo) - 1; i >= 0; i-- { // right to left
			syn, err := s.v.Pseudonym(classes[i], reals[i])
			if err != nil {
				return "", &SpanError{Kind: ErrPseudonym, Rule: todo[i].Rule, Class: classes[i], Cause: err}
			}
			syns[i] = syn
		}
		out, want, ins := build(in, pre, todo, syns)
		if testHookBuilt != nil {
			out = testHookBuilt(out)
		}
		got, _ := s.r.snapshot().scan(out, 0, true)
		k, bad := firstMismatch(got, want)
		if !bad {
			s.record(todo, classes, down, reals)
			return out, nil
		}
		fix := offending(got, want, ins, syns, classes)
		if pass > 0 || len(fix) == 0 {
			sp := todo[nearest(ins, k)]
			return "", &SpanError{Kind: ErrUnrestorable, Rule: sp.Rule, Class: sp.Class}
		}
		for _, i := range fix {
			classes[i], down[i] = "secret", true
		}
	}
}

// build writes in with each todo span replaced by its synthetic; spans are
// sorted and disjoint. It returns the output, where the Restorer must find
// synthetics in it (each inserted one plus each surviving pre-existing one,
// sorted), and the start of each inserted synthetic (parallel to todo).
func build(in string, pre []match, todo []detect.Span, syns []string) (string, []match, []int) {
	var b strings.Builder
	want := make([]match, 0, len(todo)+len(pre))
	ins := make([]int, 0, len(todo))
	last, delta, pi := 0, 0, 0
	keepPre := func(upto int) { // pre matches ending at or before upto
		for ; pi < len(pre) && pre[pi].end <= upto; pi++ {
			if pre[pi].start >= last {
				want = append(want, match{pre[pi].start + delta, pre[pi].end + delta})
			}
		}
	}
	for i, sp := range todo {
		keepPre(sp.Start)
		for pi < len(pre) && pre[pi].start < sp.End { // overlaps the span: replaced with it
			pi++
		}
		b.WriteString(in[last:sp.Start])
		at := b.Len()
		b.WriteString(syns[i])
		want = append(want, match{at, at + len(syns[i])})
		ins = append(ins, at)
		delta += len(syns[i]) - (sp.End - sp.Start)
		last = sp.End
	}
	keepPre(len(in))
	b.WriteString(in[last:])
	sort.Slice(want, func(i, j int) bool { return want[i].start < want[j].start })
	return b.String(), want, ins
}

// offending returns the indices of the inserted spans to downgrade after a
// self-check mismatch: every bounded-class (not self-delimiting) inserted
// synthetic the Restorer would not find where it was put, and, for any other
// difference (a pre-existing synthetic lost or an unexpected match), the
// bounded-class inserted synthetic nearest to it. Sorted, no duplicates.
func offending(got, want []match, ins []int, syns []string, classes []string) []int {
	inGot := make(map[match]bool, len(got))
	for _, m := range got {
		inGot[m] = true
	}
	inWant := make(map[match]bool, len(want))
	for _, m := range want {
		inWant[m] = true
	}
	var bounded []int // starts of bounded inserted synthetics
	var idx []int     // their todo indices
	isIns := map[match]bool{}
	pick := map[int]bool{}
	for i, at := range ins {
		m := match{at, at + len(syns[i])}
		isIns[m] = true
		if selfDelimiting[classes[i]] {
			continue
		}
		bounded = append(bounded, at)
		idx = append(idx, i)
		if !inGot[m] {
			pick[i] = true
		}
	}
	if len(bounded) > 0 {
		for _, m := range want {
			if !isIns[m] && !inGot[m] {
				pick[idx[nearest(bounded, m.start)]] = true
			}
		}
		for _, m := range got {
			if !inWant[m] {
				pick[idx[nearest(bounded, m.start)]] = true
			}
		}
	}
	out := make([]int, 0, len(pick))
	for i := range pick {
		out = append(out, i)
	}
	sort.Ints(out)
	return out
}

// firstMismatch returns the output offset where match lists a and b first
// differ, and whether they differ at all.
func firstMismatch(a, b []match) (int, bool) {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return min(a[i].start, b[i].start), true
		}
	}
	switch {
	case len(a) > len(b):
		return a[len(b)].start, true
	case len(b) > len(a):
		return b[len(a)].start, true
	}
	return 0, false
}

// nearest returns the index in starts (non-empty, ascending) closest to pos.
func nearest(starts []int, pos int) int {
	best := 0
	for i, s := range starts {
		if abs(s-pos) < abs(starts[best]-pos) {
			best = i
		}
	}
	return best
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func (s *Sanitizer) record(todo []detect.Span, classes []string, down []bool, reals []string) {
	if s.sink == nil {
		return
	}
	type key struct {
		rule, class, fp string
		down            bool
	}
	counts := map[key]int{}
	var order []key
	for i, sp := range todo {
		k := key{sp.Rule, classes[i], s.v.Fingerprint(classes[i], reals[i]), down[i]}
		if counts[k] == 0 {
			order = append(order, k)
		}
		counts[k]++
	}
	for _, k := range order {
		s.sink.Record(Event{Rule: k.rule, Class: k.class, Fingerprint: k.fp, Count: counts[k], Downgraded: k.down})
	}
}
