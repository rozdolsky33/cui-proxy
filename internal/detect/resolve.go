package detect

import "sort"

// specificity breaks ties between equally long, equally early spans.
func specificity(class string) int {
	switch class {
	case "ocid":
		return 4
	case "cidr":
		return 3
	case "ipv4":
		return 0
	default:
		return 1
	}
}

// resolve keeps the longest spans first (ties: earlier Start, then more
// specific class, then input order), dropping any span that overlaps a kept
// one, and returns the survivors sorted by Start.
func resolve(spans []Span) []Span {
	cand := make([]Span, 0, len(spans))
	for _, sp := range spans {
		if sp.End > sp.Start {
			cand = append(cand, sp)
		}
	}
	sort.SliceStable(cand, func(i, j int) bool {
		a, b := cand[i], cand[j]
		if la, lb := a.End-a.Start, b.End-b.Start; la != lb {
			return la > lb
		}
		if a.Start != b.Start {
			return a.Start < b.Start
		}
		return specificity(a.Class) > specificity(b.Class)
	})
	var kept []Span
	for _, sp := range cand {
		overlap := false
		for _, k := range kept {
			if sp.Start < k.End && k.Start < sp.End {
				overlap = true
				break
			}
		}
		if !overlap {
			kept = append(kept, sp)
		}
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].Start < kept[j].Start })
	return kept
}
