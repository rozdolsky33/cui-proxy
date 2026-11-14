package detect

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestBoundedSearchEligibility(t *testing.T) {
	for _, pattern := range []string{`\bTAG[0-9]{1,3}\b`, `\Btag(?:a|b){1,3}`, `\b(TAG)([0-9]{2})\b`} {
		if newBoundedSearch(regexp.MustCompile(pattern)) == nil {
			t.Fatal("bounded pattern rejected")
		}
	}
	for _, pattern := range []string{`TAG[0-9]{2}`, `\bTAG[0-9]+`, `\bTAG.*x`, `\bTAG.{300}`, `\b(?i:tag)[0-9]{2}`, `^\bTAG`, `\bTAG$`, `(\bTAG[0-9]{2})`, `\bX` + strings.Repeat(`(?:a|b)?`, 130), `\bZ` + strings.Repeat(`(?:)`, 1100), `\bZ(?:aa|bb|cc|dd|ee|ff|gg|hh|ii|jj|kk|ll|mm|nn|oo|pp|qq|rr|ss|tt|uu|vv|ww|xx|yy|zz){127}`} {
		if newBoundedSearch(regexp.MustCompile(pattern)) != nil {
			t.Fatal("unsupported pattern accepted")
		}
	}
}

func TestBoundedSearchPreservesContextAndCaptures(t *testing.T) {
	cases := []struct{ pattern, text string }{
		{`\b(TAG)([0-9]{2})\b`, "TAG12"},
		{`\bTAG[0-9]{2}\b`, "xTAG12 TAG34a TAG56"},
		{`\bTAG[0-9]{2}\b`, "_TAG12\nTAG34\x00TAG56"},
		{`\BTAG[0-9]{2}\b`, "_TAG12 TAG34"},
		{`\bTAG.{0,10}Z\b`, "xTAGx TAGZ"},
		{`\bTAG(?:TAG)?`, "TAGTAG TAG"},
		{`\bTAG(?:a|ab)`, "TAGab TAGa"},
		{`\bTAG(?:ab|a)`, "TAGab TAGa"},
		{`\b(TAG)(x)?([0-9]{2})\b`, "TAG12 TAGx34"},
		{`\bTAG[0-9]{2}\b`, "TAG12a"},
	}
	for _, tc := range cases {
		re := regexp.MustCompile(tc.pattern)
		helper := newBoundedSearch(re)
		if helper == nil {
			t.Fatal("test pattern not eligible")
		}
		for _, text := range []string{tc.text + strings.Repeat(" ", 4096), strings.Repeat(" ", 4096) + tc.text} {
			got, used := helper.find(text)
			if !used {
				t.Fatal("optimized path not exercised")
			}
			if !reflect.DeepEqual(got, re.FindAllStringSubmatchIndex(text, -1)) {
				t.Fatal("match indices differ from reference")
			}
		}
	}
}

func TestBoundedSearchFallbackAndCandidateLimit(t *testing.T) {
	helper := newBoundedSearch(regexp.MustCompile(`\bTAG[0-9]{2}\b`))
	for _, text := range []string{"TAG12", strings.Repeat(" ", 4096) + "TAG12\u212a", strings.Repeat(" ", 4096) + string([]byte{255}), strings.Repeat(" ", 4096) + strings.Repeat("TAG12 ", 33)} {
		if _, used := helper.find(text); used {
			t.Fatal("required reference fallback not taken")
		}
	}
	text := strings.Repeat(" ", 4096) + strings.Repeat("TAG12 ", 32)
	got, used := helper.find(text)
	if !used || len(got) != 32 {
		t.Fatal("exact candidate limit failed")
	}
}

func TestBoundedDetectorPreservesValidationAndBlocking(t *testing.T) {
	d, err := newRegex("bounded", "ipv4", `\b10\.80\.[0-9]{1,3}\.[0-9]{1,3}\b`, "ipv4")
	if err != nil {
		t.Fatal("detector construction failed")
	}
	if d.bounded == nil {
		t.Fatal("custom detector did not opt in")
	}
	reference := *d
	reference.bounded = nil
	text := strings.Repeat(" ", 4096) + "10.80.1.2 10.80.999.3"
	got := Scan([]Detector{blocking{d}}, text)
	want := Scan([]Detector{blocking{&reference}}, text)
	if !reflect.DeepEqual(got, want) || len(got) != 1 || !got[0].Block {
		t.Fatal("validation or policy changed")
	}
}

// FuzzBoundedSearch compares complete capture offsets, including unmatched
// groups, against the unchanged regexp engine. Every ASCII case is long enough
// to enter the optimized path; arbitrary bytes also exercise safe fallback.
func FuzzBoundedSearch(f *testing.F) {
	for _, seed := range []string{"", " TAG12 ", "atagab TAG1", "TAG1_TAG22", "\x00TAG123\n", strings.Repeat(" TAG1", 33), "\xff TAG1", "K TAG1"} {
		f.Add(seed, uint8(0))
	}
	patterns := []string{
		`\bTAG([0-9]{1,3})(x)?\b`,
		`\Btag(?:a|b){1,3}`,
		`\bTAG(?:1|12)([a-z]{0,3})\b`,
		`\bTAG.{0,8}\b`,
		`\bTAG(?:TAG)?`,
		`\bTAG(?:\b|_)[a-z]{0,4}`,
	}
	refs := make([]*regexp.Regexp, len(patterns))
	helpers := make([]*boundedSearch, len(patterns))
	for i, pattern := range patterns {
		refs[i] = regexp.MustCompile(pattern)
		helpers[i] = newBoundedSearch(refs[i])
		if helpers[i] == nil {
			f.Fatal("eligible fuzz pattern rejected")
		}
	}
	f.Fuzz(func(t *testing.T, input string, choice uint8) {
		if len(input) > 16384 {
			t.Skip()
		}
		i := int(choice) % len(refs)
		// Test both true end-of-input context and a match embedded in a larger body.
		for _, s := range []string{strings.Repeat(" ", minBoundedInput) + input, input + strings.Repeat(" ", minBoundedInput)} {
			got, used := helpers[i].find(s)
			if !used {
				got = refs[i].FindAllStringSubmatchIndex(s, -1)
			}
			want := refs[i].FindAllStringSubmatchIndex(s, -1)
			if !reflect.DeepEqual(got, want) {
				t.Fatal("capture offsets differ from reference")
			}
		}
	})
}

func TestBoundedSearchConcurrent(t *testing.T) {
	re := regexp.MustCompile(`\bTAG([0-9]{1,3})\b`)
	helper := newBoundedSearch(re)
	input := strings.Repeat(" ", minBoundedInput) + " TAG12 "
	want := re.FindAllStringSubmatchIndex(input, -1)
	for range 16 {
		t.Run("shared", func(t *testing.T) {
			t.Parallel()
			for range 100 {
				got, used := helper.find(input)
				if !used || !reflect.DeepEqual(got, want) {
					t.Fatal("concurrent offsets differ")
				}
			}
		})
	}
}

func BenchmarkBoundedControls(b *testing.B) {
	for _, tc := range []struct{ name, pattern, input string }{
		{"sparse", `\bTAG[0-9]{1,3}\b`, strings.Repeat(" ", 262144) + " TAG12 "},
		{"dense", `\bTAG[0-9]{1,3}\b`, strings.Repeat(" TAG1", 8192)},
		{"short", `\bTAG[0-9]{1,3}\b`, " TAG1 "},
		{"unicode", `\bTAG[0-9]{1,3}\b`, strings.Repeat(" ", 262144) + "K TAG1"},
		{"unsupported", `TAG[0-9]+`, strings.Repeat(" ", 262144) + " TAG1"},
		{"maximum_width", `\bTAG.{0,253}`, strings.Repeat(" ", 262144) + strings.Repeat(" TAG", 32)},
	} {
		re := regexp.MustCompile(tc.pattern)
		helper := newBoundedSearch(re)
		b.Run(tc.name, func(b *testing.B) {
			for _, optimized := range []bool{false, true} {
				name := "reference"
				if optimized {
					name = "candidate"
				}
				b.Run(name, func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						if optimized {
							if _, used := helper.find(tc.input); used {
								continue
							}
						}
						re.FindAllStringSubmatchIndex(tc.input, -1)
					}
				})
			}
		})
	}
}
