package transform

import (
	"testing"

	"cui-proxy/internal/vault"
)

// WithSink returns a sanitizer sharing detectors and vault but recording to a
// different sink; the original's sink is untouched.
func TestWithSinkSeparatesEvents(t *testing.T) {
	v := vault.New(testKey)
	base := &recSink{}
	s := NewSanitizer(builtins(t), v, base)
	a, b := &recSink{}, &recSink{}
	sa, sb := s.WithSink(a), s.WithSink(b)

	outA, err := sa.Text("host 10.20.30.15")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sb.Text("mail bob@corp-internal.com and bob@corp-internal.com"); err != nil {
		t.Fatal(err)
	}
	if len(base.evs) != 0 {
		t.Errorf("base sink got %d events, want 0", len(base.evs))
	}
	if len(a.evs) != 1 || a.evs[0].Class != "ipv4" || a.evs[0].Count != 1 {
		t.Errorf("sink a = %+v", a.evs)
	}
	if len(b.evs) != 1 || b.evs[0].Class != "email" || b.evs[0].Count != 2 {
		t.Errorf("sink b = %+v", b.evs)
	}
	// Shared vault: the original sanitizer reuses the same synthetic.
	outBase, err := s.Text("host 10.20.30.15")
	if err != nil {
		t.Fatal(err)
	}
	if outBase != outA {
		t.Errorf("vault not shared: %q vs %q", outBase, outA)
	}
}

func TestWithSinkNilSafe(t *testing.T) {
	s := NewSanitizer(builtins(t), vault.New(testKey), &recSink{}).WithSink(nil)
	if _, err := s.Text("host 10.20.30.15"); err != nil {
		t.Fatal(err)
	}
	var none *Sanitizer
	if none.WithSink(&recSink{}) != nil {
		t.Error("nil sanitizer WithSink should return nil")
	}
}

// Detect reports detected values without replacing or recording them, and
// skips values that already are vault synthetics.
func TestDetect(t *testing.T) {
	v := vault.New(testKey)
	sink := &recSink{}
	s := NewSanitizer(builtins(t), v, sink)
	sp := s.Detect("host 10.20.30.15 mail bob@corp-internal.com")
	if len(sp) != 2 || sp[0].Class != "ipv4" || sp[1].Class != "email" {
		t.Errorf("spans = %+v", sp)
	}
	if len(s.Detect("plain text, model gpt-5.5")) != 0 {
		t.Error("false positive on plain text")
	}
	syn := pseudo(t, v, "ipv4", "10.20.30.16")
	if sp := s.Detect("host " + syn); len(sp) != 0 {
		t.Errorf("synthetic reported: %+v", sp)
	}
	if len(sink.evs) != 0 || v.Len() != 1 {
		t.Errorf("Detect had side effects: events %d, vault %d", len(sink.evs), v.Len())
	}
}
