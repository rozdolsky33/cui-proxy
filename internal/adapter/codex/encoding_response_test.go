package codex

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"cui-proxy/internal/sse"
)

func TestEncodingArgumentsEscapes(t *testing.T) {
	for _, tc := range []struct{ name, prefix, suffix string }{
		{"newline", `\n`, ""},
		{"tab", `\t`, ""},
		{"unicode_space", `\u0020`, ""},
		{"escaped_synthetic", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			real := "10.20.30.15"
			syn := e.pseudo(t, "ipv4", real)
			wireSyn := syn
			if tc.name == "escaped_synthetic" {
				wireSyn = fmt.Sprintf(`\u%04x`, syn[0]) + syn[1:]
			}
			args := `{"cmd":"` + tc.prefix + wireSyn + tc.suffix + `"}`
			b, _ := json.Marshal(map[string]any{"type": "response.function_call_arguments.done", "arguments": args})
			out, err := New().NewResponseRewriter(e.r).Event(sse.Event{Data: b})
			if err != nil {
				t.Fatal(err)
			}
			got := field(t, out[0], "arguments")
			var obj map[string]string
			if err := json.Unmarshal([]byte(got), &obj); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(obj["cmd"], real) {
				t.Fatalf("missed restore: cmd=%q", obj["cmd"])
			}
		})
	}
}

func TestEncodingNestedArgumentsEscaping(t *testing.T) {
	e := newEnv(t)
	real := "line1\nline2"
	syn := e.pseudo(t, "secret", real)
	inner, _ := json.Marshal(map[string]string{"v": syn})
	args, _ := json.Marshal(map[string]string{"cmd": string(inner)})
	b, _ := json.Marshal(map[string]any{"type": "response.function_call_arguments.done", "arguments": string(args)})
	out, err := New().NewResponseRewriter(e.r).Event(sse.Event{Data: b})
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]string
	if err := json.Unmarshal([]byte(field(t, out[0], "arguments")), &obj); err != nil {
		t.Fatal(err)
	}
	var nested map[string]string
	if err := json.Unmarshal([]byte(obj["cmd"]), &nested); err != nil {
		t.Fatalf("nested JSON corrupted: %v; content=%q", err, obj["cmd"])
	}
	if nested["v"] != real {
		t.Fatalf("got %q", nested["v"])
	}
}

func TestEncodingStreamKeyEscapeEquivalence(t *testing.T) {
	e := newEnv(t)
	real := "alice@example.com"
	syn := e.pseudo(t, "email", real)
	q := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	in := []sse.Event{
		{Data: []byte(`{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"delta":` + q(syn[:6]) + `}`)},
		{Data: []byte(`{"type":"response.output_text.delta","item_id":"msg\u005f1","output_index":0.0e0,"delta":` + q(syn[6:]) + `}`)},
	}
	out := feed(t, New().NewResponseRewriter(e.r), in)
	if got := deltas(t, out, "response.output_text.delta"); got != real {
		t.Fatalf("equivalent ids split stream: got %q", got)
	}
}

func argumentEvent(typ, key, value string) sse.Event {
	b, _ := json.Marshal(map[string]string{"type": typ, "item_id": "fc_1", key: value})
	return sse.Event{Data: b}
}

func TestEncodingArgumentEverySplit(t *testing.T) {
	e := newEnv(t)
	real := "line1\n\"quoted\"\\line2\t\u2028"
	syn := e.pseudo(t, "secret", real)
	ip := e.pseudo(t, "ipv4", "10.20.30.15")
	inner, _ := json.Marshal(map[string]string{"value": syn})
	nested, _ := json.Marshal(map[string]string{"nested": string(inner)})
	cases := []string{
		`{"cmd":"\n` + ip + `\t","escaped":"\u` + fmt.Sprintf("%04x", ip[0]) + ip[1:] + `","secret":"` + syn + `"}`,
		string(nested), string(quote(syn)), `{"` + syn + `":"` + syn + `","n":123,"ok":true}`,
	}
	for _, args := range cases {
		want, err := restoreArgumentDocument(args, e.r)
		if err != nil {
			t.Fatal(err)
		}
		for split := 0; split <= len(args); split++ {
			rw := New().NewResponseRewriter(e.r)
			out := feed(t, rw, []sse.Event{argumentEvent("response.function_call_arguments.delta", "delta", args[:split]), argumentEvent("response.function_call_arguments.delta", "delta", args[split:]), argumentEvent("response.function_call_arguments.done", "arguments", args)})
			got := deltas(t, out, "response.function_call_arguments.delta")
			if got != want || !json.Valid([]byte(got)) {
				t.Fatalf("split %d: got %q want %q", split, got, want)
			}
			if strings.Contains(got, syn) || strings.Contains(got, ip) {
				t.Fatal("synthetic survived")
			}
			if field(t, find(t, out, "response.function_call_arguments.done"), "arguments") != want {
				t.Fatal("delta/done disagreement")
			}
		}
	}
}

func TestEncodingArgumentsFailClosed(t *testing.T) {
	e := newEnv(t)
	for _, args := range []string{`{"v":"unfinished`, `{"v":`, `{"v":true,}`, `{} {}`, strings.Repeat("[", maxDepth+1) + strings.Repeat("]", maxDepth+1)} {
		for _, finish := range []string{"eof", "response.function_call_arguments.done", "response.completed"} {
			rw := New().NewResponseRewriter(e.r)
			_, err := rw.Event(argumentEvent("response.function_call_arguments.delta", "delta", args))
			if err == nil {
				if finish == "eof" {
					_, err = rw.Flush()
				} else {
					_, err = rw.Event(argumentEvent(finish, "arguments", `{}`))
				}
			}
			if err == nil {
				t.Fatalf("accepted invalid arguments at %s", finish)
			}
		}
	}
	rw := newRewriter(e.r)
	rw.argumentBytes = maxArgumentBytes - 1
	if _, err := rw.Event(argumentEvent("response.function_call_arguments.delta", "delta", `{}`)); err == nil {
		t.Fatal("argument budget ignored")
	}
}

func TestEncodingNumericAndKeyRestoration(t *testing.T) {
	e := newEnv(t)
	for _, real := range []string{"4111111111111111", "6011000990139424"} {
		syn := e.pseudo(t, "card", real)
		want := real
		if strings.Contains(real, " ") {
			want = string(quote(real))
		}
		for _, doc := range []struct{ in, want string }{{syn, want}, {`{"card":` + syn + `}`, `{"card":` + want + `}`}} {
			for split := 0; split <= len(doc.in); split++ {
				out := feed(t, New().NewResponseRewriter(e.r), []sse.Event{argumentEvent("response.function_call_arguments.delta", "delta", doc.in[:split]), argumentEvent("response.function_call_arguments.delta", "delta", doc.in[split:]), argumentEvent("response.function_call_arguments.done", "arguments", doc.in)})
				if got := deltas(t, out, "response.function_call_arguments.delta"); got != doc.want {
					t.Fatalf("numeric split %d: %q want %q", split, got, doc.want)
				}
				if got := field(t, find(t, out, "response.function_call_arguments.done"), "arguments"); got != doc.want {
					t.Fatal("numeric done disagrees")
				}
			}
		}
	}
	real := "line1\nline2"
	syn := e.pseudo(t, "secret", real)
	key := `{"v":"` + syn + `"}`
	doc, _ := json.Marshal(map[string]string{key: "x"})
	expected, _ := json.Marshal(map[string]string{strings.ReplaceAll(key, syn, real): "x"})
	out := feed(t, New().NewResponseRewriter(e.r), []sse.Event{argumentEvent("response.function_call_arguments.delta", "delta", string(doc)), argumentEvent("response.function_call_arguments.done", "arguments", string(doc))})
	if got := deltas(t, out, "response.function_call_arguments.delta"); got != string(expected) {
		t.Fatalf("key changed meaning: %q", got)
	}
	if got := field(t, find(t, out, "response.function_call_arguments.done"), "arguments"); got != string(expected) {
		t.Fatal("key done disagrees")
	}
}

// Mutation exercises the tokenizer independently of the document walker. The
// two paths must produce the same decoded JSON at arbitrary chunk boundaries.
func FuzzArgumentJSON(f *testing.F) {
	e := newEnv(f)
	syn := e.pseudo(f, "secret", "line1\n\"quoted\"\\line2")
	card := e.pseudo(f, "card", "4111111111111111")
	ip := e.pseudo(f, "ipv4", "10.20.30.15")
	for _, doc := range []string{`{"v":"\n` + ip + `","card":` + card + `}`, `{"v":"` + syn + `"}`, `{"{\"v\":\"` + syn + `\"}":"x"}`, `"` + syn + `"`, card, `{"nested":"{\"v\":\"` + syn + `\"}"}`} {
		f.Add(doc, uint16(3))
	}
	f.Fuzz(func(t *testing.T, doc string, split uint16) {
		if len(doc) > 65536 || !json.Valid([]byte(doc)) {
			return
		}
		want, err := restoreArgumentDocument(doc, e.r)
		if err != nil {
			return
		}
		st := argumentStream{r: e.r}
		at := int(split) % (len(doc) + 1)
		a, err := st.Write(doc[:at])
		if err != nil {
			t.Fatal(err)
		}
		b, err := st.Write(doc[at:])
		if err != nil {
			t.Fatal(err)
		}
		tail, err := st.Finish()
		if err != nil {
			t.Fatal(err)
		}
		decode := func(text string) any {
			dec := json.NewDecoder(strings.NewReader(text))
			dec.UseNumber()
			var v any
			if err := dec.Decode(&v); err != nil {
				t.Fatal(err)
			}
			return v
		}
		if !reflect.DeepEqual(decode(a+b+tail), decode(want)) {
			t.Fatalf("stream/document mismatch for %q", doc)
		}
	})
}
