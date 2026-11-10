// Package vault maps real values to deterministic synthetics and back. It
// never logs or prints real values.
package vault

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"

	"cui-proxy/internal/synth"
)

// Vault is safe for concurrent use.
type Vault struct {
	key      []byte
	auditKey []byte // HMAC-SHA256(key, "cui-proxy/audit"): fingerprints only

	mu     sync.RWMutex
	fwd    map[string]string   // class+"\x00"+real -> synthetic
	rev    map[string]string   // synthetic -> real
	cls    map[string]string   // synthetic -> class
	reals  map[string]struct{} // every real held or reserved
	sorted []string            // synthetics, ascending, for prefix lookup
	// noOverflow is set once a real value inside the IPv4 overflow range
	// (synth.InOverflow), or an ipv4/cidr real that cannot be parsed, was
	// held or reserved: from then on no overflow synthetic is coined, so none
	// can equal a real address. It is saved with the vault.
	noOverflow bool
	// changes counts changes that a save must persist (mappings and the
	// flag), so callers can tell whether the vault is dirty.
	changes uint64
}

// ErrAmbiguous means a real value presented for admission is already a
// synthetic (an overflow-range synthetic, or one coined concurrently). The
// caller must block: the value may be a real address equal to a synthetic.
var ErrAmbiguous = errors.New("vault: detected value equals an existing synthetic")

// Item is a detected real value with its class.
type Item struct{ Class, Real string }

// New returns an empty vault using key for synthetic derivation.
func New(key []byte) *Vault {
	return &Vault{
		key:      append([]byte(nil), key...),
		auditKey: subkey(key, "cui-proxy/audit"),
		fwd:      map[string]string{},
		rev:      map[string]string{},
		cls:      map[string]string{},
		reals:    map[string]struct{}{},
	}
}

// subkey derives a domain-separated key: HMAC-SHA256(key, label).
func subkey(key []byte, label string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(label))
	return m.Sum(nil)
}

// Fingerprint returns an audit-safe identifier for (class, real): the first 8
// hex chars of HMAC-SHA256(auditKey, class+"\x00"+real). It is keyed so that
// small value spaces (SSNs, IPv4) cannot be brute-forced from audit logs, and
// the key never leaves the vault.
func (v *Vault) Fingerprint(class, real string) string {
	m := hmac.New(sha256.New, v.auditKey)
	m.Write([]byte(class + "\x00" + real))
	return hex.EncodeToString(m.Sum(nil)[:4])
}

func (v *Vault) sum(class, real string) []byte {
	m := hmac.New(sha256.New, v.key)
	m.Write([]byte(class + "\x00" + real))
	return m.Sum(nil)
}

// Pseudonym returns the synthetic for (class, real), creating it if needed.
// The result is deterministic for a given key; on collision with an existing
// synthetic or any known real value the attempt counter is incremented.
func (v *Vault) Pseudonym(class, real string) (string, error) {
	if !synth.Known(class) {
		return "", fmt.Errorf("vault: unknown class %q", class)
	}
	fk := class + "\x00" + real // exact spelling: restores byte for byte
	v.mu.RLock()
	s, ok := v.fwd[fk]
	v.mu.RUnlock()
	if ok {
		return s, nil
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if s, ok := v.fwd[fk]; ok {
		return s, nil
	}
	v.noteReal(class, real)
	c := canonical(class, real)
	sum := v.sum(class, real)
	for attempt, n := 0, synth.Attempts(class); attempt < n; attempt++ {
		cand, err := synth.Generate(class, sum, attempt, real)
		if err != nil {
			return "", fmt.Errorf("vault: %s: %w", class, err)
		}
		if v.noOverflow && synth.UsesOverflowAddress(cand) {
			break // overflow attempts come last: the usable pool is used up
		}
		if cand == c || cand == real || v.taken(cand) {
			continue
		}
		v.insert(fk, real, cand)
		return cand, nil
	}
	return "", fmt.Errorf("vault: %s: %w", class, synth.ErrExhausted)
}

// taken reports whether cand collides with an existing synthetic or with a
// held or reserved real in any spelling (the reserved set holds every real
// as written and, for IPv4 addresses and prefixes, canonically).
func (v *Vault) taken(cand string) bool {
	if _, ok := v.rev[cand]; ok {
		return true
	}
	for _, f := range forms("", cand) {
		if _, ok := v.reals[f]; ok {
			return true
		}
	}
	return false
}

// canonical is the form of a real used for collision checks only (never for
// mapping keys): one spelling per address for ipv4/cidr ("198.018.1.1" is
// "198.18.1.1"). Unparseable values and other classes are kept as is.
func canonical(class, real string) string {
	if c, ok := synth.Canonical(class, real); ok {
		return c
	}
	return real
}

// forms are the strings a real occupies in the reserved set: as written and,
// for an IPv4 address or prefix (any class, or none), its canonical form.
func forms(class, real string) []string {
	out := []string{real}
	if class == "" {
		if c, ok := synth.Canonical("ipv4", real); ok && c != real {
			out = append(out, c)
		} else if c, ok := synth.Canonical("cidr", real); ok && c != real {
			out = append(out, c)
		}
	} else if c := canonical(class, real); c != real {
		out = append(out, c)
	}
	return out
}

// noteReal disables the overflow pool for a real in the overflow range (any
// spelling) or an ipv4/cidr real that cannot be parsed (fail closed).
// class may be "" (Reserve). Callers hold mu.
func (v *Vault) noteReal(class, real string) {
	if v.noOverflow {
		return
	}
	_, ok := synth.Canonical(class, real)
	if (class == "ipv4" || class == "cidr") && !ok || synth.InOverflow(real) {
		v.noOverflow = true
		v.changes++
	}
}

func (v *Vault) reserve(class, real string) {
	v.noteReal(class, real)
	for _, f := range forms(class, real) {
		v.reals[f] = struct{}{}
	}
}

// Admit is the sanitizer's single locked step before coining: it reserves
// every real (so no synthetic coined later can equal one), disables the
// overflow pool for overflow-range or unparseable addresses, and fails with
// ErrAmbiguous, and the index of the first such item, if a real already is a
// synthetic. Everything is reserved even on failure.
func (v *Vault) Admit(items []Item) (int, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, it := range items {
		v.reserve(it.Class, it.Real)
	}
	for i, it := range items {
		for _, f := range forms(it.Class, it.Real) {
			if _, ok := v.rev[f]; ok {
				return i, ErrAmbiguous
			}
		}
	}
	return -1, nil
}

// Changes returns a counter that grows whenever the vault gains something a
// save must persist (a mapping or the overflow flag).
func (v *Vault) Changes() uint64 {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.changes
}

// insert adds a mapping. fk is class + "\x00" + real (exact spelling).
func (v *Vault) insert(fk, real, syn string) {
	class, _ := splitKey(fk)
	v.reserve(class, real)
	v.changes++
	v.fwd[fk] = syn
	v.rev[syn] = real
	v.cls[syn] = class
	i := sort.SearchStrings(v.sorted, syn)
	v.sorted = append(v.sorted, "")
	copy(v.sorted[i+1:], v.sorted[i:])
	v.sorted[i] = syn
}

// Real returns the real value behind a synthetic.
func (v *Vault) Real(synthetic string) (string, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	r, ok := v.rev[synthetic]
	return r, ok
}

// ClassOf returns the class a synthetic was generated for.
func (v *Vault) ClassOf(synthetic string) (string, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	c, ok := v.cls[synthetic]
	return c, ok
}

// IsSynthetic reports whether s is a synthetic held by the vault.
func (v *Vault) IsSynthetic(s string) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	_, ok := v.rev[s]
	return ok
}

// HasSyntheticWithPrefix reports whether any synthetic starts with p
// (including an exact match, and the empty prefix when the vault is non-empty).
func (v *Vault) HasSyntheticWithPrefix(p string) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	i := sort.SearchStrings(v.sorted, p)
	return i < len(v.sorted) && len(v.sorted[i]) >= len(p) && v.sorted[i][:len(p)] == p
}

// Synthetics returns every synthetic, longest first (ties lexicographic).
func (v *Vault) Synthetics() []string {
	v.mu.RLock()
	out := append([]string(nil), v.sorted...)
	v.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) > len(out[j])
		}
		return out[i] < out[j]
	})
	return out
}

// Reserve records known real values that no synthetic may ever equal.
func (v *Vault) Reserve(reals ...string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, r := range reals {
		v.reserve("", r)
	}
}

// Len returns the number of mappings.
func (v *Vault) Len() int {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return len(v.fwd)
}

var errCorrupt = errors.New("vault: corrupt vault file")
