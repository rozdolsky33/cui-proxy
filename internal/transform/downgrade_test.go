package transform

import (
	"errors"
	"math/rand"
	"regexp"
	"strings"
	"testing"

	"cui-proxy/internal/vault"
)

// wedgeProbes are inputs where a bounded-class synthetic would land in a
// context the Restorer's boundary rule refuses (final review I1). real is
// the detected value that must not reach the output.
var wedgeProbes = []struct{ in, real string }{
	{"ipv6:fe80::1", "fe80::1"},
	{"IPv6:2001:db8::1", "2001:db8::1"},
	{"inet6:2001:db8::5", "2001:db8::5"},
	{"src:fe80::1", "fe80::1"},
	{"id:fe80::1", "fe80::1"},
	{"dhcp6:2001:db8::1 lease", "2001:db8::1"},
	{"ID123-45-6789", "123-45-6789"},
	{"SN123-45-6789", "123-45-6789"},
	{"ssn=123-45-6789x", "123-45-6789"},
	{"ref 123-45-6789A", "123-45-6789"},
	{"x4111 1111 1111 1111", "4111 1111 1111 1111"},
	{"4111 1111 1111 1111.5", "4111 1111 1111 1111"},
	{"id x123-45-6789y now", "123-45-6789"},
}

var redacted = regexp.MustCompile(`REDACTED-[0-9a-f]{8}`)

// streamChunks restores out through a Stream fed in random chunks.
func streamChunks(r *Restorer, out string, rng *rand.Rand) string {
	st := r.NewStream()
	var b strings.Builder
	for i := 0; i < len(out); {
		n := 1 + rng.Intn(4)
		if i+n > len(out) {
			n = len(out) - i
		}
		b.WriteString(st.Write(out[i : i+n]))
		i += n
	}
	b.WriteString(st.Flush())
	return b.String()
}

// I1: a span that would be unrestorable as its bounded class is downgraded
// to class secret instead of blocking the request.
func TestSanitizeDowngradesUnrestorableSpans(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for _, p := range wedgeProbes {
		v := vault.New(testKey)
		sink := &recSink{}
		s := NewSanitizer(builtins(t), v, sink)
		out, err := s.Text(p.in)
		if err != nil {
			t.Errorf("%q: %v", p.in, err)
			continue
		}
		if strings.Contains(out, p.real) {
			t.Errorf("%q: real value remains in %q", p.in, out)
		}
		if !redacted.MatchString(out) {
			t.Errorf("%q: no REDACTED token in %q", p.in, out)
		}
		r := NewRestorer(v)
		if got := r.Text(out); got != p.in {
			t.Errorf("%q: Text restore = %q", p.in, got)
		}
		for k := 0; k < 20; k++ {
			if got := streamChunks(r, out, rng); got != p.in {
				t.Errorf("%q: Stream restore = %q", p.in, got)
				break
			}
		}
		down := 0
		for _, e := range sink.evs {
			if e.Downgraded {
				down += e.Count
				if e.Class != "secret" || e.Rule == "" || strings.Contains(e.Fingerprint, p.real) {
					t.Errorf("%q: bad downgrade event %+v", p.in, e)
				}
			}
		}
		if down != 1 {
			t.Errorf("%q: downgraded count = %d, want 1 (events %+v)", p.in, down, sink.evs)
		}
		// Deterministic: the same text sanitizes to the same output again
		// (idempotent on the output too).
		again, err := s.Text(p.in)
		if err != nil || again != out {
			t.Errorf("%q: second run = %q, %v; want %q", p.in, again, err, out)
		}
		if same, err := s.Text(out); err != nil || same != out {
			t.Errorf("%q: re-sanitizing output = %q, %v", p.in, same, err)
		}
	}
}

// Only the offending span is downgraded: the same real standing alone keeps
// its bounded class, and the glued occurrence gets its own secret mapping.
func TestDowngradeIsPerSpan(t *testing.T) {
	v := vault.New(testKey)
	s := NewSanitizer(builtins(t), v, nil)
	in := "addr fe80::1 and id:fe80::1"
	out, err := s.Text(in)
	if err != nil {
		t.Fatal(err)
	}
	v6 := pseudo(t, v, "ipv6", "fe80::1")
	sec := pseudo(t, v, "secret", "fe80::1")
	if want := "addr " + v6 + " and id:" + sec; out != want {
		t.Errorf("out = %q, want %q", out, want)
	}
	if got := NewRestorer(v).Text(out); got != in {
		t.Errorf("restore = %q", got)
	}
}

// The fail-closed branch stays: if the rebuilt (downgraded) text still does
// not restore faithfully, Text blocks with ErrUnrestorable.
func TestSanitizeBlocksWhenDowngradeCannotFix(t *testing.T) {
	calls := 0
	testHookBuilt = func(out string) string { calls++; return "x" + out } // shifts every match
	defer func() { testHookBuilt = nil }()
	sink := &recSink{}
	out, err := NewSanitizer(builtins(t), vault.New(testKey), sink).Text("ssn 123-45-6789 ok")
	if !errors.Is(err, ErrUnrestorable) {
		t.Fatalf("err = %v, want ErrUnrestorable", err)
	}
	if msg := err.Error(); !strings.Contains(msg, "rule=builtin.ssn") || !strings.Contains(msg, "class=ssn") || strings.Contains(msg, "6789") {
		t.Errorf("error should name rule and class, never the value: %q", msg)
	}
	if out != "" || len(sink.evs) != 0 {
		t.Errorf("partial text %q or events %+v on error", out, sink.evs)
	}
	if calls != 2 {
		t.Errorf("built %d times, want 2 (one rebuild)", calls)
	}
}

// The rebuild happens once: a mismatch on the first build only is fixed by
// the downgrade.
func TestSanitizeRebuildsOnce(t *testing.T) {
	calls := 0
	testHookBuilt = func(out string) string {
		calls++
		if calls == 1 {
			return "x" + out
		}
		return out
	}
	defer func() { testHookBuilt = nil }()
	v := vault.New(testKey)
	in := "ssn 123-45-6789 ok"
	out, err := NewSanitizer(builtins(t), v, nil).Text(in)
	if err != nil {
		t.Fatal(err)
	}
	if want := "ssn " + pseudo(t, v, "secret", "123-45-6789") + " ok"; out != want {
		t.Errorf("out = %q, want %q", out, want)
	}
}

// A mismatch with only self-delimiting spans cannot be downgraded: block.
func TestSanitizeBlocksWhenNothingToDowngrade(t *testing.T) {
	calls := 0
	testHookBuilt = func(out string) string { calls++; return "x" + out }
	defer func() { testHookBuilt = nil }()
	_, err := NewSanitizer(builtins(t), vault.New(testKey), nil).Text("tok Bearer abcdefgh12345678 ok")
	if !errors.Is(err, ErrUnrestorable) || !strings.Contains(err.Error(), "class=secret") {
		t.Fatalf("err = %v, want ErrUnrestorable class=secret", err)
	}
	if calls != 1 {
		t.Errorf("built %d times, want 1", calls)
	}
}
