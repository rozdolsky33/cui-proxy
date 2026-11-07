// Package detect finds sensitive values in text using regex, dictionary and
// builtin detectors, and resolves overlapping matches.
package detect

import (
	"fmt"
	"os"
	"path/filepath"

	"cui-proxy/internal/config"
)

// Span is a detected value: byte offsets [Start, End) into the scanned string.
type Span struct {
	Start, End int
	Rule       string // name of the detector that produced it
	Class      string // synthetic-value class (ipv4, ipv6, cidr, hostname, email, ocid, ssn, card, secret, ...)
}

// Detector finds candidate spans in a string.
type Detector interface {
	Name() string
	Class() string
	Find(s string) []Span
}

// Build turns config rules into detectors. A "builtin" rule expands to the v1
// builtin set (its detectors are named "<rule>.<class>"). Relative dictionary
// paths are resolved against baseDir.
func Build(rules []config.Rule, baseDir string) ([]Detector, error) {
	var out []Detector
	for _, r := range rules {
		switch r.Type {
		case "builtin":
			out = append(out, builtinSet(r.Name+".")...)
		case "regex":
			d, err := newRegex(r.Name, r.Synth, r.Pattern, r.Validate)
			if err != nil {
				return nil, err
			}
			out = append(out, d)
		case "dictionary":
			path := r.File
			if !filepath.IsAbs(path) {
				path = filepath.Join(baseDir, path)
			}
			f, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("detect: rule %q: %w", r.Name, err)
			}
			d, err := newDict(r.Name, r.Synth, string(f))
			if err != nil {
				return nil, err
			}
			out = append(out, d)
		default:
			return nil, fmt.Errorf("detect: rule %q: unknown type %q", r.Name, r.Type)
		}
	}
	return out, nil
}

// Scan runs every detector over s and returns non-overlapping spans sorted by
// Start (longest span wins on overlap).
func Scan(ds []Detector, s string) []Span {
	var all []Span
	for _, d := range ds {
		all = append(all, d.Find(s)...)
	}
	return resolve(all)
}
