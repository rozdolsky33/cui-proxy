package transform

import (
	"strings"
	"testing"

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
// exact input. An error is allowed (the caller blocks) but must not come with
// partial text.
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
		v := vault.New(testKey)
		out, err := NewSanitizer(ds, v, nil).Text(x)
		if err != nil {
			if out != "" {
				t.Fatalf("error %v with partial text", err)
			}
			return
		}
		random := r1 + "\x00" + r2 + "\x00" + r3
		for _, s := range []string{sa.real, sb.real} {
			if !strings.Contains(random, s) && strings.Contains(out, s) {
				t.Fatalf("real %q leaked in %q", s, out)
			}
		}
		if got := NewRestorer(v).Text(out); got != x {
			t.Fatalf("round trip:\n  in %q\n out %q\ngot %q", x, out, got)
		}
	})
}
