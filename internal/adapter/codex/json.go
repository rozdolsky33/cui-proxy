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
	var b bytes.Buffer
	n.write(&b, canonical)
	return b.Bytes()
}

func (n *node) write(b *bytes.Buffer, canonical bool) {
	if !n.dirty && !canonical || n.kind == '0' && !n.dirty || n.kind == 'l' {
		b.Write(n.raw)
		return
	}
	switch n.kind {
	case '0', 'l':
		b.Write(n.raw)
	case '{', '[':
		b.WriteByte(n.kind)
		for i, kid := range n.kids {
			if i > 0 {
				b.WriteByte(',')
			}
			if n.kind == '{' {
				writeQuote(b, n.keys[i])
				b.WriteByte(':')
			}
			kid.write(b, canonical)
		}
		if n.kind == '{' {
			b.WriteByte('}')
		} else {
			b.WriteByte(']')
		}
	default:
		writeQuote(b, n.str)
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
