package transform

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"cui-proxy/internal/ipv4"

	"cui-proxy/internal/detect"
	"cui-proxy/internal/vault"
)

// fuzzSeeds are sensitive values inserted into fuzzed text. text is what is
// inserted; real is the part that must never reach the sanitized output.
var fuzzSeeds = []struct{ text, real string }{
	{"10.20.30.15", "10.20.30.15"},
	{"ocid1.instance.oc2..abc", "ocid1.instance.oc2..abc"},
	{"bob.smith@corp.example.com", "bob.smith@corp.example.com"},
	{"123-45-6789", "123-45-6789"},
	{"4111 1111 1111 1111", "4111 1111 1111 1111"},
	{"fe80::1ff:fe23:4567:890a", "fe80::1ff:fe23:4567:890a"},
	{"10.0.0.0/24", "10.0.0.0/24"},
	{"Bearer abcdefgh12345678", "abcdefgh12345678"},
	{"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjMifQ.c2lnbmF0dXJl", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjMifQ.c2lnbmF0dXJl"},
}

// fuzzSeps delimit seeds from random text so each seed is a standalone token
// (a seed glued to random letters is, by design, not a detectable value).
const fuzzSeps = " \t\n\"'=,;()[]{}<>|!?&"

// FuzzRoundTrip: for text = r1 SEP seedA SEP r2 SEP seedB SEP r3, a successful
// Sanitizer.Text never contains a seeded real value and always restores to the
// exact input. An error is allowed (the caller blocks) only if it is
// ErrUnrestorable or ErrPseudonym, and must not come with partial text. The
// same seeds embedded in safe random text must always succeed.
func FuzzRoundTrip(f *testing.F) {
	f.Add("", "", "", uint8(0), uint8(1), uint8(0))
	f.Add("ssh admin@", " and ", " done", uint8(0), uint8(1), uint8(1))
	f.Add("ip 192.0.2.5 ", "x", "", uint8(2), uint8(3), uint8(2))
	f.Add("1", "2", "3", uint8(4), uint8(5), uint8(3))
	f.Add("ü→", "net ", "€", uint8(6), uint8(7), uint8(4))
	f.Add("10.20.30.16 ", "123-45-6780", "", uint8(8), uint8(0), uint8(5))
	f.Add("REDACTED-", "user-", "h-", uint8(7), uint8(2), uint8(6))
	ds := builtins(f)
	f.Fuzz(func(t *testing.T, r1, r2, r3 string, a, b, sep uint8) {
		sa, sb := fuzzSeeds[int(a)%len(fuzzSeeds)], fuzzSeeds[int(b)%len(fuzzSeeds)]
		sp := string(fuzzSeps[int(sep)%len(fuzzSeps)])
		x := r1 + sp + sa.text + sp + r2 + sp + sb.text + sp + r3
		check(t, ds, x, r1+"\x00"+r2+"\x00"+r3, sa.real, sb.real, false)
		// Same seeds in safe random text (letters that cannot start or extend
		// any detected value, seeds kept apart by a letter): must never error.
		y := safe(r1) + sp + sa.text + sp + "q" + safe(r2) + "q" + sp + sb.text + sp + safe(r3)
		check(t, ds, y, "", sa.real, sb.real, true)
	})
}

var dottedQuad = regexp.MustCompile(`\d{1,3}(?:\.\d{1,3}){3}`)

// canonV4 rewrites every dotted quad in s in canonical form.
func canonV4(s string) string {
	return dottedQuad.ReplaceAllStringFunc(s, func(q string) string {
		if a, ok := ipv4.Parse(q); ok {
			return a.String()
		}
		return q
	})
}

// safeAlpha has no hex letters, no 'r'/'y' (so no "bearer"/"eyJ"), no digits
// or punctuation.
const safeAlpha = "ghijkmnopqstuvwxz "

func safe(s string) string {
	b := []byte(s)
	for i := range b {
		b[i] = safeAlpha[int(b[i])%len(safeAlpha)]
	}
	return string(b)
}

func check(t *testing.T, ds []detect.Detector, x, random, ra, rb string, mustSucceed bool) {
	t.Helper()
	v := vault.New(testKey)
	out, err := NewSanitizer(ds, v, nil).Text(x)
	if err != nil {
		if out != "" {
			t.Fatalf("error %v with partial text", err)
		}
		if !errors.Is(err, ErrUnrestorable) && !errors.Is(err, ErrPseudonym) {
			t.Fatalf("unexpected error type for %q: %v", x, err)
		}
		if mustSucceed {
			t.Fatalf("safe input %q blocked: %v", x, err)
		}
		return
	}
	for _, s := range []string{ra, rb} {
		if !strings.Contains(random, s) && strings.Contains(out, s) {
			t.Fatalf("real %q leaked in %q", s, out)
		}
	}
	// Two spellings of one IPv4 address ("0.0.0.00", "0.0.0.0") share one
	// synthetic, which restores to one spelling: compare addresses
	// canonically.
	if got := NewRestorer(v).Text(out); got != x && canonV4(got) != canonV4(x) {
		t.Fatalf("round trip:\n  in %q\n out %q\ngot %q", x, out, got)
	}
}
