package transform

import (
	"errors"
	"fmt"
	"strings"
	"sync"
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

// S1: many goroutines sanitize texts that mix overflow-range addresses used
// as REAL values with fresh addresses that coin overflow synthetics. A
// successfully sanitized real must never be (or become) a synthetic.
func TestConcurrentAmbiguityGate(t *testing.T) {
	v := vault.New(testKey)
	s := NewSanitizer(builtins(t), v, nil)
	if _, err := s.Text("fill" + ips(0, 762)); err != nil {
		t.Fatal(err)
	}
	// Predict the overflow synthetics fresh reals will get: a twin vault
	// (same key, same fill) coins them without contention.
	twin := vault.New(testKey)
	tw := NewSanitizer(builtins(t), twin, nil)
	if _, err := tw.Text("fill" + ips(0, 762)); err != nil {
		t.Fatal(err)
	}
	const n = 64
	predicted := make([]string, n)
	for i := range predicted {
		syn, err := twin.Pseudonym("ipv4", fmt.Sprintf("10.250.%d.%d", i/250, i%250+1))
		if err != nil {
			t.Fatal(err)
		}
		predicted[i] = syn
	}

	var wg sync.WaitGroup
	ok := make([]bool, n) // the predicted address was sanitized as a real
	for i := 0; i < n; i++ {
		wg.Add(2)
		go func(i int) { // coins predicted[i] (if the pool is still open)
			defer wg.Done()
			_, _ = s.Text(fmt.Sprintf("fresh 10.250.%d.%d", i/250, i%250+1))
		}(i)
		go func(i int) { // presents predicted[i] as a real lab address
			defer wg.Done()
			_, err := s.Text("lab " + predicted[i] + " and" + ips(100000+i*3, 3))
			ok[i] = err == nil
		}(i)
	}
	wg.Wait()
	for i, p := range predicted {
		if ok[i] && v.IsSynthetic(p) {
			t.Fatalf("real %s was sanitized but is also a synthetic (restores to %v)", p, func() string { r, _ := v.Real(p); return r }())
		}
	}
}

// S1, deterministic: another request coins an overflow synthetic equal to
// this request's pending real right between classifying the spans and
// admitting the reals. The gate must still see it (fail closed).
func TestAmbiguityGateIsAtomic(t *testing.T) {
	v := vault.New(testKey)
	s := NewSanitizer(builtins(t), v, nil)
	if _, err := s.Text("fill" + ips(0, 762)); err != nil {
		t.Fatal(err)
	}
	twin := vault.New(testKey)
	if _, err := NewSanitizer(builtins(t), twin, nil).Text("fill" + ips(0, 762)); err != nil {
		t.Fatal(err)
	}
	const other = "10.250.0.1"
	p, err := twin.Pseudonym("ipv4", other)
	if err != nil || !synth.InOverflow(p) {
		t.Fatalf("prediction: %s, %v", p, err)
	}
	testHookBeforeAdmit = func() {
		testHookBeforeAdmit = nil
		if got, err := v.Pseudonym("ipv4", other); err != nil || got != p {
			t.Fatalf("concurrent coin: %s, %v (want %s)", got, err, p)
		}
	}
	defer func() { testHookBeforeAdmit = nil }()
	out, err := s.Text("lab " + p)
	if err == nil {
		t.Fatalf("real %s sanitized to %q although it now is a synthetic for another value", p, out)
	}
	if !errors.Is(err, ErrAmbiguous) {
		t.Errorf("err = %v, want ErrAmbiguous", err)
	}
}
