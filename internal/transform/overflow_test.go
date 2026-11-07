package transform

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"cui-proxy/internal/synth"
	"cui-proxy/internal/vault"
)

// ips returns n distinct private addresses, space separated.
func ips(from, n int) string {
	var b strings.Builder
	for i := from; i < from+n; i++ {
		fmt.Fprintf(&b, " 10.%d.%d.%d", i/62500, i/250%250, i%250+1)
	}
	return b.String()
}

// One input with a real 198.18.1.1 and 800 other addresses: the vault may not
// use the overflow pool, so it fails closed once RFC 5737 is used up.
func TestRealOverflowAddressFailsClosedAtExhaustion(t *testing.T) {
	v := vault.New(testKey)
	s := NewSanitizer(builtins(t), v, nil)
	out, err := s.Text("lab host 198.18.1.1 and" + ips(0, 800))
	if err == nil || out != "" || !errors.Is(err, ErrPseudonym) || !errors.Is(err, synth.ErrExhausted) {
		t.Fatalf("err = %v, out len %d: want a pool-exhausted block", err, len(out))
	}
	for _, syn := range v.Synthetics() {
		if synth.InOverflow(syn) {
			t.Fatalf("overflow synthetic %s coined although a real 198.18/15 address was seen", syn)
		}
	}
	// Without a real overflow-range address the same volume succeeds.
	v2 := vault.New(testKey)
	if _, err := NewSanitizer(builtins(t), v2, nil).Text("hosts" + ips(0, 800)); err != nil {
		t.Fatalf("800 addresses without a 198.18/15 real: %v", err)
	}
}

// Reverse collision: an overflow synthetic, later presented as if it were a
// real value (a real 198.18/15 address). It must not pass through as an
// "already sanitized" synthetic (it would reach the upstream and restore to a
// different real value): the sanitizer fails closed, Detect reports it (so a
// header holding it blocks), and the vault stops coining overflow addresses.
func TestOverflowSyntheticPresentedAsRealFailsClosed(t *testing.T) {
	v := vault.New(testKey)
	s := NewSanitizer(builtins(t), v, nil)
	if _, err := s.Text("fill" + ips(0, 762)); err != nil {
		t.Fatal(err)
	}
	out, err := s.Text("one more 10.200.0.1")
	if err != nil {
		t.Fatal(err)
	}
	syn := strings.TrimPrefix(out, "one more ")
	if !synth.InOverflow(syn) {
		t.Fatalf("expected an overflow synthetic, got %q", syn)
	}

	in := "ping " + syn + " please"
	got, err := s.Text(in)
	if err == nil || got != "" || !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("overflow synthetic in input: out %q, err %v; want ErrAmbiguous", got, err)
	}
	if strings.Contains(err.Error(), syn) {
		t.Error("error carries the value")
	}
	if len(s.Detect(in)) != 1 {
		t.Error("Detect does not report an overflow synthetic (a header holding it would pass)")
	}
	if _, err := v.Pseudonym("ipv4", "10.201.0.1"); !errors.Is(err, synth.ErrExhausted) {
		t.Errorf("vault still coins overflow addresses after the ambiguity: %v", err)
	}
	// RFC 5737 synthetics stay idempotent (they can never be real).
	first, _ := s.Text("again 10.0.0.1")
	if again, err := s.Text(first); err != nil || again != first {
		t.Errorf("RFC 5737 synthetic not idempotent: %q, %v", again, err)
	}
}
