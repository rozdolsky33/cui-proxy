package detect

import (
	"cui-proxy/internal/config"
	"net"
	"reflect"
	"strings"
	"testing"
)

// referenceFind is the pre-optimization matcher, retained only as a test oracle.
func referenceFind(d *regexDetector, s string) []Span {
	var out []Span
	var ms [][]int
	if d.group {
		ms = d.groupMatches(s)
	} else {
		ms = d.re.FindAllStringSubmatchIndex(s, -1)
	}
	for _, m := range ms {
		st, en := m[0], m[1]
		if d.group && len(m) >= 4 && m[2] >= 0 {
			st, en = m[2], m[3]
		}
		if d.find != nil {
			for _, sp := range d.find(s, st, en) {
				out = append(out, Span{Start: sp[0], End: sp[1], Rule: d.name, Class: d.class})
			}
			continue
		}
		if d.refine != nil {
			var ok bool
			if st, en, ok = d.refine(s, st, en); !ok {
				continue
			}
		}
		out = append(out, Span{Start: st, End: en, Rule: d.name, Class: d.class})
	}
	return out
}

func referenceScan(ds []Detector, s string) []Span {
	var all []Span
	for _, detector := range ds {
		var spans []Span
		switch d := detector.(type) {
		case *regexDetector:
			spans = referenceFind(d, s)
		case blocking:
			if inner, ok := d.Detector.(*regexDetector); ok {
				spans = referenceFind(inner, s)
				for i := range spans {
					spans[i].Block = true
					spans[i].BlockRule = spans[i].Rule
					spans[i].BlockClass = spans[i].Class
				}
			} else {
				spans = d.Find(s)
			}
		default:
			spans = detector.Find(s)
		}
		all = append(all, spans...)
	}
	return resolve(all)
}

func indexDetectors(t testing.TB, profile string) []Detector {
	t.Helper()
	ds, err := BuildForProfile([]config.Rule{{Name: "builtin", Type: "builtin"}}, "", profile)
	if err != nil {
		t.Fatal("detector setup failed")
	}
	for _, pattern := range []string{`(ab)(cd)`, `(a*)(b?)`, `(?m)^(x+)(y*)$`, `(?i)(k)(s)`, `(a|ab)(b|)`, `(?:a)(b)(?:c)`, `(z)?`, `(a){0}`} {
		d, err := newRegex("custom", "secret", pattern, "")
		if err != nil {
			t.Fatal("regex setup failed")
		}
		if profile == "strict" {
			ds = append(ds, blocking{d})
		} else {
			ds = append(ds, d)
		}
	}
	return ds
}

func indexSamples() []string {
	return []string{"", "ordinary prose", "abcd ab abc aaaa b", "xxx\nyy\nxy", "Kſ Ks", string([]byte{0xff, 0xfe, 'a', 'b'}),
		net.IPv4(192, 0, 2, 42).String(), "user" + "@" + "example.invalid", "4" + strings.Repeat("1", 15),
		"Bearer " + "token_alpha_123=Bearer " + "token_beta_456", "prefix " + strings.Repeat("(ab) ", 50),
		strings.Repeat("a", 4096) + "b", "a\x00b\x00c"}
}
func TestRegexIndicesDifferential(t *testing.T) {
	for _, profile := range []string{"sanitize", "strict"} {
		ds := indexDetectors(t, profile)
		for _, input := range indexSamples() {
			if !reflect.DeepEqual(Scan(ds, input), referenceScan(ds, input)) {
				t.Fatal("candidate differs from reference spans or blocking tags")
			}
		}
	}
}
func FuzzRegexIndicesDifferential(f *testing.F) {
	for _, input := range indexSamples() {
		f.Add(input)
	}
	ds := indexDetectors(f, "strict")
	f.Fuzz(func(t *testing.T, input string) {
		if !reflect.DeepEqual(Scan(ds, input), referenceScan(ds, input)) {
			t.Fatal("candidate differs from reference")
		}
	})
}

var indexSink []Span

func BenchmarkScanIndices(b *testing.B) {
	ds := builtinSet("builtin.")
	seed := "Routine workload text with words, delimiters and line endings.\n"
	mixed := seed + strings.Join(indexSamples()[6:10], " ") + "\n"
	for _, c := range []struct {
		name string
		size int
		seed string
	}{
		{"clean64KiB", 64 << 10, seed}, {"mixed64KiB", 64 << 10, mixed},
		{"dense64KiB", 64 << 10, mixed + net.IP([]byte{32, 1, 13, 184, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}).String() + " " + net.IPv4(192, 0, 2, 42).String() + "/24 " + strings.Join([]string{"123", "45", "6789"}, "-") + " ocid1." + "instance.oc1.." + strings.Repeat("a", 32) + "\n"}, {"mixed2MiB", 2 << 20, mixed}, {"mixed8MiB", 8 << 20, mixed},
	} {
		input := strings.Repeat(c.seed, c.size/len(c.seed)+1)[:c.size]
		for _, reference := range []bool{true, false} {
			name := "candidate"
			if reference {
				name = "reference"
			}
			b.Run(c.name+"/"+name, func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(len(input)))
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if reference {
						indexSink = referenceScan(ds, input)
					} else {
						indexSink = Scan(ds, input)
					}
				}
			})
		}
	}
}

func TestRequiredLiteralConservative(t *testing.T) {
	for _, pattern := range []string{`a*`, `(?:foo)?`, `(?i)abc`, `a|b`, `[a-z]+`, `(?:x){0}`} {
		if requiredLiteral(pattern) != "" {
			t.Fatal("optional or alternative text became mandatory")
		}
	}
	for _, pattern := range []string{`\bmarker:`, `(foo)(bar)`, `a+`, `(?:aa:|bb::)`, `x{2,4}`, `αEND`, `(?i)ABC:123`} {
		literal := requiredLiteral(pattern)
		if literal == "" {
			t.Fatal("mandatory literal missing")
		}
		d, err := newRegex("literal", "secret", pattern, "")
		if err != nil {
			t.Fatal("setup failed")
		}
		for _, input := range []string{"marker:", "foobar", "aaaa", "aa:", "bb::", "xxxx", "αEND", "abc:123"} {
			if d.re.MatchString(input) && !strings.Contains(input, literal) {
				t.Fatal("literal is not required")
			}
		}
	}
}

func FuzzRequiredLiteralSoundness(f *testing.F) {
	for _, seed := range [][2]string{{`\bmarker:`, `marker:`}, {`a*`, ``}, {`(?i)k`, "K"}, {`αEND`, "αEND"}, {`(?:aa:|bb::)`, "bb::"}, {`(x){0,3}END`, "END"}, {`[^x]END`, string([]byte{0xff}) + "END"}, {`(?s).*END`, "line\nEND"}} {
		f.Add(seed[0], seed[1])
	}
	f.Fuzz(func(t *testing.T, pattern, input string) {
		if len(pattern) > 512 || len(input) > 65536 {
			return
		}
		d, err := newRegex("fuzz", "secret", pattern, "")
		if err != nil {
			return
		}
		if !reflect.DeepEqual(d.Find(input), referenceFind(d, input)) {
			t.Fatal("prefilter differs from reference")
		}
	})
}

func TestRequiredLiteralBoundAndUnicodeFallback(t *testing.T) {
	pattern := strings.Repeat("abcdef", 1000)
	literal := requiredLiteral(pattern)
	if len(literal) == 0 || len(literal) > 64 || !strings.Contains(pattern, literal) {
		t.Fatal("literal metadata is unbounded or not necessary")
	}
	for _, pattern := range []string{`(?:abc|)`, `(?i)ſ`, `(?i)K`, `\x{FFFD}`, `(?:\bEND\b|[^[:ascii:]])`, `^$`} {
		if requiredLiteral(pattern) != "" {
			t.Fatal("unsafe Unicode or optional prerequisite")
		}
	}
}

func TestPrefilterRespectsExistingSearchStrategy(t *testing.T) {
	for _, pattern := range []string{`needlemarker`, `^needlemarker`, `\A(?:a|b)END`} {
		d, err := newRegex("strategy", "secret", pattern, "")
		if err != nil {
			t.Fatal("setup failed")
		}
		if d.required != "" {
			t.Fatal("prefilter duplicates an existing prefix or anchored search strategy")
		}
	}
}
func BenchmarkExistingRegexStrategy(b *testing.B) {
	for _, c := range []struct{ name, pattern, input string }{
		{"prefix_hit", `needlemarker`, strings.Repeat("x", 2<<20) + "needlemarker"},
		{"anchored_miss", `^needlemarker`, strings.Repeat("x", 2<<20)},
	} {
		d, err := newRegex("strategy", "secret", c.pattern, "")
		if err != nil {
			b.Fatal("setup failed")
		}
		for _, reference := range []bool{true, false} {
			arm := "candidate"
			if reference {
				arm = "reference"
			}
			b.Run(c.name+"/"+arm, func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if reference {
						indexSink = referenceFind(d, c.input)
					} else {
						indexSink = d.Find(c.input)
					}
				}
			})
		}
	}
}
