package codex

import (
	"encoding/json"
	"net/http"
	"unicode/utf8"

	"cui-proxy/internal/adapter"
	"cui-proxy/internal/transform"
)

const turnMetadataHeader = "X-Codex-Turn-Metadata"
const maxTurnMetadataBytes = 64 << 10

// SanitizeHeaders rewrites only confirmed content fields in Codex turn
// metadata. Session/routing fields and unknown fields are inspected in decoded
// form but never changed. Authentication and other headers are left alone.
func (*Adapter) SanitizeHeaders(h http.Header, s *transform.Sanitizer) error {
	values, present := h[turnMetadataHeader]
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
	// Also inspect decoded rewritten keys and strings. This catches custom rules
	// on protocol-looking keys that the shared body walker deliberately preserves.
	if err := inspectMetadata(root, s); err != nil {
		return err
	}
	if root.dirty {
		out := root.encode(false)
		if len(out) > maxTurnMetadataBytes {
			return adapter.ErrBadBody
		}
		h.Set(turnMetadataHeader, string(out))
	}
	return nil
}

func inspectMetadata(n *node, s *transform.Sanitizer) error {
	for _, key := range n.keys {
		if err := inspectMetadataText(key, s); err != nil {
			return err
		}
	}
	if n.kind == '"' || n.kind == '0' {
		if err := inspectMetadataText(n.str, s); err != nil {
			return err
		}
	}
	for _, child := range n.kids {
		if err := inspectMetadata(child, s); err != nil {
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
