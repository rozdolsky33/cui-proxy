package codex

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestArgumentPlainStringEmitsBeforeClosingQuote(t *testing.T) {
	e := newEnv(t)
	st := &argumentStream{r: e.r}
	first, err := st.Write(`{"command":"printf hello `)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first, "printf hello ") {
		t.Fatal("ordinary argument text was held until the closing quote")
	}
	last, err := st.Write(`world"}`)
	if err != nil {
		t.Fatal(err)
	}
	tail, err := st.Finish()
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]string
	if err := json.Unmarshal([]byte(first+last+tail), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["command"] != "printf hello world" {
		t.Fatal("streamed argument changed its meaning")
	}
}

func TestArgumentKeyEmitsBeforeClosingQuote(t *testing.T) {
	for _, key := range []string{"reference", "exit_code", "[ordinary key"} {
		e := newEnv(t)
		st := &argumentStream{r: e.r}
		first, err := st.Write(`{"` + key[:len(key)-1])
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(first, key[:len(key)-1]) {
			t.Fatal("ordinary argument key was held until its closing quote")
		}
		last, err := st.Write(key[len(key)-1:] + `":"value"}`)
		if err != nil {
			t.Fatal(err)
		}
		tail, err := st.Finish()
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]string
		if json.Unmarshal([]byte(first+last+tail), &decoded) != nil || decoded[key] != "value" {
			t.Fatal("streamed argument key changed its meaning")
		}
	}
}

func TestArgumentWhitespaceClassificationAllocationsStayLinear(t *testing.T) {
	e := newEnv(t)
	const size = 128 << 10
	chunk := strings.Repeat(" ", 1024)
	result := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			st := &argumentStream{r: e.r}
			if _, err := st.Write(`{"command":"`); err != nil {
				b.Fatal(err)
			}
			for offset := 0; offset < size; offset += len(chunk) {
				if _, err := st.Write(chunk); err != nil {
					b.Fatal(err)
				}
			}
			if _, err := st.Write(`ordinary"}`); err != nil {
				b.Fatal(err)
			}
			if _, err := st.Finish(); err != nil {
				b.Fatal(err)
			}
		}
	})
	if result.AllocedBytesPerOp() > 32*size {
		t.Fatal("leading whitespace causes excessive rescanning allocations")
	}
}

func TestArgumentStringRestorationEverySplit(t *testing.T) {
	e := newEnv(t)
	original := "restored \"line\"\nwith a slash \\ and snowman ☃"
	synthetic, err := e.v.Pseudonym("secret", original)
	if err != nil {
		t.Fatal(err)
	}
	nested, err := json.Marshal(map[string]string{"value": "before " + synthetic + " after"})
	if err != nil {
		t.Fatal(err)
	}
	cases := []any{
		map[string]string{"command": "printf before " + synthetic + " after"},
		map[string]string{"command": "  " + string(nested)},
		map[string]string{"command": "{invalid nested " + synthetic},
		map[string]string{"key " + synthetic: "ordinary value"},
	}
	for index, value := range cases {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		input := string(encoded)
		// Exercise restoration after decoding Unicode escapes as well as raw text.
		var escaped strings.Builder
		for _, r := range synthetic {
			fmt.Fprintf(&escaped, `\u%04x`, r)
		}
		for _, spelling := range []string{input, strings.ReplaceAll(input, synthetic, escaped.String())} {
			want, err := restoreArgumentDocument(spelling, e.r)
			if err != nil {
				t.Fatal(err)
			}
			var expected any
			if err := json.Unmarshal([]byte(want), &expected); err != nil {
				t.Fatal(err)
			}
			check := func(parts []string) {
				t.Helper()
				st := &argumentStream{r: e.r}
				var result strings.Builder
				for _, part := range parts {
					out, err := st.Write(part)
					if err != nil {
						t.Fatalf("case %d: incremental restoration failed", index)
					}
					result.WriteString(out)
				}
				tail, err := st.Finish()
				if err != nil {
					t.Fatalf("case %d: final validation failed", index)
				}
				result.WriteString(tail)
				var actual any
				if err := json.Unmarshal([]byte(result.String()), &actual); err != nil {
					t.Fatalf("case %d: restored document is invalid", index)
				}
				a, _ := json.Marshal(actual)
				b, _ := json.Marshal(expected)
				if string(a) != string(b) {
					t.Fatalf("case %d: restored document changed with chunking", index)
				}
			}
			for split := 0; split <= len(spelling); split++ {
				check([]string{spelling[:split], spelling[split:]})
			}
			parts := make([]string, len(spelling))
			for i := 0; i < len(spelling); i++ {
				parts[i] = spelling[i : i+1]
			}
			check(parts)
		}
	}
}

func FuzzArgumentStringPrefixDecoding(f *testing.F) {
	for _, seed := range []string{"ordinary text", `plain \u0061 \/ \n`, `\uD83D\uDE00`, `\uD800 x \uDC00`, "snowman ☃"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		var expected string
		if json.Unmarshal([]byte(`"`+raw+`"`), &expected) != nil {
			return
		}
		var actual strings.Builder
		pending := ""
		for i := 0; i < len(raw); i++ {
			pending += raw[i : i+1]
			text, _, remaining, err := decodeArgumentPrefix(pending, false)
			if err != nil {
				t.Fatal("valid JSON string prefix rejected")
			}
			actual.WriteString(text)
			pending = remaining
		}
		text, _, remaining, err := decodeArgumentPrefix(pending, true)
		if err != nil || remaining != "" {
			t.Fatal("valid final JSON string rejected")
		}
		actual.WriteString(text)
		if actual.String() != expected {
			t.Fatal("incremental decoding differs from encoding/json")
		}
	})
}

func TestArgumentStreamPreservesUnchangedEscapesEverySplit(t *testing.T) {
	e := newEnv(t)
	input := `{"command":"plain \u0061 \/ \n \" \\ \uD83D\uDE00 \uD800 end"}`
	for split := 0; split <= len(input); split++ {
		st := &argumentStream{r: e.r}
		a, err := st.Write(input[:split])
		if err != nil {
			t.Fatal(err)
		}
		b, err := st.Write(input[split:])
		if err != nil {
			t.Fatal(err)
		}
		c, err := st.Finish()
		if err != nil {
			t.Fatal(err)
		}
		if a+b+c != input {
			t.Fatalf("unchanged spelling changed at split %d", split)
		}
	}
}
