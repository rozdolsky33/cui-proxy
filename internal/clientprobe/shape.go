// Package clientprobe records the structure of requests that a Codex client
// sends, without their content. A shape is a set of JSON key paths plus the
// values of a few protocol discriminators such as "type" and "role". Comparing
// the shape recorded for one client release with the shape of the next shows
// where the wire protocol changed.
package clientprobe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// identifier limits recorded discriminator values to protocol-style tokens.
// Anything else is content and is recorded only as "=<other>".
var identifier = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)

// opaque reports whether the children of path are caller-defined, not
// protocol structure. Tool definitions carry a JSON Schema or a grammar, and
// metadata maps are free-form.
func opaque(path string) bool {
	switch path {
	case "metadata", "client_metadata", "text.format.schema":
		return true
	}
	return strings.HasSuffix(path, "tools[].parameters") || strings.HasSuffix(path, "tools[].format")
}

// Shape returns the sorted structural entries of one JSON document. Entries
// are key paths with a JSON kind, such as "input[].content[]: array", and
// discriminator values, such as "input[].type=message". String content is
// never included.
func Shape(body []byte) ([]string, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if dec.More() {
		return nil, fmt.Errorf("decode: trailing data")
	}
	set := map[string]struct{}{}
	walk("", v, set)
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

func walk(path string, v any, set map[string]struct{}) {
	switch t := v.(type) {
	case map[string]any:
		add(set, path, "object")
		if opaque(path) {
			return
		}
		for k, child := range t {
			p := k
			if path != "" {
				p = path + "." + k
			}
			if s, ok := child.(string); ok && discriminator(k) {
				if identifier.MatchString(s) {
					set[p+"="+s] = struct{}{}
				} else {
					set[p+"=<other>"] = struct{}{}
				}
			}
			walk(p, child, set)
		}
	case []any:
		add(set, path, "array")
		for _, child := range t {
			walk(path+"[]", child, set)
		}
	case string:
		add(set, path, "string")
	case json.Number:
		add(set, path, "number")
	case bool:
		add(set, path, "bool")
	case nil:
		add(set, path, "null")
	}
}

func add(set map[string]struct{}, path, kind string) {
	if path == "" {
		path = "$"
	}
	set[path+": "+kind] = struct{}{}
}

// discriminator reports whether a string member names a protocol variant.
// Tool names are left out: the set of tools a client offers changes with
// client releases, feature flags and user configuration, and the proxy treats
// tool definitions as text.
func discriminator(key string) bool {
	return key == "type" || key == "role"
}

// Diff returns the entries only in next and the entries only in base.
func Diff(base, next []string) (added, removed []string) {
	in := func(list []string) map[string]bool {
		m := make(map[string]bool, len(list))
		for _, s := range list {
			m[s] = true
		}
		return m
	}
	b, n := in(base), in(next)
	for _, s := range next {
		if !b[s] {
			added = append(added, s)
		}
	}
	for _, s := range base {
		if !n[s] {
			removed = append(removed, s)
		}
	}
	return added, removed
}

// Merge returns the sorted union of several shapes.
func Merge(shapes ...[]string) []string {
	set := map[string]struct{}{}
	for _, s := range shapes {
		for _, e := range s {
			set[e] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
