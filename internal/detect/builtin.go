package detect

import "regexp"

type builtinDef struct {
	class, pattern string
	refine         refiner
	group          bool
}

var builtinDefs = []builtinDef{
	{class: "ocid", pattern: `ocid1\.[A-Za-z0-9_-]+\.[A-Za-z0-9-]+\.[A-Za-z0-9-]*\.[A-Za-z0-9]+`, refine: ocidRefine},
	{class: "ipv4", pattern: `\d{1,3}(?:\.\d{1,3}){3}`, refine: ipv4Refine},
	{class: "ipv6", pattern: `[0-9A-Fa-f:.]{3,}`, refine: ipv6Refine},
	{class: "cidr", pattern: `[0-9A-Fa-f:.]{3,}/\d{1,3}`, refine: cidrRefine},
	{class: "email", pattern: `[A-Za-z0-9._%+-]+@(?:[A-Za-z0-9-]+\.)+[A-Za-z]{2,}`},
	{class: "ssn", pattern: `\b\d{3}-\d{2}-\d{4}\b`, refine: ssnRefine},
	{class: "card", pattern: `\b\d(?:[ -]?\d){12,18}\b`, refine: luhnRefine},
	// secrets: the span is the token/blob itself, not the "Bearer" keyword.
	{class: "secret", pattern: `(?i)\bbearer\s+([A-Za-z0-9._~+/=-]{16,})`, group: true},
	{class: "secret", pattern: `\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`},
	// An unterminated block (truncated body) runs to the end of the text: fail closed.
	{class: "secret", pattern: `-----BEGIN [A-Z ]*PRIVATE KEY-----(?s:.*?)(?:-----END [A-Z ]*PRIVATE KEY-----|\z)`},
}

// builtinSet returns the v1 detectors; each is named prefix+class.
func builtinSet(prefix string) []Detector {
	out := make([]Detector, 0, len(builtinDefs))
	for _, b := range builtinDefs {
		out = append(out, &regexDetector{
			name: prefix + b.class, class: b.class,
			re: regexp.MustCompile(b.pattern), refine: b.refine, group: b.group,
		})
	}
	return out
}
