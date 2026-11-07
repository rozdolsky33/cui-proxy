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

// better reports whether a outranks b: longer, then earlier Start, then more
// specific class.
func better(a, b Span) bool {
	if la, lb := a.End-a.Start, b.End-b.Start; la != lb {
		return la > lb
	}
	if a.Start != b.Start {
		return a.Start < b.Start
	}
	return specificity(a.Class) > specificity(b.Class)
}

// resolve groups transitively overlapping spans and replaces each group with
// its union, so no part of any detected value is left unmasked. The group's
// Rule and Class come from its best member (longer, then earlier Start, then
// more specific class, then input order). Spans that merely touch stay
// separate. Output is sorted by Start. O(n log n).
func resolve(spans []Span) []Span {
	cand := make([]Span, 0, len(spans))
	for _, sp := range spans {
		if sp.End > sp.Start {
			cand = append(cand, sp)
		}
	}
	sort.SliceStable(cand, func(i, j int) bool {
		if cand[i].Start != cand[j].Start {
			return cand[i].Start < cand[j].Start
		}
		return cand[i].End > cand[j].End
	})
	var out []Span
	var best Span
	for i, sp := range cand {
		if i > 0 && sp.Start < out[len(out)-1].End {
			u := &out[len(out)-1]
			if sp.End > u.End {
				u.End = sp.End
			}
			if better(sp, best) {
				best = sp
			}
		} else {
			out = append(out, sp)
			best = sp
		}
		u := &out[len(out)-1]
		u.Rule, u.Class = best.Rule, best.Class
	}
	return out
}
