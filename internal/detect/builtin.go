package detect

import "regexp"

type builtinDef struct {
	class, pattern string
	refine         refiner
	find           finder
	group          bool
}

var builtinDefs = []builtinDef{
	{class: "ocid", pattern: `ocid1\.[A-Za-z0-9_-]+\.[A-Za-z0-9-]+\.[A-Za-z0-9-]*\.[A-Za-z0-9]+`, refine: ocidRefine},
	{class: "ipv4", pattern: `\d{1,3}(?:\.\d{1,3}){3}`, refine: ipv4Refine},
	{class: "ipv6", pattern: `[0-9A-Fa-f:.]{3,}`, find: ipv6Find},
	{class: "cidr", pattern: `[0-9A-Fa-f:.]{3,}/\d{1,3}`, find: cidrFind},
	{class: "email", pattern: `[\p{L}\p{N}._%+-]+@(?:[A-Za-z0-9-]+\.)+[A-Za-z]{2,}`},
	{class: "ssn", pattern: `\d{3}-\d{2}-\d{4}`, refine: ssnRefine},
	{class: "card", pattern: `\d(?:[ -]?\d)*`, find: cardFind},
	// A card window strictly inside a longer digit run masks the whole run.
	{class: "secret", pattern: `\d(?:[ -]?\d)*`, find: cardRunFind},
	// secrets: the span is the token/blob itself, not the "Bearer" keyword.
	// Bearer tokens need at least 8 token-charset characters after "Bearer ":
	// short enough to catch opaque tokens, long enough to skip prose ("Bearer token").
	{class: "secret", pattern: `(?i)\bbearer\s+([A-Za-z0-9._~+/=-]{8,})`, group: true},
	{class: "secret", pattern: `\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`},
	// An unterminated block (truncated body) runs to the end of the text: fail closed.
	{class: "secret", pattern: `-----BEGIN [A-Z ]*PRIVATE KEY(?: BLOCK)?-----(?s:.*?)(?:-----END [A-Z ]*PRIVATE KEY(?: BLOCK)?-----|\z)`},
}

// builtinSet returns the v1 detectors; each is named prefix+class.
func builtinSet(prefix string) []Detector {
	out := make([]Detector, 0, len(builtinDefs))
	for _, b := range builtinDefs {
		out = append(out, &regexDetector{
			name: prefix + b.class, class: b.class,
			re: regexp.MustCompile(b.pattern), refine: b.refine, find: b.find, group: b.group,
		})
	}
	return out
}
