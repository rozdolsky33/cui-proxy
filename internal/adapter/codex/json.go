package codex

import (
	"bytes"
	"encoding/json"
	"errors"
)

// Order-preserving JSON helpers. Values are kept as raw bytes so that
// anything not rewritten is emitted exactly as received. Inputs must be
// valid JSON (callers check json.Valid once at the top).

var errShape = errors.New("codex: malformed JSON")

type member struct {
	key string
	val json.RawMessage
}

// first returns the first non-whitespace byte of raw, or 0.
func first(raw []byte) byte {
	for _, c := range raw {
		switch c {
		case ' ', '\t', '\r', '\n':
			continue
		}
		return c
	}
	return 0
}

// members splits a JSON object into its members, in order.
func members(raw []byte) ([]member, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, errShape
	}
	var ms []member
	for dec.More() {
		t, err := dec.Token()
		k, ok := t.(string)
		if err != nil || !ok {
			return nil, errShape
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, errShape
		}
		ms = append(ms, member{k, v})
	}
	if t, err := dec.Token(); err != nil || t != json.Delim('}') {
		return nil, errShape
	}
	return ms, nil
}

// elements splits a JSON array into its elements, in order.
func elements(raw []byte) ([]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('[') {
		return nil, errShape
	}
	var es []json.RawMessage
	for dec.More() {
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, errShape
		}
		es = append(es, v)
	}
	if t, err := dec.Token(); err != nil || t != json.Delim(']') {
		return nil, errShape
	}
	return es, nil
}

// get returns the raw value of the first member named key.
func get(ms []member, key string) (json.RawMessage, bool) {
	for _, m := range ms {
		if m.key == key {
			return m.val, true
		}
	}
	return nil, false
}

// getString returns the string value of member key, if it is a string.
func getString(ms []member, key string) (string, bool) {
	raw, ok := get(ms, key)
	if !ok || first(raw) != '"' {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}

// quote encodes s as a JSON string without HTML escaping.
func quote(s string) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // a string always encodes
	return bytes.TrimSuffix(b.Bytes(), []byte("\n"))
}

func encodeObject(ms []member) []byte {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range ms {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(quote(m.key))
		b.WriteByte(':')
		b.Write(m.val)
	}
	b.WriteByte('}')
	return b.Bytes()
}

func encodeArray(es []json.RawMessage) []byte {
	var b bytes.Buffer
	b.WriteByte('[')
	for i, e := range es {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(e)
	}
	b.WriteByte(']')
	return b.Bytes()
}
