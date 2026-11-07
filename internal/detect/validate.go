package detect

import (
	"net/netip"
	"strings"

	"cui-proxy/internal/ipv4"
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

// parseV4 parses a dotted quad of decimal octets (leading zeros allowed, as
// they appear in real configs), each <= 255.
func parseV4(c string) bool {
	_, ok := ipv4.Parse(c)
	return ok
}

func ipv4Refine(text string, s, e int) (int, int, bool) {
	if !parseV4(text[s:e]) || !boundedBefore(text, s) || !boundedAfter(text, e) {
		return 0, 0, false
	}
	return s, e, true
}

// trimTail drops trailing '.' and a single trailing ':' that the permissive
// candidate pattern picked up (e.g. "fe80::1." at the end of a sentence).
func trimTail(text string, s, e int) int {
	for e > s && text[e-1] == '.' {
		e--
	}
	if e-s >= 2 && text[e-1] == ':' && text[e-2] != ':' {
		e--
	}
	return e
}

// ipv6Find scans a hex/colon/dot candidate. The candidate may swallow
// preceding hex letters ("d" of "id:fe80::1"), so every start that is not glued
// to a preceding letter/digit is tried, earliest first.
func ipv6Find(text string, s, e int) [][2]int {
	var out [][2]int
	for st := s; st < e; {
		if st > 0 && isAlnum(text[st-1]) {
			st++
			continue
		}
		en := trimTail(text, st, e)
		c := text[st:en]
		ok := false
		if len(c) >= 3 && strings.Count(c, ":") >= 2 {
			a, err := netip.ParseAddr(c)
			ok = err == nil && a.Is6() && !a.IsUnspecified() && !(en < len(text) && isAlnum(text[en]))
		}
		if ok {
			out = append(out, [2]int{st, en})
			st = en
			continue
		}
		st++
	}
	return out
}

// portOK reports whether p is 1-5 decimal digits with a value <= 65535.
func portOK(p string) bool {
	if len(p) == 0 || len(p) > 5 {
		return false
	}
	n := 0
	for i := 0; i < len(p); i++ {
		if !isDigit(p[i]) {
			return false
		}
		n = n*10 + int(p[i]-'0')
	}
	return n <= 65535
}

// addrPortFind accepts a candidate of exactly five dotted numeric components
// (tcpdump's "a.b.c.d.port") whose first four are an IPv4 address and whose
// fifth is a port, not continued on either side by letters, digits or dotted
// digits. A run of five single-digit components ("1.2.3.4.5") is taken for a
// version number and left alone.
func addrPortFind(text string, s, e int) [][2]int {
	if !boundedBefore(text, s) || !boundedAfter(text, e) {
		return nil
	}
	c := text[s:e]
	i := strings.LastIndexByte(c, '.')
	if !parseV4(c[:i]) || !portOK(c[i+1:]) || len(c) == 9 {
		return nil // len 9: five single digits and four dots
	}
	return [][2]int{{s, e}}
}

// v6Port reports whether c is an IPv6 address followed by ".<port>" (and is
// not itself an address, as "::ffff:10.20.30.15" is).
func v6Port(c string) bool {
	i := strings.LastIndexByte(c, '.')
	if i < 0 || !portOK(c[i+1:]) || strings.Count(c[:i], ":") < 2 {
		return false
	}
	if _, err := netip.ParseAddr(c); err == nil {
		return false
	}
	a, err := netip.ParseAddr(c[:i])
	return err == nil && a.Is6() && !a.IsUnspecified()
}

// ipv6PortFind is ipv6Find for tcpdump's "addr.port" IPv6 notation
// ("2001:db8::1.443"): the address plus ".port" is one span.
func ipv6PortFind(text string, s, e int) [][2]int {
	var out [][2]int
	for st := s; st < e; {
		if st > 0 && isAlnum(text[st-1]) {
			st++
			continue
		}
		en := trimTail(text, st, e)
		if v6Port(text[st:en]) && !(en < len(text) && isAlnum(text[en])) {
			out = append(out, [2]int{st, en})
			st = en
			continue
		}
		st++
	}
	return out
}

func parsePrefixAny(c string) bool {
	if !strings.Contains(c, ":") {
		_, ok := ipv4.ParsePrefix(c)
		return ok
	}
	_, err := netip.ParsePrefix(c)
	return err == nil
}

func cidrFind(text string, s, e int) [][2]int {
	for st := s; st < e; st++ {
		if st > 0 && isAlnum(text[st-1]) {
			continue
		}
		c := text[st:e]
		if !parsePrefixAny(c) || (e < len(text) && isAlnum(text[e])) {
			continue
		}
		if !strings.Contains(c, ":") && (!boundedBefore(text, st) || !boundedAfter(text, e)) {
			continue
		}
		return [][2]int{{st, e}}
	}
	return nil
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

// ssnRefine: '_' and letters count as boundaries; only adjacent digits reject.
func ssnRefine(text string, s, e int) (int, int, bool) {
	if s > 0 && isDigit(text[s-1]) || e < len(text) && isDigit(text[e]) {
		return 0, 0, false
	}
	return s, e, ssnOK(text[s:e])
}

func luhnDigits(digits []int) bool {
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
	return luhnDigits(digits)
}

func luhnRefine(text string, s, e int) (int, int, bool) { return s, e, luhnOK(text[s:e]) }

// cardGroups splits a candidate text[s:e] (digit groups separated by single
// spaces/dashes) into its digit groups.
func cardGroups(text string, s, e int) [][2]int {
	var gs [][2]int
	for i := s; i < e; {
		j := i
		for j < e && isDigit(text[j]) {
			j++
		}
		gs = append(gs, [2]int{i, j})
		i = j + 1 // skip the single separator
	}
	return gs
}

// cardFind reports card windows that span several digit groups (they contain
// a single space or dash separator, e.g. "4111 1111 1111 1111"): windows that
// start and end at group boundaries, at most 19 digits, Luhn-valid, no prefix
// filter. These keep class card (a synthetic with the same grouping).
// Unseparated digit runs are cardRunFind's job. Windows may overlap; resolve
// merges them, so a spurious window never hides the rest of a card.
func cardFind(text string, s, e int) [][2]int {
	gs := cardGroups(text, s, e)
	var out [][2]int
	for i := range gs {
		best, digits := -1, gs[i][1]-gs[i][0]
		for j := i + 1; j < len(gs); j++ {
			digits += gs[j][1] - gs[j][0]
			if digits > 19 {
				break
			}
			if luhnOK(text[gs[i][0]:gs[j][1]]) && !inUUID(text, gs[i][0], gs[j][1]) {
				best = j
			}
		}
		if best >= 0 {
			out = append(out, [2]int{gs[i][0], gs[best][1]})
		}
	}
	return out
}

// cardRunFind reports a whole unseparated digit group (class secret, an
// opaque self-delimiting REDACTED-… token) when it contains a Luhn hit:
//   - the whole group has 14-19 digits and is Luhn-valid (no prefix filter, so
//     2-series PANs such as 2223000048400011 are caught);
//   - the whole group has 13 digits, starts with 3-6 and is Luhn-valid (a
//     Luhn-valid 13-digit millisecond timestamp such as 1700000000004 must
//     not be masked);
//   - a strict sub-window (13-19 digits, shorter than the group) starts with
//     3-6 and is Luhn-valid.
//
// Masking the whole run (rather than a card-shaped synthetic) is the safe
// direction: the token restores even when the run is glued to letters, '_',
// '-' or dotted digits, where a digit synthetic would be refused by the
// restorer's boundary rule and block the request.
func cardRunFind(text string, s, e int) [][2]int {
	var out [][2]int
	for _, g := range cardGroups(text, s, e) {
		if luhnRun(text, g[0], g[1]) && !inUUID(text, g[0], g[1]) {
			out = append(out, g)
		}
	}
	return out
}

// uuidLen is the length of a canonical UUID: 8-4-4-4-12 hex digits.
const uuidLen = 36

// inUUID reports whether text[s:e] lies inside a canonical UUID token
// (8-4-4-4-12 hex digits, any case) bounded by non-alphanumerics. Digit runs
// in session and turn ids are not cards. Only the bytes around the window
// are examined, so the check is O(1) per candidate.
func inUUID(text string, s, e int) bool {
	if e-s > uuidLen {
		return false
	}
	for u := max(0, e-uuidLen); u <= s && u+uuidLen <= len(text); u++ {
		if isUUID(text[u:u+uuidLen]) && (u == 0 || !isAlnum(text[u-1])) &&
			(u+uuidLen == len(text) || !isAlnum(text[u+uuidLen])) {
			return true
		}
	}
	return false
}

func isUUID(c string) bool {
	for i := 0; i < len(c); i++ {
		switch i {
		case 8, 13, 18, 23:
			if c[i] != '-' {
				return false
			}
		default:
			if b := c[i]; !isDigit(b) && !(b >= 'a' && b <= 'f') && !(b >= 'A' && b <= 'F') {
				return false
			}
		}
	}
	return true
}

func luhnRun(text string, gs, ge int) bool {
	glen := ge - gs
	issuer := func(i int) bool { return text[i] >= '3' && text[i] <= '6' }
	if glen >= 13 && glen <= 19 && (glen >= 14 || issuer(gs)) && luhnOK(text[gs:ge]) {
		return true
	}
	for st := gs; st+13 <= ge; st++ {
		if !issuer(st) {
			continue
		}
		for n := 13; n <= 19 && st+n <= ge; n++ {
			if n != glen && luhnOK(text[st:st+n]) {
				return true
			}
		}
	}
	return false
}

// ocidRefine requires the match not to be glued to a preceding letter/digit.
func ocidRefine(text string, s, e int) (int, int, bool) {
	if s > 0 && isAlnum(text[s-1]) {
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
