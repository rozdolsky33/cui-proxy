package detecteval

import (
	"cmp"
	"slices"

	"cui-proxy/internal/config"
	"cui-proxy/internal/detect"
)

// Seed and ValuesPerVariant define the published corpus. Changing either
// changes every count in the baseline.
const (
	Seed             uint64 = 20261119
	ValuesPerVariant int    = 40
)

// Positive counts how one group of positive samples was handled.
//
// Full means every byte of the value fell inside a detected span, so none of
// it would be forwarded. Partial means some bytes did and some did not.
// Missed means no span touched the value. AsFamily counts the Full samples
// where a covering span carried the value's own family as its class.
type Positive struct {
	Family   string `json:"family"`
	Group    string `json:"group"`
	Claimed  bool   `json:"claimed"`
	Total    int    `json:"total"`
	Full     int    `json:"full"`
	Partial  int    `json:"partial"`
	Missed   int    `json:"missed"`
	AsFamily int    `json:"as_family"`
}

// Negative counts how one group of negative samples was handled. Flagged
// means a detected span touched the value.
type Negative struct {
	Family  string `json:"family"`
	Group   string `json:"group"`
	Total   int    `json:"total"`
	Flagged int    `json:"flagged"`
}

// Report is the result of one evaluation. It holds counts only, never values.
type Report struct {
	Seed             uint64     `json:"seed"`
	ValuesPerVariant int        `json:"values_per_variant"`
	Samples          int        `json:"samples"`
	Variants         []Positive `json:"positive_variants"`
	Contexts         []Positive `json:"positive_contexts"`
	Negatives        []Negative `json:"negative_variants"`
}

// Builtin returns the built-in detector bundle under the default profile,
// with no custom or dictionary rules.
func Builtin() ([]detect.Detector, error) {
	return detect.Build([]config.Rule{{Name: "builtin", Type: "builtin"}}, "")
}

// Evaluate scans every sample and tallies the outcomes.
func Evaluate(ds []detect.Detector, samples []Sample) Report {
	type key struct{ family, group string }
	variants, contexts := map[key]*Positive{}, map[key]*Positive{}
	negatives := map[key]*Negative{}

	for _, s := range samples {
		spans := detect.Scan(ds, s.Text)
		covered := make([]bool, s.End-s.Start)
		touched, asFamily := 0, false
		for _, sp := range spans {
			lo, hi := max(sp.Start, s.Start), min(sp.End, s.End)
			if lo >= hi {
				continue
			}
			if sp.Class == s.Family {
				asFamily = true
			}
			for i := lo; i < hi; i++ {
				if !covered[i-s.Start] {
					covered[i-s.Start] = true
					touched++
				}
			}
		}
		if !s.Positive {
			k := key{s.Family, s.Variant}
			if negatives[k] == nil {
				negatives[k] = &Negative{Family: s.Family, Group: s.Variant}
			}
			negatives[k].Total++
			if touched > 0 {
				negatives[k].Flagged++
			}
			continue
		}
		for _, t := range []struct {
			m map[key]*Positive
			k key
		}{{variants, key{s.Family, s.Variant}}, {contexts, key{s.Family, s.Context}}} {
			p := t.m[t.k]
			if p == nil {
				p = &Positive{Family: t.k.family, Group: t.k.group, Claimed: true}
				t.m[t.k] = p
			}
			// A context group is claimed only if every variant in it is.
			p.Claimed = p.Claimed && s.Claimed
			p.Total++
			switch {
			case touched == len(covered):
				p.Full++
				if asFamily {
					p.AsFamily++
				}
			case touched > 0:
				p.Partial++
			default:
				p.Missed++
			}
		}
	}

	r := Report{Seed: Seed, ValuesPerVariant: ValuesPerVariant, Samples: len(samples)}
	for _, p := range variants {
		r.Variants = append(r.Variants, *p)
	}
	for _, p := range contexts {
		r.Contexts = append(r.Contexts, *p)
	}
	for _, n := range negatives {
		r.Negatives = append(r.Negatives, *n)
	}
	byName := func(af, ag, bf, bg string) int {
		return cmp.Or(cmp.Compare(af, bf), cmp.Compare(ag, bg))
	}
	slices.SortFunc(r.Variants, func(a, b Positive) int { return byName(a.Family, a.Group, b.Family, b.Group) })
	slices.SortFunc(r.Contexts, func(a, b Positive) int { return byName(a.Family, a.Group, b.Family, b.Group) })
	slices.SortFunc(r.Negatives, func(a, b Negative) int { return byName(a.Family, a.Group, b.Family, b.Group) })
	return r
}

// Run evaluates the built-in bundle against the published corpus.
func Run() (Report, error) {
	ds, err := Builtin()
	if err != nil {
		return Report{}, err
	}
	return Evaluate(ds, Corpus(Seed, ValuesPerVariant)), nil
}
