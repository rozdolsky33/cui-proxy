// Package vault maps real values to deterministic synthetics and back. It
// never logs or prints real values.
package vault

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"sync"

	"cui-proxy/internal/synth"
)

// maxAttempts bounds collision retries; generators with smaller pools (ipv4)
// return their own exhaustion error first.
const maxAttempts = 4096

// Vault is safe for concurrent use.
type Vault struct {
	key []byte

	mu     sync.RWMutex
	fwd    map[string]string   // class+"\x00"+real -> synthetic
	rev    map[string]string   // synthetic -> real
	reals  map[string]struct{} // every real held or reserved
	sorted []string            // synthetics, ascending, for prefix lookup
}

// New returns an empty vault using key for synthetic derivation.
func New(key []byte) *Vault {
	return &Vault{
		key:   append([]byte(nil), key...),
		fwd:   map[string]string{},
		rev:   map[string]string{},
		reals: map[string]struct{}{},
	}
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
	sum := v.sum(class, real)
	for attempt := 0; attempt < maxAttempts; attempt++ {
		cand, err := synth.Generate(class, sum, attempt, real)
		if err != nil {
			return "", fmt.Errorf("vault: %s: %w", class, err)
		}
		if cand == real || v.taken(cand) {
			continue
		}
		v.insert(fk, real, cand)
		return cand, nil
	}
	return "", fmt.Errorf("vault: %s: no free synthetic after %d attempts", class, maxAttempts)
}

// taken reports whether cand collides with an existing synthetic or real.
func (v *Vault) taken(cand string) bool {
	if _, ok := v.rev[cand]; ok {
		return true
	}
	_, ok := v.reals[cand]
	return ok
}

func (v *Vault) insert(fk, real, syn string) {
	v.fwd[fk] = syn
	v.rev[syn] = real
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
	}
}

// Len returns the number of mappings.
func (v *Vault) Len() int {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return len(v.fwd)
}

var errCorrupt = errors.New("vault: corrupt vault file")
