package codex

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func referenceQuoteOutput(s string) []byte {
	var b bytes.Buffer
	encoder := json.NewEncoder(&b)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(s)
	return bytes.TrimSuffix(b.Bytes(), []byte("\n"))
}

func TestWriteQuotePreservesPrefixSuffixAndJSONBytes(t *testing.T) {
	cases := []string{"", "plain", "\"\\\n\r\t\b\f\x00\x01\x1f", "<>&", "\u2028\u2029", "é界🙂", string([]byte{0xff, 0xc0, 0x80, 'x'}), strings.Repeat("line\n", 4096)}
	for i, s := range cases {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			var b bytes.Buffer
			b.WriteString("prefix:")
			b.Next(2)
			writeQuote(&b, s)
			b.WriteString(":suffix")
			want := append([]byte("efix:"), referenceQuoteOutput(s)...)
			want = append(want, []byte(":suffix")...)
			if !bytes.Equal(b.Bytes(), want) {
				t.Fatal("direct quote bytes differ")
			}
			if !bytes.Equal(quote(s), referenceQuoteOutput(s)) {
				t.Fatal("quote wrapper differs")
			}
		})
	}
}

func FuzzWriteQuoteMatchesStandardJSON(f *testing.F) {
	for _, s := range []string{"", "line\n", "<>&", "\u2028\u2029", "\x00\xff", "\"\\"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		var b bytes.Buffer
		b.WriteString("[")
		writeQuote(&b, s)
		b.WriteString(",")
		writeQuote(&b, s)
		b.WriteString("]")
		q := referenceQuoteOutput(s)
		want := append([]byte("["), q...)
		want = append(want, ',')
		want = append(want, q...)
		want = append(want, ']')
		if !bytes.Equal(b.Bytes(), want) {
			t.Fatal("direct output differs from standard encoder")
		}
		if !json.Valid(b.Bytes()) {
			t.Fatal("direct output is invalid JSON")
		}
	})
}
