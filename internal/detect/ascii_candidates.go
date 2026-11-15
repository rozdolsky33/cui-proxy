package detect

// ipv6Candidates is the exact greedy candidate language [0-9A-Fa-f:.]{3,}.
// Address and port validation remains in the existing finders. All accepted
// bytes are ASCII, so Unicode and invalid UTF-8 bytes are separators just as
// they are for the original regexp; offsets remain relative to the input.
func ipv6Candidates(s string) [][]int {
	var out [][]int
	for pos := 0; pos < len(s); {
		start := pos
		for pos < len(s) && ipv6CandidateByte(s[pos]) {
			pos++
		}
		if pos-start >= 3 {
			out = append(out, []int{start, pos})
		}
		if pos < len(s) {
			pos++
		}
	}
	return out
}

func ipv6CandidateByte(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'A' && b <= 'F' ||
		b >= 'a' && b <= 'f' || b == ':' || b == '.'
}

// cardCandidates is the exact greedy candidate language \d(?:[ -]?\d)*.
// A separator is consumed only when followed immediately by another ASCII
// digit. Length, checksum and surrounding-boundary checks stay in the finders.
func cardCandidates(s string) [][]int {
	var out [][]int
	for pos := 0; pos < len(s); {
		if s[pos] < '0' || s[pos] > '9' {
			pos++
			continue
		}
		start := pos
		pos++
		for pos < len(s) {
			if s[pos] >= '0' && s[pos] <= '9' {
				pos++
			} else if (s[pos] == ' ' || s[pos] == '-') && pos+1 < len(s) && s[pos+1] >= '0' && s[pos+1] <= '9' {
				pos += 2
			} else {
				break
			}
		}
		out = append(out, []int{start, pos})
	}
	return out
}
