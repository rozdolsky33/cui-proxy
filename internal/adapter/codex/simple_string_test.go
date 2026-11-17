package codex

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeSimpleStringMatchesStandard(t *testing.T) {
	for _, value := range []string{"", "plain", "line\nnext", "tab\tquote\"slash\\", "\b\f\r", "café\n日本語", strings.Repeat("line\n", 4096)} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal("encode failed")
		}
		got, ok := decodeSimpleString(raw[1 : len(raw)-1])
		if !ok || got != value {
			t.Fatal("simple decoding differs")
		}
	}
	got, ok := decodeSimpleString([]byte(`a\/b`))
	if !ok || got != "a/b" {
		t.Fatal("slash decoding differs")
	}
}

func TestDecodeSimpleStringFallsBack(t *testing.T) {
	for _, body := range [][]byte{[]byte(`\u0061`), []byte(`\uD834\uDD1E`), []byte(`\uD800`), {0xff}, []byte(`\x`), []byte(`\`)} {
		if _, ok := decodeSimpleString(body); ok {
			t.Fatal("unsupported decoding accepted")
		}
	}
}

func FuzzDecodeSimpleStringParity(f *testing.F) {
	for _, s := range []string{"plain", "line\nnext", "café", "\"\\\t\r", string([]byte{0xff})} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		raw, err := json.Marshal(s)
		if err != nil {
			t.Fatal("encode failed")
		}
		var want string
		if json.Unmarshal(raw, &want) != nil {
			t.Fatal("oracle failed")
		}
		if got, ok := decodeSimpleString(raw[1 : len(raw)-1]); ok && got != want {
			t.Fatal("decoding differs")
		}
		if got := decodeString(raw, strings.Contains(string(raw), `\`)); got != want {
			t.Fatal("fallback differs")
		}
	})
}
