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
		n := &node{kind: '"', str: s, dirty: true}
		for _, limit := range []int{0, 1, 17, 1048576} {
			if got := n.encodedSizeHint(false, limit); got != min(limit, len(q)) {
				t.Fatal("encoded size differs from standard encoder")
			}
		}
		object := &node{kind: '{', dirty: true, keys: []string{s}, kids: []*node{n}}
		for _, limit := range []int{0, 1, 17, 1048576} {
			if got := object.encodedSizeHint(true, limit); got != min(limit, 3+2*len(q)) {
				t.Fatal("object size differs from standard encoder")
			}
		}
	})
}

func TestEncodedSizeHintMatchesOutputAndCaps(t *testing.T) {
	for _, text := range []string{"plain", "\n\x00\"\\", "\u2028\u2029", "é界", string([]byte{0xff, 0xfe})} {
		for _, canonical := range []bool{false, true} {
			n, err := parse([]byte(` { "key": "old", "items": [true,12,"raw"] } `), 0)
			if err != nil {
				t.Fatal("test document rejected")
			}
			n.dirty = true
			n.get("key").setString(text)
			var expected bytes.Buffer
			n.write(&expected, canonical)
			for _, limit := range []int{0, 1, 2, 7, 64, 1024} {
				got := n.encodedSizeHint(canonical, limit)
				if got != min(limit, expected.Len()) {
					t.Fatalf("size hint %d, expected %d", got, min(limit, expected.Len()))
				}
			}
		}
	}
}

func TestOutputReservationDoesNotUseDiscardedImageSize(t *testing.T) {
	n := &node{kind: '{', raw: make([]byte, 512*1024), dirty: true, keys: []string{"text"}, kids: []*node{{kind: '"', str: "small", dirty: true}}}
	got := n.encode(false)
	if !bytes.Equal(got, []byte(`{"text":"small"}`)) {
		t.Fatal("rewritten output differs")
	}
	if cap(got) > 4096 {
		t.Fatal("reserved discarded input capacity")
	}
}

func TestEncodedSizeHintSaturatesExpandedOutput(t *testing.T) {
	text := strings.Repeat("\x00", 200000)
	n := &node{kind: '{', raw: make([]byte, 512*1024), dirty: true, keys: []string{"text"}, kids: []*node{{kind: '"', str: text, dirty: true}}}
	if n.encodedSizeHint(false, maxEncodeHint) != maxEncodeHint {
		t.Fatal("expanded output did not saturate")
	}
	var expected bytes.Buffer
	n.write(&expected, false)
	if !bytes.Equal(n.encode(false), expected.Bytes()) {
		t.Fatal("saturated fallback changed output")
	}
}
