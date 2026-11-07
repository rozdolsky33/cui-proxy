package detect

import (
	"net/netip"
	"strings"
)

// A refiner inspects a regex match text[s:e] and returns the (possibly
// trimmed) span, or ok=false to reject it.
type refiner func(text string, s, e int) (int, int, bool)

func isAlnum(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// boundedBefore reports whether the match does not continue to the left: the
// previous byte is not a letter or digit, nor a '.' that follows a digit.
func boundedBefore(text string, s int) bool {
	if s == 0 {
		return true
	}
	if isAlnum(text[s-1]) {
		return false
	}
	return !(text[s-1] == '.' && s >= 2 && isDigit(text[s-2]))
}

// boundedAfter reports whether the match does not continue to the right.
func boundedAfter(text string, e int) bool {
	if e >= len(text) {
		return true
	}
	if isAlnum(text[e]) {
		return false
	}
	return !(text[e] == '.' && e+1 < len(text) && isDigit(text[e+1]))
}

func ipv4Refine(text string, s, e int) (int, int, bool) {
	a, err := netip.ParseAddr(text[s:e])
	if err != nil || !a.Is4() {
		return 0, 0, false
	}
	if !boundedBefore(text, s) || !boundedAfter(text, e) {
		return 0, 0, false
	}
	return s, e, true
}

// trimIPv6 drops a stray single leading/trailing ':' or trailing '.' that the
// permissive candidate pattern picked up (e.g. "addr:fe80::1").
func trimIPv6(text string, s, e int) (int, int) {
	for e > s && text[e-1] == '.' {
		e--
	}
	if e-s >= 2 && text[s] == ':' && text[s+1] != ':' {
		s++
	}
	if e-s >= 2 && text[e-1] == ':' && text[e-2] != ':' {
		e--
	}
	return s, e
}

func ipv6Refine(text string, s, e int) (int, int, bool) {
	s, e = trimIPv6(text, s, e)
	c := text[s:e]
	if strings.Count(c, ":") < 2 || len(c) < 3 {
		return 0, 0, false
	}
	a, err := netip.ParseAddr(c)
	if err != nil || !a.Is6() || a.IsUnspecified() {
		return 0, 0, false
	}
	if s > 0 && isAlnum(text[s-1]) || e < len(text) && isAlnum(text[e]) {
		return 0, 0, false
	}
	return s, e, true
}

func cidrRefine(text string, s, e int) (int, int, bool) {
	s, e = trimIPv6(text, s, e)
	p, err := netip.ParsePrefix(text[s:e])
	if err != nil {
		return 0, 0, false
	}
	if p.Addr().Is4() && (!boundedBefore(text, s) || !boundedAfter(text, e)) {
		return 0, 0, false
	}
	if s > 0 && isAlnum(text[s-1]) || e < len(text) && isAlnum(text[e]) {
		return 0, 0, false
	}
	return s, e, true
}

func ssnOK(c string) bool {
	if len(c) != 11 || c[3] != '-' || c[6] != '-' {
		return false
	}
	area, group, serial := c[0:3], c[4:6], c[7:11]
	if area == "000" || area == "666" || area[0] == '9' {
		return false
	}
	return group != "00" && serial != "0000"
}

func ssnRefine(text string, s, e int) (int, int, bool) { return s, e, ssnOK(text[s:e]) }

// luhnOK strips spaces and dashes and checks for 13-19 digits with a valid Luhn sum.
func luhnOK(c string) bool {
	var digits []int
	for i := 0; i < len(c); i++ {
		switch {
		case isDigit(c[i]):
			digits = append(digits, int(c[i]-'0'))
		case c[i] == ' ' || c[i] == '-':
		default:
			return false
		}
	}
	if len(digits) < 13 || len(digits) > 19 {
		return false
	}
	sum := 0
	for i, alt := len(digits)-1, false; i >= 0; i-- {
		d := digits[i]
		if alt {
			if d *= 2; d > 9 {
				d -= 9
			}
		}
		sum += d
		alt = !alt
	}
	return sum%10 == 0
}

func luhnRefine(text string, s, e int) (int, int, bool) { return s, e, luhnOK(text[s:e]) }

// ocidRefine requires the match not to continue a longer word on the left.
func ocidRefine(text string, s, e int) (int, int, bool) {
	if s > 0 && (isAlnum(text[s-1]) || text[s-1] == '_' || text[s-1] == '-') {
		return 0, 0, false
	}
	return s, e, true
}

var refiners = map[string]refiner{
	"ipv4": ipv4Refine,
	"ssn":  ssnRefine,
	"luhn": luhnRefine,
	"ocid": ocidRefine,
}
