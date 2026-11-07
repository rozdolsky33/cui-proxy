package codex

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"cui-proxy/internal/transform"
)

// ErrBadBody means the request body is empty, not valid UTF-8, not a single
// JSON object, or contains a binary (image, file, audio) part. Block.
var ErrBadBody = errors.New("codex: request body rejected")

// Keys that carry or reference binary content. Any of them in the request
// protocol JSON blocks the request: its content cannot be scanned.
var binaryKeys = map[string]bool{"image_url": true, "file_data": true, "file_id": true}

// A "type" value containing one of these marks a binary part.
var binaryTypes = []string{"image", "file", "audio"}

// Top-level keys whose subtrees define tools or output formats. Their
// strings are still scanned, but schema property names such as "file_id" or
// tool types such as "image_generation" describe data and do not block.
var definitionKeys = map[string]bool{"tools": true, "tool_choice": true, "text": true}

// sanitizeRequest rewrites a Responses API request body. Every string leaf is
// sanitized except values of opaque keys; strings that are themselves JSON
// objects or arrays (function-call arguments, metadata) are sanitized leaf by
// leaf and then scanned again as text. Untouched values keep their original
// bytes; a body with nothing to replace is returned as is. On error the
// result is nil.
func sanitizeRequest(body []byte, s *transform.Sanitizer) ([]byte, error) {
	if first(body) != '{' || !utf8.Valid(body) || !json.Valid(body) {
		return nil, fmt.Errorf("%w: not a JSON object", ErrBadBody)
	}
	w := reqWalker{s: s}
	out, changed, err := w.object(body, scope{top: true})
	if err != nil {
		return nil, err
	}
	if !changed {
		return body, nil
	}
	return out, nil
}

type reqWalker struct{ s *transform.Sanitizer }

// scope says where a value sits.
type scope struct {
	top        bool // the request object itself
	inner      bool // inside a JSON document carried in a string: no opaque keys, no binary checks
	definition bool // inside tools / tool_choice / text: no binary checks
}

func (w reqWalker) value(raw json.RawMessage, sc scope) (json.RawMessage, bool, error) {
	switch first(raw) {
	case '{':
		return w.object(raw, sc)
	case '[':
		es, err := elements(raw)
		if err != nil {
			return nil, false, err
		}
		changed := false
		for i, e := range es {
			ne, ch, err := w.value(e, sc)
			if err != nil {
				return nil, false, err
			}
			if ch {
				es[i], changed = ne, true
			}
		}
		if !changed {
			return raw, false, nil
		}
		return encodeArray(es), true, nil
	case '"':
		var str string
		if err := json.Unmarshal(raw, &str); err != nil {
			return nil, false, errShape
		}
		out, err := w.str(str)
		if err != nil {
			return nil, false, err
		}
		if out == str {
			return raw, false, nil
		}
		return quote(out), true, nil
	}
	return raw, false, nil // number, true, false, null
}

func (w reqWalker) object(raw json.RawMessage, sc scope) (json.RawMessage, bool, error) {
	ms, err := members(raw)
	if err != nil {
		return nil, false, err
	}
	if !sc.inner && !sc.definition {
		if err := checkBinary(ms); err != nil {
			return nil, false, err
		}
	}
	changed := false
	for i, m := range ms {
		if !sc.inner && opaque[m.key] {
			continue
		}
		child := scope{inner: sc.inner, definition: sc.definition || sc.top && definitionKeys[m.key]}
		nv, ch, err := w.value(m.val, child)
		if err != nil {
			return nil, false, err
		}
		if ch {
			ms[i].val, changed = nv, true
		}
	}
	if !changed {
		return raw, false, nil
	}
	return encodeObject(ms), true, nil
}

// checkBinary rejects an object that is or references a binary part. Error
// messages name the key, never a value. Every "type" member is checked, so a
// duplicate key cannot hide a binary type from a last-key-wins parser.
func checkBinary(ms []member) error {
	for _, m := range ms {
		if binaryKeys[m.key] {
			return fmt.Errorf("%w: binary content key %q", ErrBadBody, m.key)
		}
		var t string
		if m.key != "type" || first(m.val) != '"' || json.Unmarshal(m.val, &t) != nil {
			continue
		}
		lt := strings.ToLower(t)
		for _, b := range binaryTypes {
			if strings.Contains(lt, b) {
				return fmt.Errorf("%w: %s part", ErrBadBody, b)
			}
		}
	}
	return nil
}

// str sanitizes one string leaf. A string holding a JSON object or array is
// walked as JSON (so escapes inside it cannot hide a value), then the whole
// re-encoded text is scanned again; the result must still be valid JSON.
func (w reqWalker) str(s string) (string, error) {
	if c := first([]byte(s)); (c == '{' || c == '[') && json.Valid([]byte(s)) {
		out, changed, err := w.value([]byte(s), scope{inner: true})
		if err != nil {
			return "", err
		}
		text := s
		if changed {
			text = string(out)
		}
		again, err := w.s.Text(text)
		if err != nil {
			return "", fmt.Errorf("codex: sanitize request: %w", err)
		}
		if again != text && !json.Valid([]byte(again)) {
			return "", fmt.Errorf("%w: embedded JSON broken by sanitizing", ErrBadBody)
		}
		return again, nil
	}
	out, err := w.s.Text(s)
	if err != nil {
		return "", fmt.Errorf("codex: sanitize request: %w", err)
	}
	return out, nil
}
