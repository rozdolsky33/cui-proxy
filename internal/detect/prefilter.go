package detect

import (
	"regexp"
	"regexp/syntax"
	"strings"
	"unicode"
)

// requiredLiteral derives one necessary ASCII substring, never a sufficient
// condition for a match. Empty means no precheck. Unicode and case folding are
// deliberately conservative: invalid UTF-8 may match RuneError without its UTF-8
// spelling, and Unicode folding can equate ASCII letters with non-ASCII runes.
// The retained literal is bounded and belongs to the compiled detector, not a
// request cache; rebuilding rules automatically rebuilds this metadata.
// detectorLiteral avoids adding a full-input search where regexp already has
// a literal-prefix search. Anchored expressions are excluded below as well.
func detectorLiteral(re *regexp.Regexp) string {
	if prefix, _ := re.LiteralPrefix(); prefix != "" {
		return ""
	}
	return requiredLiteral(re.String())
}

func requiredLiteral(pattern string) string {
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return ""
	}
	program, err := syntax.Compile(re.Simplify())
	if err != nil || program.StartCond()&syntax.EmptyBeginText != 0 {
		return ""
	}
	return mandatoryLiteral(re)
}

func mandatoryLiteral(re *syntax.Regexp) string {
	switch re.Op {
	case syntax.OpLiteral:
		best, run := "", ""
		for _, r := range re.Rune {
			if r >= 0 && r < 128 && (re.Flags&syntax.FoldCase == 0 || unicode.SimpleFold(r) == r) {
				if len(run) < 64 {
					run += string(r)
				}
				if len(run) > len(best) {
					best = run
				}
			} else {
				run = ""
			}
		}
		return best
	case syntax.OpCapture, syntax.OpPlus:
		return mandatoryLiteral(re.Sub[0])
	case syntax.OpRepeat:
		if re.Min > 0 {
			return mandatoryLiteral(re.Sub[0])
		}
	case syntax.OpConcat:
		best := ""
		for _, sub := range re.Sub {
			if candidate := mandatoryLiteral(sub); len(candidate) > len(best) {
				best = candidate
			}
		}
		return best
	case syntax.OpAlternate:
		if len(re.Sub) == 0 {
			return ""
		}
		candidates := make([]string, len(re.Sub))
		for i, sub := range re.Sub {
			candidates[i] = mandatoryLiteral(sub)
			if candidates[i] == "" {
				return ""
			}
		}
		first := candidates[0]
		common := func(value string) bool {
			for _, c := range candidates[1:] {
				if !strings.Contains(c, value) {
					return false
				}
			}
			return true
		}
		if common(first) {
			return first
		}
		// A single common ASCII byte is also necessary in every branch. Avoid an
		// unbounded longest-common-substring computation for large configured rules.
		for i := range len(first) {
			if common(first[i : i+1]) {
				return first[i : i+1]
			}
		}
	}
	return ""
}
