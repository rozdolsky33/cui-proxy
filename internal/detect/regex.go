package detect

import (
	"fmt"
	"regexp"
)

type regexDetector struct {
	name, class string
	re          *regexp.Regexp
	refine      refiner // nil: accept every match
	find        finder  // if set, replaces refine: may return several spans per candidate
	group       bool    // report capture group 1 instead of the whole match
}

// A finder turns a regex candidate text[s:e] into zero or more spans.
type finder func(text string, s, e int) [][2]int

func newRegex(name, class, pattern, validate string) (*regexDetector, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("detect: rule %q: bad pattern: %w", name, err)
	}
	d := &regexDetector{name: name, class: class, re: re}
	if validate != "" {
		r, ok := refiners[validate]
		if !ok {
			return nil, fmt.Errorf("detect: rule %q: unknown validator %q", name, validate)
		}
		d.refine = r
	}
	return d, nil
}

func (d *regexDetector) Name() string  { return d.name }
func (d *regexDetector) Class() string { return d.class }

func (d *regexDetector) Find(s string) []Span {
	var out []Span
	for _, m := range d.re.FindAllStringSubmatchIndex(s, -1) {
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
