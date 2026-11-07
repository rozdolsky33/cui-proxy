package transform

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
	"sync"

	"cui-proxy/internal/vault"
)

// Restorer replaces synthetics held by a vault with their real values. It is
// safe for concurrent use.
//
// Matching is left to right, longest synthetic first at each position, and
// only vault members match.
//
// Self-delimiting classes (secret, hostname, email, ocid; see selfDelimiting)
// restore wherever they occur, even glued to letters, digits, '_', '-' or '.':
// their fixed constructed form cannot be a proper prefix of another synthetic
// or plausibly arise in model-written text.
//
// Boundary rule (ipv4, ipv6, cidr, ssn, card). These synthetics are prefixes
// of one another ("192.0.2.5" vs "192.0.2.50") and model-written text may
// contain documentation-range values the vault never issued, so a synthetic S
// found at text[s:e] is restored only when it stands alone as a token. A
// candidate is rejected when
//
//  1. the byte before s or the byte at e is an ASCII letter or digit;
//  2. S starts with a digit and is preceded by "<digit>." , or S ends with a
//     digit and is followed by ".<digit>" (dotted-number continuation, as in
//     "1.192.0.2.5" or "192.0.2.5.1");
//  3. S contains ':' (ipv6-shaped) and is preceded by ':' that follows a hex
//     digit or ':', or followed by ':' then a hex digit or ':' (colon-hex run);
//  4. S starts with a letter and is adjacent to '_', or to '-' or '.' that is
//     itself adjacent (outward) to an ASCII letter or digit (word/hostname run
//     such as "x-S", "S-abc", "S.com"). No bounded class currently generates a
//     letter-initial synthetic; the rule is kept for classes added later.
//
// Anything else adjacent (space, quotes, brackets, '/', '=', '@', ':' after an
// ipv4, '-' after an ipv4, non-ASCII runes, '.' at a sentence end) restores.
// The rule is applied identically by Text and Stream.
type Restorer struct {
	v      *vault.Vault
	escape bool // write real values JSON-escaped for string-literal context

	mu   sync.Mutex
	snap *snapshot
}

// NewRestorer returns a restorer backed by v.
func NewRestorer(v *vault.Vault) *Restorer { return &Restorer{v: v} }

// JSONEscaped returns a restorer over the same vault that writes each real
// value JSON-escaped for string-literal context: the content of
// json.Marshal(real) without the surrounding quotes, HTML characters left
// as is. Use it for text that is itself JSON (function-call arguments), so a
// real value holding '"', '\' or control characters keeps the text valid.
// Matching is identical to the plain restorer; NewStream on the result
// streams with the same escaping.
func (r *Restorer) JSONEscaped() *Restorer { return &Restorer{v: r.v, escape: true} }

// jsonEscape returns s escaped for use inside a JSON string literal.
func jsonEscape(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // a string always encodes
	out := bytes.TrimSuffix(b.Bytes(), []byte("\n"))
	return string(out[1 : len(out)-1])
}

// selfDelimiting lists the classes whose synthetic has a fixed, constructed
// form that is never a proper prefix of another synthetic: REDACTED-<8hex>,
// h-<8hex>.example, user-<8hex>@mail.example, ocid1.<type>.ocsynth..<26hex>.
// They restore wherever a vault member occurs, without the boundary rule.
var selfDelimiting = map[string]bool{"secret": true, "hostname": true, "email": true, "ocid": true}

// snapshot is an immutable view of the vault's synthetics.
type snapshot struct {
	n          int             // vault Len when taken
	set        map[string]bool // every synthetic -> self-delimiting?
	lens       []int           // distinct synthetic lengths, descending
	selfLens   []int           // distinct self-delimiting lengths, descending
	sorted     []string        // all synthetics, ascending, for prefix queries
	sortedSelf []string        // self-delimiting synthetics, ascending
	selfFirst  [256]bool       // first bytes of self-delimiting synthetics
	maxLen     int
	selfMax    int
}

// snapshot returns a view of the current synthetics. The vault only grows, so
// a cached view is reused while Len is unchanged.
func (r *Restorer) snapshot() *snapshot {
	n := r.v.Len()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.snap != nil && r.snap.n == n {
		return r.snap
	}
	syns := r.v.Synthetics()
	sn := &snapshot{n: n, set: make(map[string]bool, len(syns))}
	seen, seenSelf := map[int]bool{}, map[int]bool{}
	for _, s := range syns { // longest first, so lens come out descending
		c, _ := r.v.ClassOf(s)
		self := selfDelimiting[c]
		sn.set[s] = self
		if !seen[len(s)] {
			seen[len(s)] = true
			sn.lens = append(sn.lens, len(s))
		}
		if self {
			sn.sortedSelf = append(sn.sortedSelf, s)
			sn.selfFirst[s[0]] = true
			if !seenSelf[len(s)] {
				seenSelf[len(s)] = true
				sn.selfLens = append(sn.selfLens, len(s))
			}
		}
	}
	if len(sn.lens) > 0 {
		sn.maxLen = sn.lens[0]
	}
	if len(sn.selfLens) > 0 {
		sn.selfMax = sn.selfLens[0]
	}
	sn.sorted = append([]string(nil), syns...)
	sort.Strings(sn.sorted)
	sort.Strings(sn.sortedSelf)
	r.snap = sn
	return sn
}

// hasPrefix reports whether some string in sorted starts with p (exact match included).
func hasPrefix(sorted []string, p string) bool {
	i := sort.SearchStrings(sorted, p)
	return i < len(sorted) && strings.HasPrefix(sorted[i], p)
}

type match struct{ start, end int }

func isDigit(b byte) bool  { return b >= '0' && b <= '9' }
func isLetter(b byte) bool { return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' }
func isAlnum(b byte) bool  { return isDigit(b) || isLetter(b) }
func isHex(b byte) bool    { return isDigit(b) || b >= 'a' && b <= 'f' || b >= 'A' && b <= 'F' }

// scan finds restorable synthetics in t starting at from; t[:from] is already
// decided context used only for the left boundary check. With final=false it
// stops at the first position whose outcome could change if more text were
// appended (a tail that is a prefix of a synthetic that could match there, or
// a bounded synthetic whose right boundary is not yet known) and returns that
// position as stop; otherwise stop is len(t).
//
// Every synthetic starts with an ASCII letter or digit. At a token start (the
// previous byte is not a letter/digit) any synthetic may match; elsewhere
// only self-delimiting ones can, since rule 1 rejects the others.
func (sn *snapshot) scan(t string, from int, final bool) (ms []match, stop int) {
	if len(sn.set) == 0 {
		return nil, len(t)
	}
	i := from
next:
	for i < len(t) {
		if !isAlnum(t[i]) {
			i++
			continue
		}
		tokenStart := i == 0 || !isAlnum(t[i-1])
		lens, sorted, lim := sn.lens, sn.sorted, sn.maxLen
		if !tokenStart {
			if !sn.selfFirst[t[i]] {
				i++
				continue
			}
			lens, sorted, lim = sn.selfLens, sn.sortedSelf, sn.selfMax
		}
		if !final && len(t)-i <= lim && hasPrefix(sorted, t[i:]) {
			return ms, i
		}
		for _, l := range lens {
			if i+l > len(t) {
				continue
			}
			self, ok := sn.set[t[i:i+l]]
			if !ok || !tokenStart && !self {
				continue
			}
			ok, more := true, false
			if !self {
				ok, more = bounded(t, i, i+l, final)
			}
			if more {
				return ms, i
			}
			if ok {
				ms = append(ms, match{i, i + l})
				i += l
				continue next
			}
		}
		i++
	}
	return ms, len(t)
}

// bounded applies the boundary rule to the synthetic t[s:e]. more reports
// that the right boundary depends on text not yet seen (only when !final).
func bounded(t string, s, e int, final bool) (ok, more bool) {
	syn := t[s:e]
	letter := isLetter(syn[0])
	colon := strings.IndexByte(syn, ':') >= 0
	if s > 0 {
		p := t[s-1]
		var p2 byte
		has2 := s >= 2
		if has2 {
			p2 = t[s-2]
		}
		switch {
		case isAlnum(p):
			return false, false
		case isDigit(syn[0]) && p == '.' && has2 && isDigit(p2):
			return false, false
		case colon && p == ':' && has2 && (isHex(p2) || p2 == ':'):
			return false, false
		case letter && (p == '_' || (p == '-' || p == '.') && has2 && isAlnum(p2)):
			return false, false
		}
	}
	if e == len(t) {
		return final, !final
	}
	n := t[e]
	if isAlnum(n) || letter && n == '_' {
		return false, false
	}
	needs2 := n == '.' && (isDigit(syn[len(syn)-1]) || letter) ||
		n == ':' && colon ||
		n == '-' && letter
	if !needs2 {
		return true, false
	}
	if e+1 == len(t) {
		return final, !final
	}
	n2 := t[e+1]
	switch {
	case n == '.' && isDigit(syn[len(syn)-1]) && isDigit(n2):
		return false, false
	case n == ':' && colon && (isHex(n2) || n2 == ':'):
		return false, false
	case letter && (n == '-' || n == '.') && isAlnum(n2):
		return false, false
	}
	return true, false
}

// apply writes t[from:stop] with each match replaced by its real value.
func (r *Restorer) apply(t string, from, stop int, ms []match) string {
	if len(ms) == 0 {
		return t[from:stop]
	}
	var b strings.Builder
	b.Grow(stop - from)
	last := from
	for _, m := range ms {
		b.WriteString(t[last:m.start])
		syn := t[m.start:m.end]
		if real, ok := r.v.Real(syn); ok {
			if r.escape {
				real = jsonEscape(real)
			}
			b.WriteString(real)
		} else {
			b.WriteString(syn)
		}
		last = m.end
	}
	b.WriteString(t[last:stop])
	return b.String()
}

// Text returns in with every restorable synthetic replaced by its real value.
func (r *Restorer) Text(in string) string {
	ms, _ := r.snapshot().scan(in, 0, true)
	return r.apply(in, 0, len(in), ms)
}
