package codex

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"cui-proxy/internal/transform"
)

// argumentUnit relates decoded content to its original JSON spelling. Ordinary
// runs can be split at any decoded byte boundary; an escape stays indivisible.
type argumentUnit struct {
	raw       string
	decoded   int
	identical bool
}

type argumentString struct {
	stream  *transform.Stream
	pending string
	units   []argumentUnit
}

func (st *argumentString) write(text string, units []argumentUnit, final bool) (string, error) {
	st.pending += text
	st.units = append(st.units, units...)
	out := st.stream.Write(text)
	if final {
		out += st.stream.Flush()
	}
	n := len(st.pending) - st.stream.BufferedBytes()
	original := st.pending[:n]
	var raw strings.Builder
	left, index := n, 0
	for left > 0 && index < len(st.units) {
		unit := &st.units[index]
		take := min(left, unit.decoded)
		if take < unit.decoded && !unit.identical {
			return "", fmt.Errorf("codex: argument escape split during restoration")
		}
		if unit.identical {
			raw.WriteString(unit.raw[:take])
			unit.raw = unit.raw[take:]
			unit.decoded -= take
		} else {
			raw.WriteString(unit.raw)
			unit.decoded = 0
		}
		left -= take
		if unit.decoded == 0 {
			*unit = argumentUnit{}
			index++
		}
	}
	if left != 0 {
		return "", fmt.Errorf("codex: argument spelling accounting mismatch")
	}
	st.pending = strings.Clone(st.pending[n:])
	st.units = append([]argumentUnit(nil), st.units[index:]...)
	if out == original {
		return raw.String(), nil
	}
	encoded := quote(out)
	return string(encoded[1 : len(encoded)-1]), nil
}

// decodeArgumentPrefix decodes complete JSON string units only. A split UTF-8
// sequence, escape, or possible surrogate pair stays pending for the next delta.
func decodeArgumentPrefix(raw string, final bool) (string, []argumentUnit, string, error) {
	input := raw
	// Bound temporary spelling metadata even for escape-heavy, near-limit deltas.
	if len(raw) > 32<<10 {
		raw = raw[:32<<10]
		final = false
	}
	var text strings.Builder
	var units []argumentUnit
	i := 0
	for i < len(raw) && len(units) < 1024 {
		start := i
		for i < len(raw) {
			c := raw[i]
			if c < 0x20 || c == '\\' || c == '"' {
				break
			}
			if c < utf8.RuneSelf {
				i++
				continue
			}
			if !utf8.FullRuneInString(raw[i:]) && !final {
				break
			}
			r, size := utf8.DecodeRuneInString(raw[i:])
			if r == utf8.RuneError && size == 1 {
				break
			}
			i += size
		}
		if i > start {
			text.WriteString(raw[start:i])
			units = append(units, argumentUnit{raw[start:i], i - start, true})
			continue
		}
		if raw[i] >= utf8.RuneSelf {
			if !utf8.FullRuneInString(raw[i:]) && !final {
				break
			}
			text.WriteRune(utf8.RuneError)
			units = append(units, argumentUnit{raw[i : i+1], utf8.RuneLen(utf8.RuneError), false})
			i++
			continue
		}
		if raw[i] != '\\' {
			return "", nil, "", fmt.Errorf("codex: invalid argument string")
		}
		decoded, size, err := decodeArgumentEscape(raw[i:], final)
		if err != nil {
			return "", nil, "", err
		}
		if size == 0 {
			break
		}
		text.WriteString(decoded)
		units = append(units, argumentUnit{raw[i : i+size], len(decoded), false})
		i += size
	}
	return text.String(), units, input[i:], nil
}

func decodeArgumentEscape(raw string, final bool) (string, int, error) {
	invalid := func() (string, int, error) {
		return "", 0, fmt.Errorf("codex: invalid argument string escape")
	}
	if len(raw) < 2 {
		if !final {
			return "", 0, nil
		}
		return invalid()
	}
	switch raw[1] {
	case '"', '\\', '/':
		return raw[1:2], 2, nil
	case 'b':
		return "\b", 2, nil
	case 'f':
		return "\f", 2, nil
	case 'n':
		return "\n", 2, nil
	case 'r':
		return "\r", 2, nil
	case 't':
		return "\t", 2, nil
	case 'u':
		if len(raw) < 6 {
			if !final {
				return "", 0, nil
			}
			return invalid()
		}
		value, err := strconv.ParseUint(raw[2:6], 16, 16)
		if err != nil {
			return invalid()
		}
		r := rune(value)
		if r >= 0xD800 && r <= 0xDBFF {
			if !final && (len(raw) == 6 || len(raw) == 7 && raw[6] == '\\' || len(raw) >= 8 && raw[6:8] == `\u` && len(raw) < 12) {
				return "", 0, nil
			}
			if len(raw) >= 12 && raw[6:8] == `\u` {
				low, err := strconv.ParseUint(raw[8:12], 16, 16)
				if err == nil && low >= 0xDC00 && low <= 0xDFFF {
					return string(utf16.DecodeRune(r, rune(low))), 12, nil
				}
			}
			return string(utf8.RuneError), 6, nil
		}
		if r >= 0xDC00 && r <= 0xDFFF {
			r = utf8.RuneError
		}
		return string(r), 6, nil
	default:
		return invalid()
	}
}
