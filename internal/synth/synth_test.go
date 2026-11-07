package synth

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
	"testing"
)

func sumOf(class, real string) []byte {
	m := hmac.New(sha256.New, []byte("0123456789abcdef0123456789abcdef"))
	m.Write([]byte(class + "\x00" + real))
	return m.Sum(nil)
}

func gen(t *testing.T, class, real string) string {
	t.Helper()
	s, err := Generate(class, sumOf(class, real), 0, real)
	if err != nil {
		t.Fatalf("%s: %v", class, err)
	}
	return s
}

func luhn(digits string) bool {
	sum, alt := 0, false
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if alt {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		alt = !alt
	}
	return sum%10 == 0
}

func TestKnown(t *testing.T) {
	for _, c := range []string{"ipv4", "ipv6", "cidr", "hostname", "email", "ocid", "ssn", "card", "secret"} {
		if !Known(c) {
			t.Errorf("%s should be known", c)
		}
	}
	if Known("nope") || Known("") {
		t.Error("unknown class reported known")
	}
	if _, err := Generate("nope", sumOf("nope", "x"), 0, "x"); err == nil {
		t.Error("Generate of unknown class should fail")
	}
}

func TestGenerateFormats(t *testing.T) {
	var nets []netip.Prefix
	for _, p := range []string{"192.0.2.0/24", "198.51.100.0/24", "203.0.113.0/24"} {
		nets = append(nets, netip.MustParsePrefix(p))
	}
	for i := 0; i < 50; i++ {
		real := fmt.Sprintf("10.0.0.%d", i)
		a, err := netip.ParseAddr(gen(t, "ipv4", real))
		if err != nil {
			t.Fatal(err)
		}
		ok := false
		for _, n := range nets {
			ok = ok || n.Contains(a)
		}
		if !ok {
			t.Errorf("ipv4 %s outside RFC 5737", a)
		}
	}
	a, err := netip.ParseAddr(gen(t, "ipv6", "fd00::1"))
	if err != nil || !netip.MustParsePrefix("2001:db8::/32").Contains(a) {
		t.Errorf("ipv6 %v %v", a, err)
	}
	c := gen(t, "cidr", "10.1.2.0/22")
	p, err := netip.ParsePrefix(c)
	if err != nil || p.Bits() != 22 || !strings.HasSuffix(c, "/22") {
		t.Errorf("cidr %q %v", c, err)
	}
	ok := false
	for _, n := range nets {
		ok = ok || n.Contains(p.Addr())
	}
	if !ok {
		t.Errorf("cidr addr outside 5737: %s", c)
	}
	checks := []struct{ class, real, re string }{
		{"hostname", "db.corp.internal", `^h-[0-9a-f]{8}\.example$`},
		{"email", "alice@corp.com", `^user-[0-9a-f]{8}@mail\.example$`},
		{"ocid", "ocid1.instance.oc1.phx.abcdef123456", `^ocid1\.instance\.ocsynth\.\.[0-9a-f]{26}$`},
		{"ssn", "123-45-6789", `^000-\d{2}-\d{4}$`},
		{"secret", "hunter2hunter2", `^REDACTED-[0-9a-f]{8}$`},
	}
	for _, c := range checks {
		if got := gen(t, c.class, c.real); !regexp.MustCompile(c.re).MatchString(got) {
			t.Errorf("%s: %q !~ %s", c.class, got, c.re)
		}
	}
	for _, real := range []string{"4111111111111111", "4111 1111 1111 1111", "4111-1111-1111-1111", "378282246310005", "4222222222222"} {
		got := gen(t, "card", real)
		if len(got) != len(real) {
			t.Errorf("card %q -> %q length", real, got)
		}
		var digits strings.Builder
		for i := range got {
			if got[i] >= '0' && got[i] <= '9' {
				digits.WriteByte(got[i])
			} else if got[i] != real[i] {
				t.Errorf("card grouping not preserved: %q -> %q", real, got)
			}
		}
		if !strings.HasPrefix(digits.String(), "400000") || !luhn(digits.String()) {
			t.Errorf("card %q not 400000-prefixed Luhn-valid", got)
		}
	}
}

func TestGenerateDeterministic(t *testing.T) {
	for _, c := range []string{"ipv4", "ipv6", "cidr", "hostname", "email", "ocid", "ssn", "card", "secret"} {
		real := map[string]string{"ipv4": "10.0.0.1", "ipv6": "fd00::1", "cidr": "10.0.0.0/8", "hostname": "a.b",
			"email": "a@b.co", "ocid": "ocid1.vcn.oc1..xyz", "ssn": "123-45-6789", "card": "4111111111111111", "secret": "s3cr3tvalue"}[c]
		a, err1 := Generate(c, sumOf(c, real), 0, real)
		b, err2 := Generate(c, sumOf(c, real), 0, real)
		if err1 != nil || err2 != nil || a != b {
			t.Errorf("%s not deterministic: %q %q", c, a, b)
		}
		d, err := Generate(c, sumOf(c, real), 1, real)
		if err != nil || d == a {
			t.Errorf("%s attempt 1 should differ: %q %q (%v)", c, a, d, err)
		}
	}
}

func TestIPv4AttemptsCoverPoolThenError(t *testing.T) {
	rfc5737 := []netip.Prefix{
		netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	}
	overflow := netip.MustParsePrefix("198.18.0.0/15")
	in := func(a netip.Addr, ps ...netip.Prefix) bool {
		for _, p := range ps {
			if p.Contains(a) {
				return true
			}
		}
		return false
	}
	if n := Attempts("ipv4"); n != 762+OverflowPool {
		t.Fatalf("Attempts(ipv4) = %d", n)
	}
	sum := sumOf("ipv4", "x")
	seen := map[string]bool{}
	for i := 0; i < 762+OverflowPool; i++ {
		s, err := Generate("ipv4", sum, i, "x")
		if err != nil {
			t.Fatalf("attempt %d: %v", i, err)
		}
		a := netip.MustParseAddr(s)
		last := a.As4()[3]
		switch {
		case i < 762 && !in(a, rfc5737...):
			t.Fatalf("attempt %d = %s: not in RFC 5737 (the overflow pool must come last)", i, s)
		case i >= 762 && !in(a, overflow):
			t.Fatalf("attempt %d = %s: not in 198.18.0.0/15", i, s)
		case last == 0 || last == 255:
			t.Fatalf("attempt %d = %s: network or broadcast-looking address", i, s)
		case InOverflow(s) != (i >= 762):
			t.Fatalf("InOverflow(%s) = %v", s, InOverflow(s))
		}
		seen[s] = true
	}
	if len(seen) != 762+OverflowPool {
		t.Errorf("pool coverage %d", len(seen))
	}
	_, err := Generate("ipv4", sum, 762+OverflowPool, "x")
	if !errors.Is(err, ErrExhausted) || err.Error() != "synthetic pool exhausted" {
		t.Errorf("exhaustion error = %v", err)
	}
	if Attempts("hostname") != 4096 {
		t.Errorf("Attempts(hostname) = %d", Attempts("hostname"))
	}
}

func TestInOverflow(t *testing.T) {
	for s, want := range map[string]bool{
		"198.18.0.1": true, "198.19.255.254": true, "198.17.255.255": false, "198.20.0.0": false,
		"192.0.2.1": false, "198.18.4.0/24": true, "198.0.0.0/8": true, "10.0.0.0/8": false,
		"::ffff:198.18.1.1": true, "h-1.example": false, "": false, "2001:db8::1": false,
	} {
		if got := InOverflow(s); got != want {
			t.Errorf("InOverflow(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestBadInputsError(t *testing.T) {
	if _, err := Generate("card", sumOf("card", "123"), 0, "123"); err == nil {
		t.Error("short card should error")
	}
	if _, err := Generate("cidr", sumOf("cidr", "nonsense"), 0, "nonsense"); err == nil {
		t.Error("bad cidr should error")
	}
	if _, err := Generate("ipv4", []byte{1}, 0, "x"); err == nil {
		t.Error("short sum should error")
	}
}

// S3: leading-zero dotted quads (which the detector accepts) are recognised.
func TestInOverflowLeadingZeros(t *testing.T) {
	for s, want := range map[string]bool{
		"198.018.001.001": true, "198.18.001.1": true, "198.018.1.1/24": true, "198.019.255.254": true,
		"198.017.1.1": false, "010.20.30.15": false,
		"0198.18.1.1": false, // not a dotted quad; the detector does not report it either
	} {
		if got := InOverflow(s); got != want {
			t.Errorf("InOverflow(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestCanonical(t *testing.T) {
	for _, tc := range []struct {
		class, in, want string
		ok              bool
	}{
		{"ipv4", "198.018.001.001", "198.18.1.1", true},
		{"ipv4", "10.20.30.15", "10.20.30.15", true},
		{"ipv4", "999.1.1.1", "", false},
		{"ipv4", "not an ip", "", false},
		{"cidr", "198.018.1.1/24", "198.18.1.1/24", true},
		{"cidr", "2001:DB8::/32", "2001:db8::/32", true},
		{"cidr", "1.2.3.4/99", "", false},
		{"hostname", "Db01.Example", "Db01.Example", true},
	} {
		got, ok := Canonical(tc.class, tc.in)
		if ok != tc.ok || ok && got != tc.want {
			t.Errorf("Canonical(%s, %q) = %q %v, want %q %v", tc.class, tc.in, got, ok, tc.want, tc.ok)
		}
	}
}
