package codex

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"cui-proxy/internal/adapter"
	"cui-proxy/internal/transform"
)

// ErrBadBody means the request body is empty, not valid UTF-8, not a single
// JSON object, or contains a binary (image, file, audio) part. Block. It is
// adapter.ErrBadBody.
var ErrBadBody = adapter.ErrBadBody

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
// is sanitized, except the string values at protocol positions
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

// checkBlocked refuses text that a blocking rule matches, without rewriting
// it. It is how fields passed through unscanned are still held to the policy.
func (w reqWalker) checkBlocked(s string) error {
	if err := w.s.BlockedBy(s); err != nil {
		return fmt.Errorf("codex: sanitize request: %w", err)
	}
	return nil
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
		if sc.inner || !protocolKeys[k] {
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
			// Passed byte for byte, but never past a blocking rule. The one
			// exception is the CONTENT of encrypted_content: vendor ciphertext
			// resent as received, never a place for user text, and megabytes
			// of base64 invite false positives. While any rule blocks it must
			// still be shaped like ciphertext, so text cannot ride in it.
			if k == "encrypted_content" {
				if w.s.HasBlockingRules() && !cipherShaped(kid.str) {
					return fmt.Errorf("%w: encrypted_content is not base64 ciphertext", ErrBadBody)
				}
			} else if err := w.checkBlocked(kid.str); err != nil {
				return err
			}
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

// number sanitizes integer literals (including short custom identifiers); a
// changed value becomes a JSON string. Decimal and exponent forms are checked
// both as written and as exact expanded digits. A sensitive non-integer
// spelling is refused: rewriting its expansion would lose the original form.
func (w reqWalker) number(n *node) error {
	lit := n.str
	if err := w.checkBlocked(lit); err != nil {
		return err
	}
	if strings.ContainsAny(lit, ".eE") {
		// 4.111111111111111e15 is the integer 4111111111111111 in another
		// spelling: check the plain-digit form too, expanded exactly.
		plain, ok := expandNumber(lit)
		if !ok {
			return fmt.Errorf("%w: number literal exponent out of range", ErrBadBody)
		}
		if plain != "" {
			if err := w.checkBlocked(plain); err != nil {
				return err
			}
		}
		if len(w.s.Detect(lit)) > 0 || plain != "" && len(w.s.Detect(plain)) > 0 {
			return fmt.Errorf("%w: sensitive decimal or exponent number", ErrBadBody)
		}
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

// str sanitizes decoded JSON content, never its serialized escape syntax.
// Matches spanning JSON tokens cannot be rewritten safely and fail closed.
func (w reqWalker) str(n *node, sc scope) error {
	s := n.str
	if c := first(s); (c == '{' || c == '[') && json.Valid([]byte(s)) {
		tree, err := parse([]byte(s), sc.depth)
		if err != nil {
			return err
		}
		if err := checkJSONStructure(s, w.s); err != nil {
			return err
		}
		if err := checkJSONStructure(string(tree.encode(true)), w.s); err != nil {
			return err
		}
		if err := w.walk(tree, scope{pos: posOther, inner: true, depth: sc.depth}); err != nil {
			return err
		}
		if tree.dirty {
			n.setString(string(tree.encode(false)))
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

// maxExponent bounds the exponent of a number literal that is expanded for
// scanning; beyond it no real number is being written and the literal is
// refused.
const maxExponent = 400

// expandNumber returns the decimal digits of the integer part of a JSON number
// literal, expanded exactly from its digits and exponent (never through
// float64, which would corrupt a 17 to 19 digit number). The result is empty
// when the literal has no integer digits (0.5, 1e-5). ok is false when the
// exponent is out of range.
func expandNumber(lit string) (string, bool) {
	s := strings.TrimPrefix(lit, "-")
	exp := 0
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		e, err := strconv.Atoi(s[i+1:])
		if err != nil || e > maxExponent || e < -maxExponent {
			return "", false
		}
		exp, s = e, s[:i]
	}
	intPart, frac, _ := strings.Cut(s, ".")
	digits := intPart + frac
	point := len(intPart) + exp
	if point <= 0 {
		return "", true
	}
	if point > len(digits) {
		digits += strings.Repeat("0", point-len(digits))
	}
	return strings.TrimLeft(digits[:point], "0"), true
}

// cipherShaped reports whether v consists only of base64 or base64url
// characters (empty is fine): the shape of the vendor's encrypted_content.
func cipherShaped(v string) bool {
	for i := 0; i < len(v); i++ {
		c := v[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '+' || c == '/' || c == '=' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
