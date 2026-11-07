package detect

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type dictDetector struct {
	name, class string
	re          *regexp.Regexp // nil when the dictionary is empty
}

// newDict parses dictionary text: one entry per line, '#' comment lines and
// blank lines ignored. Matching is case-insensitive, longest entry first.
func newDict(name, class, text string) (*dictDetector, error) {
	seen := map[string]bool{}
	var entries []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k := strings.ToLower(line); !seen[k] {
			seen[k] = true
			entries = append(entries, line)
		}
	}
	d := &dictDetector{name: name, class: class}
	if len(entries) == 0 {
		return d, nil
	}
	sort.SliceStable(entries, func(i, j int) bool { return len(entries[i]) > len(entries[j]) })
	parts := make([]string, len(entries))
	for i, e := range entries {
		parts[i] = regexp.QuoteMeta(e)
	}
	re, err := regexp.Compile("(?i)" + strings.Join(parts, "|"))
	if err != nil {
		return nil, fmt.Errorf("detect: rule %q: dictionary: %w", name, err)
	}
	d.re = re
	return d, nil
}

func (d *dictDetector) Name() string  { return d.name }
func (d *dictDetector) Class() string { return d.class }

// Find reports whole-entry matches: an entry that begins or ends with a
// letter/digit must not be glued to an adjacent letter/digit.
func (d *dictDetector) Find(s string) []Span {
	if d.re == nil {
		return nil
	}
	var out []Span
	for _, m := range d.re.FindAllStringIndex(s, -1) {
		st, en := m[0], m[1]
		if isAlnum(s[st]) && st > 0 && isAlnum(s[st-1]) {
			continue
		}
		if isAlnum(s[en-1]) && en < len(s) && isAlnum(s[en]) {
			continue
		}
		out = append(out, Span{Start: st, End: en, Rule: d.name, Class: d.class})
	}
	return out
}
