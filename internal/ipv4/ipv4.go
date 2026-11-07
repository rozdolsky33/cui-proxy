// Package ipv4 parses dotted-quad IPv4 text the way the detector accepts it:
// four decimal octets of one to three digits each (leading zeros allowed),
// each at most 255. net/netip rejects leading zeros, so code that must
// recognise every address the detector finds (range checks, canonical
// forms) parses with this package. It imports nothing internal.
package ipv4

import (
	"net/netip"
	"strings"
)

// Parse parses s as a dotted quad. "198.018.001.001" is 198.18.1.1.
func Parse(s string) (netip.Addr, bool) {
	var b [4]byte
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return netip.Addr{}, false
	}
	for i, p := range parts {
		n, ok := decimal(p, 3)
		if !ok || n > 255 {
			return netip.Addr{}, false
		}
		b[i] = byte(n)
	}
	return netip.AddrFrom4(b), true
}

// ParsePrefix parses "a.b.c.d/n" (n: one to three digits, at most 32). The
// address is kept as written (not masked), in canonical form.
func ParsePrefix(s string) (netip.Prefix, bool) {
	i := strings.IndexByte(s, '/')
	if i < 0 {
		return netip.Prefix{}, false
	}
	a, ok := Parse(s[:i])
	if !ok {
		return netip.Prefix{}, false
	}
	n, ok := decimal(s[i+1:], 3)
	if !ok || n > 32 {
		return netip.Prefix{}, false
	}
	return netip.PrefixFrom(a, n), true
}

// decimal parses 1..max ASCII digits.
func decimal(p string, max int) (int, bool) {
	if len(p) == 0 || len(p) > max {
		return 0, false
	}
	n := 0
	for i := 0; i < len(p); i++ {
		if p[i] < '0' || p[i] > '9' {
			return 0, false
		}
		n = n*10 + int(p[i]-'0')
	}
	return n, true
}
