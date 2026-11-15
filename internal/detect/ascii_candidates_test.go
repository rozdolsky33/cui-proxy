package detect

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

var candidateOracles = []struct {
	name string
	re   *regexp.Regexp
	find func(string) [][]int
}{
	{"ipv6", regexp.MustCompile(`[0-9A-Fa-f:.]{3,}`), ipv6Candidates},
	{"card", regexp.MustCompile(`\d(?:[ -]?\d)*`), cardCandidates},
}

func checkCandidateParity(t testing.TB, text string) {
	t.Helper()
	for _, oracle := range candidateOracles {
		if !reflect.DeepEqual(oracle.find(text), oracle.re.FindAllStringSubmatchIndex(text, -1)) {
			t.Fatalf("candidate parity failed: scanner=%s bytes=%d", oracle.name, len(text))
		}
	}
}

func TestASCIICandidateParity(t *testing.T) {
	for _, text := range []string{
		"", "a", "ab", "abc", "abcgdef", "...", ":::g::", "aBc.DeF",
		"0", "0-1 2", "0--1", "0  1", "0- 1", "0-", "-0", "1\t2\n3",
		"éabcKdefſ123١٢٣456", string([]byte{'a', 'b', 'c', 0xff, '1', '-', '2'}),
		strings.Repeat("a", 1<<20), strings.Repeat("0-1 ", 4096),
	} {
		checkCandidateParity(t, text)
	}
}

func TestASCIICandidateExhaustiveParity(t *testing.T) {
	alphabet := []byte{'0', '9', 'a', 'F', 'g', ':', '.', ' ', '-', 0xff}
	var visit func([]byte, int)
	visit = func(prefix []byte, remaining int) {
		checkCandidateParity(t, string(prefix))
		if remaining == 0 {
			return
		}
		for _, b := range alphabet {
			visit(append(prefix, b), remaining-1)
		}
	}
	visit(nil, 5)
}

func TestASCIICandidateBuiltinScanParity(t *testing.T) {
	fast := builtinSet("parity.")
	original := make([]Detector, len(fast))
	for i, detector := range fast {
		copy := *detector.(*regexDetector)
		copy.candidates = nil
		original[i] = &copy
	}
	inputs := []string{
		strings.Repeat("ordinary prose ", 400),
		strings.Repeat("abc:g 0--1 ", 500),
		strings.Join([]string{"abcd", "1", "2", "3", "4", "5", "6", "7"}, ":"),
		strings.Repeat("0", 20),
		"éabc\xff0-1 2",
	}
	for _, input := range inputs {
		for _, text := range []string{input, strings.Repeat("prose ", 800) + input} {
			if !reflect.DeepEqual(Scan(fast, text), Scan(original, text)) {
				t.Fatalf("builtin scan parity failed: bytes=%d", len(text))
			}
		}
	}
	for i := range fast {
		fast[i] = blocking{fast[i]}
		original[i] = blocking{original[i]}
	}
	for _, text := range inputs {
		if !reflect.DeepEqual(Scan(fast, text), Scan(original, text)) {
			t.Fatalf("blocking scan parity failed: bytes=%d", len(text))
		}
	}
}

func FuzzASCIICandidateParity(f *testing.F) {
	for _, seed := range []string{"", "abcgdef", "0-1 2", "0--1", "éabc", "a\xffbc"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) { checkCandidateParity(t, text) })
}
