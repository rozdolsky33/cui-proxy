package detect

import (
	"regexp"
	"regexp/syntax"
	"strings"
)

const (
	maxBoundedPattern      = 4096
	maxBoundedNodes        = 128
	maxBoundedInstructions = 4096
	maxBoundedWidth        = 256
	maxBoundedCandidates   = 32
	minBoundedInput        = 4096
)

// boundedSearch only changes candidate discovery. Both validators retain the
// original expression, including its captures and word-boundary assertions.
// Inputs outside the proven ASCII/context/width bounds use the reference path.
type boundedSearch struct {
	prefix               string
	width                int
	atStart, withContext *regexp.Regexp
}

func newBoundedSearch(re *regexp.Regexp) *boundedSearch {
	if len(re.String()) > maxBoundedPattern {
		return nil
	}
	tree, err := syntax.Parse(re.String(), syntax.Perl)
	if err != nil || tree.Op != syntax.OpConcat {
		return nil
	}
	first := 0
	for first < len(tree.Sub) && (tree.Sub[first].Op == syntax.OpWordBoundary || tree.Sub[first].Op == syntax.OpNoWordBoundary) {
		first++
	}
	if first == 0 || first == len(tree.Sub) {
		return nil
	}
	nodes := 0
	width, ok := boundedWidth(tree, &nodes)
	if !ok || width == 0 {
		return nil
	}
	program, err := syntax.Compile(tree.Simplify())
	if err != nil || len(program.Inst)+4 > maxBoundedInstructions {
		return nil
	}
	tail := &syntax.Regexp{Op: syntax.OpConcat, Sub: tree.Sub[first:]}
	prefixRE, err := regexp.Compile(tail.String())
	if err != nil {
		return nil
	}
	prefix, _ := prefixRE.LiteralPrefix()
	if prefix == "" || !asciiOnly(prefix) {
		return nil
	}
	start, err := regexp.Compile(`\A(?:` + re.String() + `)`)
	if err != nil {
		return nil
	}
	context, err := regexp.Compile(`\A(?s:.)(?:` + re.String() + `)`)
	if err != nil {
		return nil
	}
	return &boundedSearch{prefix: prefix, width: width, atStart: start, withContext: context}
}

// boundedWidth measures maximum consumed bytes for ASCII input. Reject every
// text/line anchor: a temporary window must never invent those boundaries.
func boundedWidth(re *syntax.Regexp, nodes *int) (int, bool) {
	*nodes++
	if *nodes > maxBoundedNodes {
		return 0, false
	}
	switch re.Op {
	case syntax.OpEmptyMatch, syntax.OpWordBoundary, syntax.OpNoWordBoundary:
		return 0, true
	case syntax.OpLiteral:
		return len(re.Rune), len(re.Rune) <= maxBoundedWidth
	case syntax.OpCharClass, syntax.OpAnyCharNotNL, syntax.OpAnyChar:
		return 1, true
	case syntax.OpCapture, syntax.OpQuest:
		return boundedWidth(re.Sub[0], nodes)
	case syntax.OpConcat, syntax.OpAlternate:
		width := 0
		for _, child := range re.Sub {
			n, ok := boundedWidth(child, nodes)
			if !ok {
				return 0, false
			}
			if re.Op == syntax.OpConcat {
				width += n
			} else {
				width = max(width, n)
			}
			if width > maxBoundedWidth {
				return 0, false
			}
		}
		return width, true
	case syntax.OpRepeat:
		if re.Max < 0 {
			return 0, false
		}
		n, ok := boundedWidth(re.Sub[0], nodes)
		if !ok || n > 0 && re.Max > maxBoundedWidth/n {
			return 0, false
		}
		return n * re.Max, true
	default:
		return 0, false
	}
}

func asciiOnly(s string) bool {
	for i := range len(s) {
		if s[i] >= 128 {
			return false
		}
	}
	return true
}

func (b *boundedSearch) find(s string) ([][]int, bool) {
	if b == nil || len(s) < minBoundedInput || !asciiOnly(s) {
		return nil, false
	}
	// Discover all starts first. Dense inputs fall back before paying for any
	// anchored validations, and overlapping prefixes count toward the same cap.
	var positions [maxBoundedCandidates]int
	count := 0
	for pos := 0; pos <= len(s)-len(b.prefix); {
		relative := strings.Index(s[pos:], b.prefix)
		if relative < 0 {
			break
		}
		if count == len(positions) {
			return nil, false
		}
		at := pos + relative
		positions[count] = at
		count++
		pos = at + 1
	}
	var matches [][]int
	next := 0
	for _, at := range positions[:count] {
		if at < next {
			continue
		}
		start := at
		validator := b.atStart
		if start > 0 {
			start--
			validator = b.withContext
		}
		end := at + min(len(s)-at, b.width+1)
		match := validator.FindStringSubmatchIndex(s[start:end])
		if match == nil {
			continue
		}
		for i := range match {
			if match[i] >= 0 {
				match[i] += start
			}
		}
		match[0] = at // exclude the consumed leading context byte from the full match
		matches = append(matches, match)
		next = match[1]
	}
	return matches, true
}
