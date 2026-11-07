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
	entries     []string       // longest first
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
	d := &dictDetector{name: name, class: class, entries: entries}
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

// wholeAt reports whether s[st:en] is a whole-entry match: an edge that is a
// letter/digit must not be glued to an adjacent letter/digit.
func wholeAt(s string, st, en int) bool {
	if isAlnum(s[st]) && st > 0 && isAlnum(s[st-1]) {
		return false
	}
	return !(isAlnum(s[en-1]) && en < len(s) && isAlnum(s[en]))
}

// Find reports whole-entry matches. When the longest entry at a position is
// glued to an alphanumeric, shorter entries starting at the same position are
// tried, and scanning resumes one byte later, so a rejected long entry never
// hides a valid shorter one.
func (d *dictDetector) Find(s string) []Span {
	if d.re == nil {
		return nil
	}
	var out []Span
	for pos := 0; pos < len(s); {
		m := d.re.FindStringIndex(s[pos:])
		if m == nil {
			break
		}
		st, en := pos+m[0], pos+m[1]
		if !wholeAt(s, st, en) {
			en = 0
			for _, e := range d.entries {
				if st+len(e) <= len(s) && strings.EqualFold(s[st:st+len(e)], e) && wholeAt(s, st, st+len(e)) {
					en = st + len(e)
					break
				}
			}
			if en == 0 {
				pos = st + 1
				continue
			}
		}
		out = append(out, Span{Start: st, End: en, Rule: d.name, Class: d.class})
		pos = en
	}
	return out
}
