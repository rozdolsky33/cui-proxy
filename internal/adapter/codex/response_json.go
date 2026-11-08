package codex

import (
	"encoding/json"
	"fmt"
	"strings"

	"cui-proxy/internal/transform"
)

// Bound buffered argument data across a response. Ordinary text streams retain
// only a possible synthetic suffix; JSON needs complete tokens and validation.
const maxArgumentBytes = 20 << 20

// restoreJSONContent handles complete decoded string content, including JSON
// nested inside it. Each level is decoded/restored/re-encoded independently.
func restoreJSONContent(text string, r *transform.Restorer, depth int) (string, error) {
	if c := first(text); (c == '{' || c == '[') && json.Valid([]byte(text)) {
		n, err := parse([]byte(text), depth)
		if err != nil {
			return "", err
		}
		if err = restoreJSONNode(n, r, depth); err != nil {
			return "", err
		}
		if n.dirty {
			return string(n.encode(false)), nil
		}
		return text, nil
	}
	return r.Text(text), nil
}

func restoreJSONNode(n *node, r *transform.Restorer, depth int) error {
	switch n.kind {
	case '{', '[':
		for i, key := range n.keys {
			if out := r.Text(key); out != key {
				n.keys[i] = out
				n.dirty = true
			}
		}
		for _, kid := range n.kids {
			if err := restoreJSONNode(kid, r, depth+1); err != nil {
				return err
			}
			n.dirty = n.dirty || kid.dirty
		}
	case '0':
		out := restoreJSONNumber(n.str, r)
		if out != n.str {
			replacement, err := parse([]byte(out), depth)
			if err != nil {
				return err
			}
			*n = *replacement
			n.dirty = true
		}
	case '"':
		out, err := restoreJSONContent(n.str, r, depth)
		if err != nil {
			return err
		}
		if out != n.str {
			n.setString(out)
		}
	}
	return nil
}

// argumentStream buffers one JSON string or number token, restoring after decoding
// it. Escape sequences and chunk boundaries are never treated as content.
// Raw input is retained (with a response-wide bound) for final JSON validation.
type argumentStream struct {
	r               *transform.Restorer
	raw             strings.Builder
	token           strings.Builder
	quoted, escaped bool
	depth           int
	containers      []byte
	key             bool
	number          strings.Builder
}

func (st *argumentStream) Write(chunk string) (string, error) {
	st.raw.WriteString(chunk)
	var out strings.Builder
	for i := 0; i < len(chunk); i++ {
		c := chunk[i]
		if st.number.Len() > 0 {
			if isNumByte(c) {
				st.number.WriteByte(c)
				continue
			}
			out.WriteString(restoreJSONNumber(st.number.String(), st.r))
			st.number.Reset()
		}
		if !st.quoted {
			if c == '-' || c >= '0' && c <= '9' {
				st.number.WriteByte(c)
				continue
			}
			if c == '"' {
				st.quoted = true
				st.token.WriteByte(c)
				continue
			}
			if c == '{' || c == '[' {
				st.containers = append(st.containers, c)
				st.key = c == '{'
				st.depth++
				if st.depth > maxDepth {
					return "", ErrDepth
				}
			}
			if c == '}' || c == ']' {
				if len(st.containers) == 0 {
					return "", fmt.Errorf("codex: invalid argument structure")
				}
				st.containers = st.containers[:len(st.containers)-1]
				st.key = false
				st.depth--
			}
			if c == ':' {
				st.key = false
			}
			if c == ',' {
				st.key = len(st.containers) > 0 && st.containers[len(st.containers)-1] == '{'
			}
			out.WriteByte(c)
			continue
		}
		st.token.WriteByte(c)
		if st.escaped {
			st.escaped = false
			continue
		}
		if c == '\\' {
			st.escaped = true
			continue
		}
		if c != '"' {
			continue
		}
		raw := st.token.String()
		var text string
		if err := json.Unmarshal([]byte(raw), &text); err != nil {
			return "", fmt.Errorf("codex: invalid argument string")
		}
		restored := st.r.Text(text)
		if !st.key {
			var err error
			restored, err = restoreJSONContent(text, st.r, st.depth)
			if err != nil {
				return "", err
			}
		}
		if restored == text {
			out.WriteString(raw)
		} else {
			out.Write(quote(restored))
		}
		st.quoted = false
		st.token.Reset()
	}
	return out.String(), nil
}

func (st *argumentStream) Finish() (string, error) {
	if st.quoted || !json.Valid([]byte(st.raw.String())) {
		return "", fmt.Errorf("codex: incomplete or invalid arguments")
	}
	var tail string
	if st.number.Len() > 0 {
		tail = restoreJSONNumber(st.number.String(), st.r)
		st.number.Reset()
	}
	return tail, nil
}

// restoreArgumentDocument also supports a scalar JSON argument without treating
// its quotes or escape sequences as plain content.
func restoreArgumentDocument(text string, r *transform.Restorer) (string, error) {
	n, err := parse([]byte(text), 0)
	if err != nil {
		return "", err
	}
	if err = restoreJSONNode(n, r, 0); err != nil {
		return "", err
	}
	if n.dirty {
		return string(n.encode(false)), nil
	}
	return text, nil
}

// A numeric synthetic can be returned by the model as a number. Preserve that
// type only when the real spelling is also a JSON number; formatted card
// numbers, for example, must be quoted to keep the argument document valid.
func restoreJSONNumber(text string, r *transform.Restorer) string {
	out := r.Text(text)
	if out == text {
		return text
	}
	if c := first(out); (c == '-' || c >= '0' && c <= '9') && json.Valid([]byte(out)) {
		return out
	}
	return string(quote(out))
}
