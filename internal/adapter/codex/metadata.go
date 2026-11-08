package codex

import (
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"

	"cui-proxy/internal/adapter"
	"cui-proxy/internal/transform"
)

const turnMetadataHeader = "X-Codex-Turn-Metadata"
const maxTurnMetadataBytes = 64 << 10

func (*Adapter) StructuredHeaders() []string { return []string{turnMetadataHeader} }

// SanitizeHeaders rewrites only confirmed content fields in Codex turn
// metadata. Session/routing fields and unknown fields are inspected in decoded
// form but never changed. Authentication and other headers are left alone.
func (*Adapter) SanitizeHeaders(h http.Header, s *transform.Sanitizer) error {
	var values []string
	present := false
	for key, vv := range h {
		if strings.EqualFold(key, turnMetadataHeader) {
			present = true
			values = append(values, vv...)
		}
	}
	if !present {
		return nil
	}
	if len(values) != 1 || len(values[0]) > maxTurnMetadataBytes {
		return adapter.ErrBadBody
	}
	raw := []byte(values[0])
	if first(raw) != '{' || !utf8.Valid(raw) || !json.Valid(raw) {
		return adapter.ErrBadBody
	}
	root, err := parse(raw, 0)
	if err != nil {
		return err
	}
	if err := checkJSONStructure(string(raw), s); err != nil {
		return err
	}
	if err := checkJSONStructure(string(root.encode(true)), s); err != nil {
		return err
	}
	w := reqWalker{s: s}
	for i, key := range root.keys {
		if err := inspectMetadataText(key, s); err != nil {
			return err
		}
		child := root.kids[i]
		switch key {
		case "workspaces", "cwd":
			if key == "workspaces" && child.kind != '{' || key == "cwd" && child.kind != '"' {
				return adapter.ErrBadBody
			}
			if err := w.walk(child, scope{pos: posOther, inner: true, depth: 1}); err != nil {
				return err
			}
			root.dirty = root.dirty || child.dirty
		default:
			if err := inspectMetadata(child, s); err != nil {
				return err
			}
			// Apply the same depth-bounded embedded-JSON and exact-number
			// inspection as body content, but refuse any required rewrite.
			if err := w.walk(child, scope{pos: posOther, inner: true, depth: 1}); err != nil {
				return err
			}
			if child.dirty {
				return adapter.ErrBadBody
			}
		}
	}
	// Recheck decoded rewritten content, including embedded JSON, without
	// confusing serialization escapes with sensitive text.
	if err := inspectMetadata(root, s); err != nil {
		return err
	}
	if root.dirty {
		out := root.encode(false)
		if len(out) > maxTurnMetadataBytes {
			return adapter.ErrBadBody
		}
		for key := range h {
			if strings.EqualFold(key, turnMetadataHeader) {
				delete(h, key)
			}
		}
		h.Set(turnMetadataHeader, string(out))
	}
	return nil
}

func inspectMetadata(n *node, s *transform.Sanitizer) error { return inspectMetadataAt(n, s, 0) }

func inspectMetadataAt(n *node, s *transform.Sanitizer, depth int) error {
	if depth > maxDepth {
		return ErrDepth
	}
	for _, key := range n.keys {
		if err := inspectMetadataText(key, s); err != nil {
			return err
		}
	}
	if n.kind == '"' && (first(n.str) == '{' || first(n.str) == '[') && json.Valid([]byte(n.str)) {
		inner, err := parse([]byte(n.str), depth)
		if err != nil {
			return err
		}
		if err := checkJSONStructure(n.str, s); err != nil {
			return err
		}
		return inspectMetadataAt(inner, s, depth)
	}
	if n.kind == '"' || n.kind == '0' {
		if err := inspectMetadataText(n.str, s); err != nil {
			return err
		}
	}
	for _, child := range n.kids {
		if err := inspectMetadataAt(child, s, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func inspectMetadataText(text string, s *transform.Sanitizer) error {
	if spans := s.Detect(text); len(spans) > 0 {
		return &transform.SpanError{Kind: adapter.ErrBadBody, Rule: spans[0].Rule, Class: spans[0].Class}
	}
	return nil
}
