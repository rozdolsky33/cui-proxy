package codex

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"cui-proxy/internal/transform"
)

// ErrBadBody means the request body is empty, not valid UTF-8, not a single
// JSON object, or contains a binary (image, file, audio) part. Block.
var ErrBadBody = errors.New("codex: request body rejected")

// Where a value sits in the Responses API request.
type pos int

const (
	posOther    pos = iota
	posTop          // the request object
	posItems        // top.input
	posItem         // an element of top.input
	posParts        // item.content / item.output / item.summary
	posPart         // an element of those
	posTools        // top.tools
	posTool         // an element of top.tools
	posIncludes     // top.include
	posInclude      // a direct element of top.include
	posText         // top.text
)

// childPos is the position of member key of an object at p.
func childPos(p pos, key string) pos {
	switch p {
	case posTop:
		switch key {
		case "input":
			return posItems
		case "tools":
			return posTools
		case "include":
			return posIncludes
		case "text":
			return posText
		}
	case posItem:
		switch key {
		case "content", "output", "summary":
			return posParts
		}
	}
	return posOther
}

// elemPos is the position of an element of an array at p.
func elemPos(p pos) pos {
	switch p {
	case posItems:
		return posItem
	case posParts:
		return posPart
	case posTools:
		return posTool
	case posIncludes:
		return posInclude
	}
	return posOther
}

// opaqueAt reports whether the STRING value v of member key of object obj
// at p is a protocol value passed byte-identical (never scanned). Only these
// protocol positions are opaque; everywhere else (schemas, client_metadata,
// tool results, unknown keys) every string is scanned, and objects or arrays
// under these names are always walked.
func opaqueAt(p pos, key string, obj, v *node) bool {
	switch p {
	case posTop:
		switch key {
		case "model", "prompt_cache_key":
			return true
		case "tool_choice":
			return v.str == "auto" || v.str == "none" || v.str == "required"
		}
	case posItem:
		switch key {
		case "id", "call_id", "type", "role", "status":
			return true
		case "name":
			t := itemType(obj)
			return t == "function_call" || t == "custom_tool_call"
		case "encrypted_content":
			t := itemType(obj)
			return t == "reasoning" || t == "compaction"
		}
	case posPart:
		return key == "type" || key == "id" || key == "status"
	case posTool:
		return key == "type" || key == "name"
	}
	return false
}

// itemType returns the item's type if every "type" member is the same
// string, else "" (a duplicate key cannot borrow another type's opacity).
func itemType(obj *node) string {
	t := ""
	for i, k := range obj.keys {
		if k != "type" {
			continue
		}
		v := obj.kids[i]
		if v.kind != '"' || t != "" && v.str != t || v.str == "" {
			return ""
		}
		t = v.str
	}
	return t
}

// An include entry is opaque only if it is a dotted identifier AND a known
// Responses API include value: the pattern alone would also admit an OCID
// ("ocid1.instance.oc1...."). Any other include string is scanned.
var includeEntry = regexp.MustCompile(`^[a-z_][a-z0-9_]*(\.[a-z_][a-z0-9_]*)*$`)

var knownIncludes = map[string]bool{
	"reasoning.encrypted_content":           true,
	"message.output_text.logprobs":          true,
	"message.input_image.image_url":         true,
	"computer_call_output.output.image_url": true,
	"file_search_call.results":              true,
	"web_search_call.action.sources":        true,
	"web_search_call.results":               true,
	"code_interpreter_call.outputs":         true,
}

func protocolInclude(s string) bool { return knownIncludes[s] && includeEntry.MatchString(s) }

// protocolKeys are field names that are not scanned as keys (any other key
// is: a key can carry a value, e.g. a host name used as a map key). They are
// fixed words, so exempting them cannot hide a value.
var protocolKeys = map[string]bool{}

func init() {
	for _, k := range strings.Fields(`
		id call_id item_id encrypted_content signature data image_url prompt_cache_key
		model type role status name content input tools text arguments output summary
		reasoning parameters properties schema format description client_metadata store
		stream include tool_choice parallel_tool_calls instructions required enum items
		additionalProperties strict effort verbosity syntax definition execution
		external_web_access search_content_types annotations metadata user
		previous_response_id max_output_tokens temperature top_p truncation service_tier
		session_id thread_id turn_id root_turn_id x-codex-installation-id
		x-codex-turn-metadata x-codex-window-id cmd workdir`) {
		protocolKeys[k] = true
	}
}

// Keys that carry or reference binary content.
var binaryKeys = map[string]bool{"image_url": true, "file_data": true, "file_id": true}

// A "type" value containing one of these marks a binary part.
var binaryTypes = []string{"image", "file", "audio"}

// sanitizeRequest rewrites a Responses API request body: every string leaf,
// every object key (protocol field names excepted) and every integer literal
// of 13+ digits is sanitized, except the string values at protocol positions
// (opaqueAt). Strings that are themselves JSON objects or arrays are walked
// as JSON. Untouched values keep their original bytes; a body with nothing to
// replace is returned as is. On error the result is nil.
func sanitizeRequest(body []byte, s *transform.Sanitizer) ([]byte, error) {
	if first(body) != '{' || !utf8.Valid(body) || !json.Valid(body) {
		return nil, fmt.Errorf("%w: not a JSON object", ErrBadBody)
	}
	root, err := parse(body, 0)
	if err != nil {
		return nil, err
	}
	w := reqWalker{s: s}
	if err := w.walk(root, scope{pos: posTop}); err != nil {
		return nil, err
	}
	if !root.dirty {
		return body, nil
	}
	return root.encode(false), nil
}

type reqWalker struct{ s *transform.Sanitizer }

type scope struct {
	pos    pos
	inner  bool // inside JSON carried in a string: nothing opaque, no binary checks
	schema bool // inside a tool/format schema: no binary checks
	tools  bool // under top-level tools
	depth  int  // containers enclosing the value
}

func (w reqWalker) text(s string) (string, error) {
	out, err := w.s.Text(s)
	if err != nil {
		return "", fmt.Errorf("codex: sanitize request: %w", err)
	}
	return out, nil
}

// walk sanitizes n in place, marking it dirty if anything changed.
func (w reqWalker) walk(n *node, sc scope) error {
	switch n.kind {
	case '{':
		return w.object(n, sc)
	case '[':
		ep := elemPos(sc.pos)
		for _, kid := range n.kids {
			if !sc.inner && ep == posInclude && kid.kind == '"' && protocolInclude(kid.str) {
				continue // include entries are protocol values
			}
			if err := w.walk(kid, scope{pos: ep, inner: sc.inner, schema: sc.schema, tools: sc.tools, depth: sc.depth + 1}); err != nil {
				return err
			}
			n.dirty = n.dirty || kid.dirty
		}
	case '"':
		return w.str(n, sc)
	case '0':
		return w.number(n)
	}
	return nil
}

func (w reqWalker) object(n *node, sc scope) error {
	if !sc.inner && !sc.schema {
		if err := checkBinary(n, sc.pos == posTool); err != nil {
			return err
		}
	}
	for i, k := range n.keys {
		if !protocolKeys[k] {
			nk, err := w.text(k)
			if err != nil {
				return err
			}
			if nk != k {
				n.keys[i], n.dirty = nk, true
			}
		}
		kid := n.kids[i]
		if !sc.inner && kid.kind == '"' && opaqueAt(sc.pos, k, n, kid) {
			continue
		}
		tools := sc.tools || sc.pos == posTop && k == "tools"
		schema := sc.schema || !sc.inner && (k == "parameters" || k == "format" || k == "schema") &&
			(sc.tools || sc.pos == posText)
		if err := w.walk(kid, scope{pos: childPos(sc.pos, k), inner: sc.inner, schema: schema, tools: tools, depth: sc.depth + 1}); err != nil {
			return err
		}
		n.dirty = n.dirty || kid.dirty
	}
	return nil
}

// checkBinary rejects an object that is or references a binary part. Every
// member is checked, so a duplicate key cannot hide a binary type. The type
// of a tools[] entry (e.g. "image_generation") is exempt. Messages name the
// key, never a value.
func checkBinary(n *node, toolEntry bool) error {
	for i, k := range n.keys {
		kid := n.kids[i]
		if binaryKeys[k] || k == "data" && kid.kind == '"' {
			return fmt.Errorf("%w: binary content key %q", ErrBadBody, k)
		}
		if k != "type" || kid.kind != '"' || toolEntry {
			continue
		}
		lt := strings.ToLower(kid.str)
		for _, b := range binaryTypes {
			if strings.Contains(lt, b) {
				return fmt.Errorf("%w: %s part", ErrBadBody, b)
			}
		}
	}
	return nil
}

// number sanitizes an integer literal of 13 or more digits (a card number or
// account id sent as a number); a changed value becomes a JSON string.
func (w reqWalker) number(n *node) error {
	lit := n.str
	if strings.ContainsAny(lit, ".eE") || len(strings.TrimPrefix(lit, "-")) < 13 {
		return nil
	}
	out, err := w.text(lit)
	if err != nil {
		return err
	}
	if out != lit {
		n.setString(out)
	}
	return nil
}

// str sanitizes one string leaf. A string holding a JSON object or array is
// parsed (depth counted from here) and walked with nothing opaque, keys
// included; the result is re-encoded if anything changed. Then the text and,
// if still unchanged, its canonical re-encoding are scanned again as a whole
// (belt and braces), and the result must still be valid JSON.
func (w reqWalker) str(n *node, sc scope) error {
	s := n.str
	if c := first(s); (c == '{' || c == '[') && json.Valid([]byte(s)) {
		tree, err := parse([]byte(s), sc.depth)
		if err != nil {
			return err
		}
		if err := w.walk(tree, scope{pos: posOther, inner: true, depth: sc.depth}); err != nil {
			return err
		}
		text := s
		if tree.dirty {
			text = string(tree.encode(false))
		}
		out, err := w.text(text)
		if err != nil {
			return err
		}
		if out == text && !tree.dirty {
			canon := string(tree.encode(true))
			if c2, err := w.text(canon); err != nil {
				return err
			} else if c2 != canon {
				out = c2
			}
		}
		if out != s && !json.Valid([]byte(out)) {
			return fmt.Errorf("%w: embedded JSON broken by sanitizing", ErrBadBody)
		}
		if out != s {
			n.setString(out)
		}
		return nil
	}
	out, err := w.text(s)
	if err != nil {
		return err
	}
	if out != s {
		n.setString(out)
	}
	return nil
}

// first returns the first non-whitespace byte of b, or 0.
func first[T string | []byte](b T) byte {
	for i := 0; i < len(b); i++ {
		switch c := b[i]; c {
		case ' ', '\t', '\r', '\n':
			continue
		default:
			return c
		}
	}
	return 0
}
