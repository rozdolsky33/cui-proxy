package vault

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"cui-proxy/internal/synth"
)

// S3: an overflow-range real written with leading zeros, as a CIDR, or an
// ipv4-class real that cannot be parsed at all, disables the overflow pool.
func TestNoOverflowSetByLeadingZeroAndUnparseableReals(t *testing.T) {
	for _, tc := range []struct{ class, real string }{
		{"ipv4", "198.018.001.001"},
		{"ipv4", "198.18.001.1"},
		{"cidr", "198.018.1.1/24"},
		{"ipv4", "999.18.1.1"}, // unparseable: fail closed
		{"cidr", "198.18.1.1/99"},
	} {
		t.Run(tc.real, func(t *testing.T) {
			v := New(testKey)
			if _, err := v.Pseudonym(tc.class, tc.real); err != nil && !errors.Is(err, synth.ErrExhausted) {
				t.Logf("Pseudonym: %v", err)
			}
			if !v.noOverflow {
				t.Fatal("overflow pool still enabled")
			}
		})
	}
	// Admit (the sanitizer's path) too.
	v := New(testKey)
	if _, err := v.Admit([]Item{{"ipv4", "198.018.001.001"}}); err != nil {
		t.Fatal(err)
	}
	if !v.noOverflow {
		t.Fatal("Admit did not disable the overflow pool")
	}
}

// S3: the same address written two ways is one real: one synthetic.
func TestSameRealTwoSpellingsOneSynthetic(t *testing.T) {
	v := New(testKey)
	a, err := v.Pseudonym("ipv4", "10.020.030.015")
	if err != nil {
		t.Fatal(err)
	}
	b, err := v.Pseudonym("ipv4", "10.20.30.15")
	if err != nil {
		t.Fatal(err)
	}
	if a != b || v.Len() != 1 {
		t.Fatalf("two synthetics (%s, %s) for one address; Len %d", a, b, v.Len())
	}
	// A reserved real blocks its synthetic in any spelling.
	v2 := New(testKey)
	v2.Reserve("192.000.002.007")
	for i := 0; i < 762; i++ {
		s, err := v2.Pseudonym("ipv4", "10.9."+itoa(i/250)+"."+itoa(i%250+1))
		if errors.Is(err, synth.ErrExhausted) {
			break
		}
		if s == "192.0.2.7" {
			t.Fatal("coined a synthetic equal to a reserved real (other spelling)")
		}
	}
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }

// S1: Admit is the sanitizer's single locked step: it reserves the reals,
// disables the overflow pool for overflow-range reals, and reports a real
// that already is a synthetic.
func TestAdmit(t *testing.T) {
	v := New(testKey)
	syns := fillIPv4(t, v, 0, 763)
	over := syns[762]
	if !synth.InOverflow(over) {
		t.Fatalf("expected an overflow synthetic, got %s", over)
	}
	before := v.Changes()
	i, err := v.Admit([]Item{{"ipv4", "10.99.0.1"}, {"ipv4", over}})
	if !errors.Is(err, ErrAmbiguous) || i != 1 {
		t.Fatalf("Admit = %d, %v; want index 1, ErrAmbiguous", i, err)
	}
	if !v.noOverflow || v.Changes() == before {
		t.Error("ambiguity did not disable (and dirty) the overflow pool")
	}
	if _, ok := v.reals["10.99.0.1"]; !ok {
		t.Error("Admit did not reserve the other reals")
	}
	// A leading-zero spelling of a synthetic is the same address.
	a := syns[0]
	p, _ := synth.Canonical("ipv4", a)
	if _, err := v.Admit([]Item{{"ipv4", zeroPad(p)}}); !errors.Is(err, ErrAmbiguous) {
		t.Errorf("zero-padded synthetic %s not ambiguous: %v", zeroPad(p), err)
	}
}

// zeroPad writes every octet with three digits.
func zeroPad(ip string) string {
	out, octet := "", ""
	flush := func() {
		for len(octet) < 3 {
			octet = "0" + octet
		}
		out += octet
		octet = ""
	}
	for _, c := range ip {
		if c == '.' {
			flush()
			out += "."
			continue
		}
		octet += string(c)
	}
	flush()
	return out
}

// S2: a flag set by a reserve only (no mapping coined) survives Save/Open,
// and the reopened vault coins no overflow address.
func TestNoOverflowPersisted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.bin")
	v := New(testKey)
	before := v.Changes()
	v.Reserve("198.18.1.1")
	if v.Changes() == before {
		t.Fatal("flag change did not mark the vault changed")
	}
	c := v.Changes()
	v.Reserve("198.18.1.2")
	if v.Changes() != c {
		t.Error("a second reserve with the flag already set marked the vault changed")
	}
	if v.Len() != 0 {
		t.Fatal("reserve coined a mapping")
	}
	if err := v.Save(path); err != nil {
		t.Fatal(err)
	}
	v2, err := Open(path, testKey)
	if err != nil {
		t.Fatal(err)
	}
	if !v2.noOverflow {
		t.Fatal("no_overflow lost across Save/Open")
	}
	fillIPv4(t, v2, 0, 762)
	if _, err := v2.Pseudonym("ipv4", "10.250.0.1"); !errors.Is(err, synth.ErrExhausted) {
		t.Fatalf("reopened vault coined from the overflow pool: %v", err)
	}
}

// S2: an old CUIV2 payload (a bare entry array, no flag) still loads, with
// the flag false.
func TestOldPayloadLoadsWithFlagFalse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.bin")
	plain, _ := json.Marshal([]entry{{Class: "ipv4", Real: "10.1.2.3", Synthetic: "192.0.2.9"}})
	gcm, err := newGCM(testKey)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		t.Fatal(err)
	}
	blob := append(append([]byte(nil), fileMagic...), nonce...)
	blob = gcm.Seal(blob, nonce, plain, fileMagic)
	if err := os.WriteFile(path, blob, 0o600); err != nil {
		t.Fatal(err)
	}
	v, err := Open(path, testKey)
	if err != nil {
		t.Fatal(err)
	}
	if v.noOverflow {
		t.Error("old payload loaded with the flag set")
	}
	if r, ok := v.Real("192.0.2.9"); !ok || r != "10.1.2.3" {
		t.Error("old payload mapping lost")
	}
	// An old payload whose reals include an overflow-range address sets it.
	plain, _ = json.Marshal([]entry{{Class: "ipv4", Real: "198.018.1.1", Synthetic: "192.0.2.9"}})
	blob = append(append([]byte(nil), fileMagic...), nonce...)
	blob = gcm.Seal(blob, nonce, plain, fileMagic)
	if err := os.WriteFile(path, blob, 0o600); err != nil {
		t.Fatal(err)
	}
	if v, err = Open(path, testKey); err != nil || !v.noOverflow {
		t.Errorf("overflow-range real in an old payload: flag %v, err %v", v != nil && v.noOverflow, err)
	}
}
