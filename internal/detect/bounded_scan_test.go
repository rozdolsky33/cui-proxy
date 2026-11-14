package detect

import (
	"math/rand"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// Wrong nullability/first-character/width analysis can silently omit a match.
func TestBoundedDigitWidth(t *testing.T) {
	cases := []struct {
		pattern string
		want    int
	}{
		{`\d{1,3}(?:\.\d{1,3}){3}`, 15},
		{`\d{3}-\d{2}-\d{4}`, 11},
		{`\b(\d{2,4})\b`, 4},
		{`(?:\d{2}|\d{4})x?`, 5},
		{`(?:\d)?\d{2}`, 3},
		{`(?:a)?\d{2}`, 0},
		{`(?:\d|a)`, 0},
		{`\d*`, 0}, {`\d+`, 0}, {`\d{0,3}`, 0},
		{`\d{257}`, 0}, {`\d{256}`, 256},
		{`\d\p{L}`, 0}, {`(?i)\dk`, 0},
		{`^\d{3}`, 0}, {`\d{3}$`, 0}, {`(?m)^\d{3}`, 0},
		{`\A\d{3}`, 0}, {`\d{3}\z`, 0},
		{`123\d{2}`, 0}, {``, 0}, {`[0-9a]`, 0},
		{`(?:\b)?\d{2}`, 2}, {`\d(?:x{0})`, 1},
	}
	for i, c := range cases {
		if got := boundedDigitWidth(regexp.MustCompile(c.pattern)); got != c.want {
			t.Errorf("case %d: width=%d want=%d", i, got, c.want)
		}
	}
}

// Incorrect window context, offset translation or fallback loses matches/captures.
func TestBoundedMatches(t *testing.T) {
	patterns := []string{`\d{1,3}(?:\.\d{1,3}){3}`, `\b(\d{2,4})\b`, `\d{1,3}?`, `(?:\d{2}|\d{4})x?`, `\B\d{2}\B`, `(\d)(a)?`}
	inputs := []string{"", "7", "123456", strings.Repeat("ordinary prose ", 400) + " 12 3456x 192.0.2.17 ", strings.Repeat("1234567890.", 600), strings.Repeat("a", 4100) + "12 34\xff", "é" + strings.Repeat(" ", 4200) + "34", strings.Repeat(" ", 4200) + "12é34", strings.Repeat("a12 a3456 ", 600)}
	rng := rand.New(rand.NewSource(81))
	alphabet := []byte("a09.- \n")
	for n := 0; n < 100; n++ {
		buf := make([]byte, 4096+rng.Intn(1000))
		for i := range buf {
			buf[i] = alphabet[rng.Intn(len(alphabet))]
		}
		inputs = append(inputs, string(buf))
	}
	for pi, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		width := boundedDigitWidth(re)
		if width == 0 {
			t.Fatalf("pattern %d unexpectedly ineligible", pi)
		}
		for si, s := range inputs {
			if !reflect.DeepEqual(boundedMatches(re, s, width), re.FindAllStringSubmatchIndex(s, -1)) {
				t.Fatalf("pattern %d input %d: indices differ", pi, si)
			}
		}
	}
}

func FuzzBoundedMatches(f *testing.F) {
	for _, s := range []string{"", "12", strings.Repeat(" ", 4100) + "12x34", strings.Repeat("9", 4200), strings.Repeat(" ", 4200) + "12\xff"} {
		for selector := uint8(0); selector < 6; selector++ {
			f.Add(s, selector)
		}
	}
	patterns := []string{`\d{1,3}(?:\.\d{1,3}){3}`, `\b(\d{2,4})\b`, `\d{1,3}?`, `(?:\d{2}|\d{4})x?`, `\B\d{2}\B`, `(\d)(a)?`}
	f.Fuzz(func(t *testing.T, s string, selector uint8) {
		if len(s) > 1<<20 {
			t.Skip()
		}
		re := regexp.MustCompile(patterns[int(selector)%len(patterns)])
		if !reflect.DeepEqual(boundedMatches(re, s, boundedDigitWidth(re)), re.FindAllStringSubmatchIndex(s, -1)) {
			t.Fatal("indices differ")
		}
	})
}

func TestBoundedDetectorRefiners(t *testing.T) {
	inputs := []string{
		strings.Repeat("harmless prose ", 400) + " 192.0.2.17 999.999.999.999 192.0.2.17.443 123-45-6789 ",
		strings.Repeat(" ", 4200) + " 0123.0.2.1 123-00-6789 192.0.2.1x ",
	}
	for i, item := range builtinSet("builtin_") {
		candidate := item.(*regexDetector)
		reference := *candidate
		reference.boundedWidth = 0
		for j, s := range inputs {
			if !reflect.DeepEqual(candidate.Find(s), reference.Find(s)) {
				t.Fatalf("detector %d input %d: spans differ", i, j)
			}
		}
	}
}
