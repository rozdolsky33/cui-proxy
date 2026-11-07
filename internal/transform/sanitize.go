// Package transform swaps detected sensitive values for vault synthetics
// (Sanitizer) and swaps them back in responses (Restorer, Stream).
package transform

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"cui-proxy/internal/detect"
	"cui-proxy/internal/vault"
)

// Event reports replacements of one value. It never contains the real value:
// only the detector rule, the class, the first 8 hex chars of the SHA-256 of
// the real value, and how many times it was replaced in one Text call.
type Event struct {
	Rule, Class, Fingerprint string
	Count                    int
}

// Sink receives audit events.
type Sink interface{ Record(Event) }

// ErrUnrestorable means the sanitized text would not restore to the input
// (e.g. a synthetic glued to a letter, which the Restorer's boundary rule
// refuses to restore). The caller must block.
var ErrUnrestorable = errors.New("transform: sanitized text would not restore faithfully")

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

// Fingerprint returns the first 8 hex chars of the SHA-256 of real.
func Fingerprint(real string) string {
	h := sha256.Sum256([]byte(real))
	return hex.EncodeToString(h[:4])
}

// Text returns in with every detected value replaced by its synthetic.
// Spans whose text already is a vault synthetic (from earlier turns) are left
// alone, so sanitizing is idempotent. On error the returned text is empty and
// the caller must block; events are recorded only on success, one per
// (rule, class, fingerprint) with its occurrence count.
//
// Before returning, Text checks that the Restorer would find exactly the
// inserted synthetics plus the synthetics that were already present, at the
// same places; otherwise it returns ErrUnrestorable.
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
		if t := in[sp.Start:sp.End]; !s.v.IsSynthetic(t) {
			todo = append(todo, sp)
			reals = append(reals, t)
		}
	}
	if len(todo) == 0 {
		return in, nil
	}
	// No synthetic coined below may equal another value in this text.
	s.v.Reserve(reals...)

	syns := make([]string, len(todo))
	for i := len(todo) - 1; i >= 0; i-- { // right to left
		syn, err := s.v.Pseudonym(todo[i].Class, reals[i])
		if err != nil {
			return "", fmt.Errorf("transform: rule %s: %w", todo[i].Rule, err)
		}
		syns[i] = syn
	}

	// Build the output; spans are sorted and disjoint. Track where each
	// inserted synthetic lands and where surviving pre-existing ones move.
	var b strings.Builder
	want := make([]match, 0, len(todo)+len(pre))
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
		delta += len(syns[i]) - (sp.End - sp.Start)
		last = sp.End
	}
	keepPre(len(in))
	b.WriteString(in[last:])
	out := b.String()

	sort.Slice(want, func(i, j int) bool { return want[i].start < want[j].start })
	got, _ := s.r.snapshot().scan(out, 0, true)
	if !equalMatches(got, want) {
		return "", ErrUnrestorable
	}
	s.record(todo, reals)
	return out, nil
}

func equalMatches(a, b []match) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (s *Sanitizer) record(todo []detect.Span, reals []string) {
	if s.sink == nil {
		return
	}
	type key struct{ rule, class, fp string }
	counts := map[key]int{}
	var order []key
	for i, sp := range todo {
		k := key{sp.Rule, sp.Class, Fingerprint(reals[i])}
		if counts[k] == 0 {
			order = append(order, k)
		}
		counts[k]++
	}
	for _, k := range order {
		s.sink.Record(Event{Rule: k.rule, Class: k.class, Fingerprint: k.fp, Count: counts[k]})
	}
}
