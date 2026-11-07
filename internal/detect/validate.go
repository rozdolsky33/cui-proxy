package detect

import (
	"net/netip"
	"strconv"
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

// parseV4 parses a dotted quad of decimal octets (leading zeros allowed, as
// they appear in real configs), each <= 255.
func parseV4(c string) bool {
	parts := strings.Split(c, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		if len(p) == 0 || len(p) > 3 {
			return false
		}
		n := 0
		for i := 0; i < len(p); i++ {
			if !isDigit(p[i]) {
				return false
			}
			n = n*10 + int(p[i]-'0')
		}
		if n > 255 {
			return false
		}
	}
	return true
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

func parsePrefixAny(c string) bool {
	if !strings.Contains(c, ":") {
		i := strings.IndexByte(c, '/')
		if i < 0 || !parseV4(c[:i]) {
			return false
		}
		n, err := strconv.Atoi(c[i+1:])
		return err == nil && n >= 0 && n <= 32
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

// cardFind reports card-shaped windows in a run of digit groups:
//   - a whole group of 14-19 digits that is Luhn-valid (no prefix filter, so
//     2-series PANs such as 2223000048400011 are caught), or of 13 digits
//     with a first digit of 3-6 (a Luhn-valid 13-digit millisecond timestamp
//     must not be masked);
//   - with several groups, windows that start and end at group boundaries
//     (at most 19 digits), without a prefix filter.
//
// Card windows strictly inside a longer digit group are not reported here:
// cardRunFind masks the whole group instead (see there). Windows may overlap;
// resolve merges them, so a spurious window never hides the rest of a card.
func cardFind(text string, s, e int) [][2]int {
	gs := cardGroups(text, s, e)
	var out [][2]int
	for _, g := range gs {
		n := g[1] - g[0]
		if n < 13 || n > 19 || n == 13 && !(text[g[0]] >= '3' && text[g[0]] <= '6') {
			continue
		}
		if luhnOK(text[g[0]:g[1]]) {
			out = append(out, g)
		}
	}
	if len(gs) == 1 {
		return out
	}
	for i := range gs {
		best, digits := -1, 0
		for j := i; j < len(gs); j++ {
			digits += gs[j][1] - gs[j][0]
			if digits > 19 {
				break
			}
			if luhnOK(text[gs[i][0]:gs[j][1]]) {
				best = j
			}
		}
		if best >= 0 {
			out = append(out, [2]int{gs[i][0], gs[best][1]})
		}
	}
	return out
}

// cardRunFind reports a whole digit group (class secret, an opaque
// REDACTED-… token) when some STRICT sub-window of it (13-19 digits, shorter
// than the group) is Luhn-valid and starts with an issuer digit 3-6. Masking
// only the sub-window would leave the synthetic glued to real digits, which
// the restorer refuses to restore, blocking ordinary numeric ids; masking the
// whole run is the safe direction (over-masking) and restores cleanly.
func cardRunFind(text string, s, e int) [][2]int {
	var out [][2]int
	for _, g := range cardGroups(text, s, e) {
		if strictCardWindow(text, g[0], g[1]) {
			out = append(out, g)
		}
	}
	return out
}

func strictCardWindow(text string, gs, ge int) bool {
	glen := ge - gs
	for st := gs; st+13 <= ge; st++ {
		if text[st] < '3' || text[st] > '6' {
			continue
		}
		for n := 13; n <= 19 && st+n <= ge; n++ {
			if n == glen {
				continue // the whole group is cardFind's job
			}
			if luhnOK(text[st : st+n]) {
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
