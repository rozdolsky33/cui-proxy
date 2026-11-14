package detect

import (
	"regexp"
	"regexp/syntax"
)

// boundedDigitWidth proves a finite ASCII match with a digit-only first byte.
// Zero means use the original regexp. This metadata contains no request data.
func boundedDigitWidth(re *regexp.Regexp) int {
	if prefix, _ := re.LiteralPrefix(); prefix != "" {
		return 0
	}
	tree, err := syntax.Parse(re.String(), syntax.Perl)
	if err != nil {
		return 0
	}
	shape, ok := digitShape(tree)
	if !ok || shape.nullable || !shape.digitFirst || shape.width == 0 {
		return 0
	}
	return shape.width
}

type boundedShape struct {
	width      int
	nullable   bool
	digitFirst bool
}

func digitShape(re *syntax.Regexp) (boundedShape, bool) {
	empty := boundedShape{nullable: true, digitFirst: true}
	switch re.Op {
	case syntax.OpEmptyMatch, syntax.OpWordBoundary, syntax.OpNoWordBoundary:
		return empty, true
	case syntax.OpLiteral:
		if re.Flags&syntax.FoldCase != 0 {
			return boundedShape{}, false
		}
		for _, r := range re.Rune {
			if r < 0 || r > 127 {
				return boundedShape{}, false
			}
		}
		if len(re.Rune) == 0 {
			return empty, true
		}
		return boundedShape{width: len(re.Rune), digitFirst: re.Rune[0] >= '0' && re.Rune[0] <= '9'}, len(re.Rune) <= 256
	case syntax.OpCharClass:
		if len(re.Rune) == 0 {
			return boundedShape{}, false
		}
		digits := true
		for i := 0; i < len(re.Rune); i += 2 {
			if re.Rune[i] < 0 || re.Rune[i+1] > 127 {
				return boundedShape{}, false
			}
			digits = digits && re.Rune[i] >= '0' && re.Rune[i+1] <= '9'
		}
		return boundedShape{width: 1, digitFirst: digits}, true
	case syntax.OpCapture:
		return digitShape(re.Sub[0])
	case syntax.OpQuest, syntax.OpRepeat:
		sub, ok := digitShape(re.Sub[0])
		if !ok {
			return boundedShape{}, false
		}
		low, high := 0, 1
		if re.Op == syntax.OpRepeat {
			low, high = re.Min, re.Max
		}
		if high < 0 || (sub.width > 0 && high > 256/sub.width) {
			return boundedShape{}, false
		}
		sub.width *= high
		sub.nullable = low == 0 || sub.nullable
		if high == 0 {
			return empty, true
		}
		return sub, true
	case syntax.OpConcat:
		result := empty
		for _, part := range re.Sub {
			sub, ok := digitShape(part)
			if !ok || result.width+sub.width > 256 {
				return boundedShape{}, false
			}
			result.width += sub.width
			if result.nullable {
				result.digitFirst = result.digitFirst && sub.digitFirst
			}
			result.nullable = result.nullable && sub.nullable
		}
		return result, true
	case syntax.OpAlternate:
		result := boundedShape{digitFirst: true}
		for _, part := range re.Sub {
			sub, ok := digitShape(part)
			if !ok {
				return boundedShape{}, false
			}
			result.width = max(result.width, sub.width)
			result.nullable = result.nullable || sub.nullable
			result.digitFirst = result.digitFirst && sub.digitFirst
		}
		return result, true
	default:
		// Anchors, unbounded repetition, wildcards and unsupported syntax stay on
		// the original engine path. Unicode and folded literals also fall back.
		return boundedShape{}, false
	}
}

// boundedMatches keeps the original regexp and its leftmost, nonoverlapping
// match semantics. Every eligible match begins at a digit. Windows include
// that candidate's maximum match width and one byte of context on either side.
// Overlapping windows merge before matching; all nonnegative capture offsets
// are translated back to the original input. Unsupported inputs fall back.
func boundedMatches(re *regexp.Regexp, s string, width int) [][]int {
	if width <= 0 || len(s) < 4096 {
		return re.FindAllStringSubmatchIndex(s, -1)
	}
	var result [][]int
	lo, hi, covered := -1, -1, 0
	flush := func() {
		if lo < 0 {
			return
		}
		for _, m := range re.FindAllStringSubmatchIndex(s[lo:hi], -1) {
			for k := range m {
				if m[k] >= 0 {
					m[k] += lo
				}
			}
			result = append(result, m)
		}
		covered += hi - lo
	}
	for i := 0; i < len(s); i++ {
		if s[i] >= 128 {
			return re.FindAllStringSubmatchIndex(s, -1)
		}
		if s[i] < '0' || s[i] > '9' {
			continue
		}
		left, right := max(0, i-1), min(len(s), i+width+1)
		if lo < 0 {
			lo, hi = left, right
		} else if left <= hi {
			hi = max(hi, right)
		} else {
			flush()
			lo, hi = left, right
		}
		// Dense candidates cannot benefit enough to justify additional searches.
		// Discard partial results and retain the original engine's full result.
		if covered+hi-lo > len(s)/8 {
			return re.FindAllStringSubmatchIndex(s, -1)
		}
	}
	flush()
	return result
}
