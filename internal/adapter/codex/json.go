package codex

import (
	"bytes"
	"encoding/json"
	"unicode/utf8"

	"cui-proxy/internal/adapter"
)

// A JSON document is parsed once into a tree whose nodes slice into the
// original buffer (no per-level copies). Anything not rewritten is emitted
// as its original bytes. parse must only be given input that json.Valid
// accepts; it then only has to find token boundaries.

// maxDepth bounds container nesting (objects plus arrays) in a request body,
// counted across JSON documents carried in strings, and in one response event.
const maxDepth = 64

// ErrDepth means the JSON nests deeper than maxDepth. Block. It is
// adapter.ErrDepth.
var ErrDepth = adapter.ErrDepth

type node struct {
	kind  byte   // '{', '[', '"' (string), '0' (number), 'l' (true/false/null)
	raw   []byte // original bytes
	str   string // decoded string, or the number literal
	keys  []string
	kids  []*node
	dirty bool // rewritten: encode from fields, not raw
}

// parse parses valid JSON. base is the number of containers already
// enclosing buf (for JSON carried in a string).
func parse(buf []byte, base int) (*node, error) {
	p := parser{buf: buf}
	n, err := p.value(base)
	if err != nil {
		return nil, err
	}
	return n, nil
}

type parser struct {
	buf []byte
	i   int
}

func (p *parser) ws() {
	for p.i < len(p.buf) {
		switch p.buf[p.i] {
		case ' ', '\t', '\r', '\n':
			p.i++
		default:
			return
		}
	}
}

func (p *parser) value(depth int) (*node, error) {
	p.ws()
	start := p.i
	switch c := p.buf[p.i]; c {
	case '{', '[':
		if depth+1 > maxDepth {
			return nil, ErrDepth
		}
		n := &node{kind: c}
		p.i++
		for {
			p.ws()
			if b := p.buf[p.i]; b == '}' || b == ']' {
				p.i++
				break
			} else if b == ',' {
				p.i++
				p.ws()
			}
			if c == '{' {
				k := p.str()
				n.keys = append(n.keys, k.str)
				p.ws()
				p.i++ // ':'
			}
			kid, err := p.value(depth + 1)
			if err != nil {
				return nil, err
			}
			n.kids = append(n.kids, kid)
		}
		n.raw = p.buf[start:p.i]
		return n, nil
	case '"':
		return p.str(), nil
	case 't', 'f', 'n':
		for p.i < len(p.buf) && p.buf[p.i] >= 'a' && p.buf[p.i] <= 'z' {
			p.i++
		}
		return &node{kind: 'l', raw: p.buf[start:p.i]}, nil
	default:
		for p.i < len(p.buf) && isNumByte(p.buf[p.i]) {
			p.i++
		}
		return &node{kind: '0', raw: p.buf[start:p.i], str: string(p.buf[start:p.i])}, nil
	}
}

func isNumByte(c byte) bool {
	return c >= '0' && c <= '9' || c == '-' || c == '+' || c == '.' || c == 'e' || c == 'E'
}

// str parses a string token at p.i.
func (p *parser) str() *node {
	start := p.i
	p.i++
	esc := false
	for {
		j := bytes.IndexAny(p.buf[p.i:], `"\`)
		p.i += j
		if p.buf[p.i] == '\\' {
			esc = true
			p.i += 2
			continue
		}
		p.i++
		break
	}
	raw := p.buf[start:p.i]
	return &node{kind: '"', raw: raw, str: decodeString(raw, esc)}
}

// decodeString decodes a valid JSON string token, as encoding/json does
// (invalid UTF-8 becomes U+FFFD).
func decodeString(raw []byte, esc bool) string {
	if body := raw[1 : len(raw)-1]; !esc && utf8.Valid(body) {
		return string(body)
	}
	var s string
	_ = json.Unmarshal(raw, &s) // valid by contract
	return s
}

// get returns the value of the first member named key of an object node.
func (n *node) get(key string) *node {
	for i, k := range n.keys {
		if k == key {
			return n.kids[i]
		}
	}
	return nil
}

// getString returns the string value of member key, if it is a string.
func (n *node) getString(key string) (string, bool) {
	if v := n.get(key); v != nil && v.kind == '"' {
		return v.str, true
	}
	return "", false
}

// setString replaces n's value with the string s.
func (n *node) setString(s string) {
	n.kind, n.str, n.dirty = '"', s, true
}

// encode returns n as JSON: original bytes where nothing changed. With
// canonical, every container and string is re-encoded from decoded values.
func (n *node) encode(canonical bool) []byte {
	if !n.dirty && !canonical {
		return bytes.Clone(n.raw)
	}
	// Larger rewritten containers can avoid repeated final-buffer growth by
	// preparing their already-standard-encoded fragments. Bound metadata work;
	// raw, small, very large and unusually fragmented trees use direct output.
	if (n.dirty || canonical) && (n.kind == '{' || n.kind == '[') && len(n.raw) >= 4096 && len(n.raw) <= 1<<20 {
		if count := n.outputPartCount(canonical, maxOutputParts); count >= 16 && count < maxOutputParts {
			plan := &jsonOutputPlan{parts: make([]jsonOutputPart, 0, count)}
			n.emit(plan, canonical)
			return plan.bytes()
		}
	}
	var b bytes.Buffer
	n.write(&b, canonical)
	return b.Bytes()
}

const maxOutputParts = 4096

// outputPartCount bounds planning metadata without scanning string contents.
func (n *node) outputPartCount(canonical bool, limit int) int {
	if limit <= 0 {
		return 0
	}
	if !n.dirty && !canonical || n.kind != '{' && n.kind != '[' {
		return 1
	}
	count := min(2, limit)
	add := func(value int) { count += min(value, limit-count) }
	for i, kid := range n.kids {
		if count == limit {
			break
		}
		if i > 0 {
			add(1)
		}
		if n.kind == '{' {
			add(2)
		}
		add(kid.outputPartCount(canonical, limit-count))
	}
	return count
}

type jsonOutput interface {
	raw([]byte)
	quoted(string)
	token(byte)
}

type directJSONOutput struct{ buffer *bytes.Buffer }

func (o directJSONOutput) raw(p []byte)    { o.buffer.Write(p) }
func (o directJSONOutput) quoted(s string) { writeQuote(o.buffer, s) }
func (o directJSONOutput) token(b byte)    { o.buffer.WriteByte(b) }

type jsonOutputPart struct {
	raw   []byte
	token byte // structural JSON tokens are always nonzero
}

type jsonOutputPlan struct {
	parts []jsonOutputPart
	size  int
}

func (p *jsonOutputPlan) addSize(size int) {
	if size > int(^uint(0)>>1)-p.size {
		panic("codex: JSON output too large")
	}
	p.size += size
}

func (p *jsonOutputPlan) raw(raw []byte) {
	p.addSize(len(raw))
	p.parts = append(p.parts, jsonOutputPart{raw: raw})
}
func (p *jsonOutputPlan) quoted(s string) { p.raw(quote(s)) }
func (p *jsonOutputPlan) token(b byte) {
	p.addSize(1)
	p.parts = append(p.parts, jsonOutputPart{token: b})
}
func (p *jsonOutputPlan) bytes() []byte {
	out := make([]byte, p.size)
	position := 0
	for _, part := range p.parts {
		if part.token != 0 {
			out[position] = part.token
			position++
		} else {
			position += copy(out[position:], part.raw)
		}
	}
	return out
}

func (n *node) write(b *bytes.Buffer, canonical bool) {
	n.emit(directJSONOutput{b}, canonical)
}

func (n *node) emit(out jsonOutput, canonical bool) {
	if !n.dirty && !canonical || n.kind == '0' && !n.dirty || n.kind == 'l' {
		out.raw(n.raw)
		return
	}
	switch n.kind {
	case '0', 'l':
		out.raw(n.raw)
	case '{', '[':
		out.token(n.kind)
		for i, kid := range n.kids {
			if i > 0 {
				out.token(',')
			}
			if n.kind == '{' {
				out.quoted(n.keys[i])
				out.token(':')
			}
			kid.emit(out, canonical)
		}
		if n.kind == '{' {
			out.token('}')
		} else {
			out.token(']')
		}
	default:
		out.quoted(n.str)
	}
}

// quote encodes s as a JSON string without HTML escaping.
func quote(s string) []byte {
	var b bytes.Buffer
	writeQuote(&b, s)
	return b.Bytes()
}

// writeQuote avoids an intermediate string buffer while retaining the standard
// encoder's escaping and invalid-UTF-8 behavior. A string always encodes, and a
// bytes.Buffer write cannot fail; remove only Encode's final newline.
func writeQuote(b *bytes.Buffer, s string) {
	enc := json.NewEncoder(b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	b.Truncate(b.Len() - 1)
}
