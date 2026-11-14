package detect

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestFoldedLiteralNecessary(t *testing.T) {
	for index, pattern := range []string{`(?i)bearer=[a-z]+`, `(?i)(?:alpha|alphabet)`, `(?i)[ \t]+marker[0-9]+`, `(?i)Key=[a-z]+`, `(?i)ſentinel=[a-z]+`} {
		re := regexp.MustCompile(pattern)
		required := foldedDetectorLiteral(re)
		if required == "" {
			t.Fatalf("missing necessary folded literal at case %d", index)
		}
		for _, input := range []string{"BEARER=value", "alphabet", " MARKER42", "KEY=value", "SENTINEL=value", "unrelated padding"} {
			if re.MatchString(input) && !strings.Contains(strings.ToLower(input), required) {
				t.Fatal("matched input excluded")
			}
		}
	}
}

func TestFoldedLiteralOptionalBranches(t *testing.T) {
	for _, pattern := range []string{`(?i)(?:marker)?`, `(?i)(?:alpha|[0-9]+)`, `(?i).*`, `(?i)marker{0,3}`} {
		re := regexp.MustCompile(pattern)
		required := foldedDetectorLiteral(re)
		for _, input := range []string{"", "42", "marke", "unrelated padding"} {
			if re.MatchString(input) && required != "" && !strings.Contains(strings.ToLower(input), required) {
				t.Fatal("optional branch excluded")
			}
		}
	}
}

func FuzzFoldedLiteralNecessary(f *testing.F) {
	for _, pattern := range []string{`(?i)needle`, `(?i)(?:K|ſ)ey`, `(?i)(alpha|alphabet)`, `(?i)(a?b)+`, `(?i)[a-z]+KEY[0-9]+`} {
		f.Add(pattern, "KEY42 needle alphabet")
	}
	f.Fuzz(func(t *testing.T, pattern, input string) {
		if len(pattern) > 1024 || len(input) > 8192 {
			t.Skip()
		}
		for i := 0; i < len(input); i++ {
			if input[i] >= 128 {
				return
			}
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return
		}
		required := foldedDetectorLiteral(re)
		if required != "" && re.MatchString(input) && !strings.Contains(strings.ToLower(input), required) {
			t.Fatal("necessary-condition violation")
		}
	})
}

func TestFoldedScanPreservesMatchesAndBlocking(t *testing.T) {
	var ds []Detector
	for _, pattern := range []string{`(?i)marker=[a-z]+`, `(?i)Key=[a-z]+`, `(?i)ſentinel=[a-z]+`, `(?i)(?:alpha|alphabet)`, `(?i)(a?b)+`, `(?i)[a-z]+KEY[0-9]+`, `(?i)token`, `(?i)phrase`} {
		d, err := newRegex("folded-test", "secret", pattern, "")
		if err != nil {
			t.Fatal("compile")
		}
		ds = append(ds, d)
	}
	ds[2] = blocking{ds[2]}
	for _, tail := range []string{"MARKER=value KEY=value SENTINEL=value", "Key=value ſentinel=value", "\xff KEY=value", "alphabet", "nothing"} {
		input := strings.Repeat("ordinary padding ", 400) + tail
		var expected []Span
		for _, d := range ds {
			expected = append(expected, d.Find(input)...)
		}
		expected = resolve(expected)
		if !reflect.DeepEqual(Scan(ds, input), expected) {
			t.Fatal("scan parity failed")
		}
	}
}

func TestFoldedPrecheckFallbackAndSkip(t *testing.T) {
	d, err := newRegex("folded-test", "secret", `(?i)marker=[a-z]+`, "")
	if err != nil {
		t.Fatal("compile")
	}
	ds := []Detector{blocking{d}}
	padding := strings.Repeat("ordinary padding ", 400)
	folded, ok := prepareFoldedText(ds, padding)
	if !ok || !skipFolded(ds[0], folded, ok) {
		t.Fatal("absent required literal was not skipped")
	}
	folded, ok = prepareFoldedText(ds, padding+" MARKER=value")
	if !ok || skipFolded(ds[0], folded, ok) {
		t.Fatal("present required literal was skipped")
	}
	for _, input := range []string{"short", padding + "K", padding + "\xff"} {
		folded, ok = prepareFoldedText(ds, input)
		if ok || skipFolded(ds[0], folded, ok) {
			t.Fatal("fallback was skipped")
		}
	}
}

func TestFoldedDictionaryPrecheck(t *testing.T) {
	d, err := newDict("folded-dictionary", "secret", "marker-one\nmarker-two\nKey-value\n")
	if err != nil {
		t.Fatal("dictionary compile")
	}
	// Heterogeneous alternatives can legitimately have no common substring.
	homogeneous, err := newDict("folded-dictionary", "secret", "marker-one\nmarker-two\nmarker-long\n")
	if err != nil {
		t.Fatal("dictionary compile")
	}
	padding := strings.Repeat("ordinary padding ", 400)
	folded, ok := prepareFoldedText([]Detector{homogeneous}, padding)
	if !ok || !skipFolded(homogeneous, folded, ok) {
		t.Fatal("dictionary necessary precheck missing")
	}
	for _, detector := range []Detector{d, homogeneous, blocking{homogeneous}} {
		for _, tail := range []string{" MARKER-ONE", "marker-longer", " Key-value", " KEY-value", "\xffmarker-two", " unrelated"} {
			input := padding + tail
			if !reflect.DeepEqual(Scan([]Detector{detector}, input), resolve(detector.Find(input))) {
				t.Fatal("dictionary parity failed")
			}
		}
	}
}

func FuzzFoldedScanParity(f *testing.F) {
	f.Add(`(?i)marker=[a-z]+`, " MARKER=value")
	f.Add(`(?i)Key=[a-z]+`, " Key=value")
	f.Add(`(?i)ſentinel=[a-z]+`, "\xffSENTINEL=value")
	f.Fuzz(func(t *testing.T, pattern, suffix string) {
		if len(pattern) > 1024 || len(suffix) > 8192 {
			t.Skip()
		}
		d, err := newRegex("folded-fuzz", "secret", pattern, "")
		if err != nil {
			return
		}
		input := strings.Repeat("ordinary padding ", 250) + suffix
		var detector Detector = blocking{d}
		if !reflect.DeepEqual(Scan([]Detector{detector}, input), resolve(detector.Find(input))) {
			t.Fatal("folded scan parity violation")
		}
	})
}
