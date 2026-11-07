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
	// (synth.InOverflow) was held or reserved: from then on no overflow
	// synthetic is coined, so none can equal a real address.
	noOverflow bool
}

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
	fk := class + "\x00" + real
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
	v.noteReal(real)
	sum := v.sum(class, real)
	for attempt, n := 0, synth.Attempts(class); attempt < n; attempt++ {
		cand, err := synth.Generate(class, sum, attempt, real)
		if err != nil {
			return "", fmt.Errorf("vault: %s: %w", class, err)
		}
		if v.noOverflow && synth.InOverflow(cand) {
			break // overflow attempts come last: the usable pool is used up
		}
		if cand == real || v.taken(cand) {
			continue
		}
		v.insert(fk, real, cand)
		return cand, nil
	}
	return "", fmt.Errorf("vault: %s: %w", class, synth.ErrExhausted)
}

// taken reports whether cand collides with an existing synthetic or real.
func (v *Vault) taken(cand string) bool {
	if _, ok := v.rev[cand]; ok {
		return true
	}
	_, ok := v.reals[cand]
	return ok
}

// noteReal records that a real value was seen (callers hold mu).
func (v *Vault) noteReal(real string) {
	if !v.noOverflow && synth.InOverflow(real) {
		v.noOverflow = true
	}
}

func (v *Vault) insert(fk, real, syn string) {
	v.noteReal(real)
	v.fwd[fk] = syn
	v.rev[syn] = real
	v.cls[syn], _ = splitKey(fk)
	v.reals[real] = struct{}{}
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
		v.reals[r] = struct{}{}
		v.noteReal(r)
	}
}

// Len returns the number of mappings.
func (v *Vault) Len() int {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return len(v.fwd)
}

var errCorrupt = errors.New("vault: corrupt vault file")
