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

	// Block marks a span in which a detector whose effective policy action is
	// block matched. When overlapping spans are merged, Block is true if ANY
	// member blocks, so a blocking rule can never lose a vote to a longer
	// match from a rule that only sanitizes. BlockRule and BlockClass name the
	// best blocking member (config names and a closed set, never the value).
	Block                 bool
	BlockRule, BlockClass string
}

// Detector finds candidate spans in a string.
type Detector interface {
	Name() string
	Class() string
	Find(s string) []Span
}

// blocking wraps a detector whose effective action is block: every span it
// returns is tagged so the sanitizer refuses the request instead of masking.
type blocking struct{ Detector }

func (b blocking) Find(s string) []Span {
	sp := b.Detector.Find(s)
	for i := range sp {
		sp[i].Block, sp[i].BlockRule, sp[i].BlockClass = true, sp[i].Rule, sp[i].Class
	}
	return sp
}

// IsBlocking reports whether d was built with an effective action of block.
func IsBlocking(d Detector) bool {
	_, ok := d.(blocking)
	return ok
}

// Build turns config rules into detectors under the default ("sanitize")
// profile: see BuildForProfile.
func Build(rules []config.Rule, baseDir string) ([]Detector, error) {
	return BuildForProfile(rules, baseDir, config.ProfileSanitize)
}

// BuildForProfile turns config rules into detectors. A "builtin" rule expands
// to the v1 builtin set (its detectors are named "<rule>.<class>"). Relative
// dictionary paths are resolved against baseDir. A rule whose effective action
// under profile is block (config.EffectiveAction) yields spans tagged Block.
func BuildForProfile(rules []config.Rule, baseDir, profile string) ([]Detector, error) {
	var out []Detector
	for _, r := range rules {
		first := len(out)
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
			if d.re == nil {
				return nil, fmt.Errorf("detect: rule %q: dictionary %s has no entries", r.Name, path)
			}
			out = append(out, d)
		default:
			return nil, fmt.Errorf("detect: rule %q: unknown type %q", r.Name, r.Type)
		}
		if config.EffectiveAction(profile, r) == config.ActionBlock {
			for i := first; i < len(out); i++ {
				out[i] = blocking{out[i]}
			}
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
