package codex

import (
	"cui-proxy/internal/adapter"
	"cui-proxy/internal/transform"
)

// checkJSONStructure preserves inspection of patterns spanning JSON tokens
// without applying text replacements to serialization syntax. Matches wholly
// inside string contents (including keys) or numbers are handled by the decoded
// walker. Input must be valid JSON. Literals and structural matches fail closed.
func checkJSONStructure(text string, s *transform.Sanitizer) error {
	spans := s.Detect(text)
	if len(spans) == 0 {
		return nil
	}
	type interval struct{ start, end int }
	var content []interval
	for i := 0; i < len(text); {
		switch {
		case text[i] == '"':
			start := i + 1
			i++
			for text[i] != '"' {
				if text[i] == '\\' {
					i++
				}
				i++
			}
			content = append(content, interval{start, i})
			i++
		case text[i] == '-' || text[i] >= '0' && text[i] <= '9':
			start := i
			for i < len(text) && isNumByte(text[i]) {
				i++
			}
			content = append(content, interval{start, i})
		default:
			i++
		}
	}
	at := 0
	for _, sp := range spans {
		for at < len(content) && content[at].end <= sp.Start {
			at++
		}
		if at < len(content) && sp.Start >= content[at].start && sp.End <= content[at].end {
			continue
		}
		kind := adapter.ErrBadBody
		rule, class := sp.Rule, sp.Class
		if sp.Block {
			kind = transform.ErrBlocked
			rule, class = sp.BlockRule, sp.BlockClass
		}
		return &transform.SpanError{Kind: kind, Rule: rule, Class: class}
	}
	return nil
}
