package detect

import (
	"regexp"
	"regexp/syntax"
	"strings"
	"unicode"
)

// foldedDetectorLiteral is a necessary substring only for ASCII input after
// lowercasing. Unicode input must never use this precheck. Fold cycles may
// contain non-ASCII spellings (Kelvin sign, long s); their ASCII equivalents
// are retained here without changing the actual regexp matcher.
func foldedDetectorLiteral(re *regexp.Regexp) string {
	if prefix, _ := re.LiteralPrefix(); prefix != "" {
		return ""
	}
	tree, err := syntax.Parse(re.String(), syntax.Perl)
	if err != nil {
		return ""
	}
	program, err := syntax.Compile(tree.Simplify())
	if err != nil || program.StartCond()&syntax.EmptyBeginText != 0 {
		return ""
	}
	var normalize func(*syntax.Regexp)
	normalize = func(node *syntax.Regexp) {
		if node.Op == syntax.OpLiteral {
			for i, original := range node.Rune {
				candidate := original
				if node.Flags&syntax.FoldCase != 0 && candidate >= 128 {
					for folded := unicode.SimpleFold(original); folded != original; folded = unicode.SimpleFold(folded) {
						if folded < 128 {
							candidate = folded
							break
						}
					}
				}
				if candidate >= 'A' && candidate <= 'Z' {
					candidate += 'a' - 'A'
				}
				node.Rune[i] = candidate
			}
			node.Flags &^= syntax.FoldCase
		}
		for _, sub := range node.Sub {
			normalize(sub)
		}
	}
	normalize(tree)
	return mandatoryLiteral(tree)
}

func foldedLiteral(d Detector) string {
	for {
		switch value := d.(type) {
		case blocking:
			d = value.Detector
		case *blocking:
			if value == nil {
				return ""
			}
			d = value.Detector
		case *dictDetector:
			if value == nil {
				return ""
			}
			return value.folded
		case *regexDetector:
			if value == nil || len(value.folded) <= len(value.required) {
				return ""
			}
			return value.folded
		default:
			return ""
		}
	}
}

// prepareFoldedText neither caches nor retains request text. Small fields and
// non-ASCII fields use the original matcher, including invalid UTF-8 input.
func prepareFoldedText(ds []Detector, text string) (string, bool) {
	if len(text) < 4096 {
		return "", false
	}
	needed := false
	for _, d := range ds {
		if foldedLiteral(d) != "" {
			needed = true
			break
		}
	}
	if !needed {
		return "", false
	}
	for i := 0; i < len(text); i++ {
		if text[i] >= 128 {
			return "", false
		}
	}
	return strings.ToLower(text), true
}

func skipFolded(d Detector, text string, prepared bool) bool {
	if !prepared {
		return false
	}
	required := foldedLiteral(d)
	return required != "" && !strings.Contains(text, required)
}
