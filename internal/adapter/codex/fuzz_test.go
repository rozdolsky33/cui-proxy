package codex

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"cui-proxy/internal/config"
	"cui-proxy/internal/detect"
	"cui-proxy/internal/sse"
	"cui-proxy/internal/transform"
	"cui-proxy/internal/vault"
)

// fuzzReals are seeded real values. text is what is inserted; real is the
// part that must never reach the sanitized request.
var fuzzReals = []struct{ text, real string }{
	{"10.20.30.15", "10.20.30.15"},
	{"ocid1.instance.oc1.phx.anyhqljrabcdefghijklmnop", "ocid1.instance.oc1.phx.anyhqljrabcdefghijklmnop"},
	{"alice.smith@corp-internal.com", "alice.smith@corp-internal.com"},
	{"512-34-5678", "512-34-5678"},
	{"4111 1111 1111 1111", "4111 1111 1111 1111"},
	{"fd00:1234:5678::1", "fd00:1234:5678::1"},
	{"Bearer sk-live-SEEDED0123456789abcdef", "sk-live-SEEDED0123456789abcdef"},
}

// fuzzSeps keep a seed a standalone token (a seed glued to random letters
// is, by design, not a detectable value).
const fuzzSeps = " \t\n\"'=,;()[]{}<>|"

// FuzzSanitizeRequest feeds arbitrary bytes to the request sanitizer. It
// must never panic, and must either fail (the proxy blocks) or return valid
// JSON. Then the same bytes, if they are a JSON object, get seeded values
// added as a user message text, an unknown top-level key and that key's
// string value: a successful result must contain none of the seeds that the
// original bytes did not already hold.
func FuzzSanitizeRequest(f *testing.F) {
	f.Add(readFixture(f, "request_turn1.json"), "x", "y", uint8(0), uint8(1), uint8(0))
	f.Add(readFixture(f, "request_turn2.json"), "", "", uint8(2), uint8(3), uint8(1))
	f.Add([]byte(`{}`), "", "", uint8(4), uint8(5), uint8(2))
	f.Add([]byte(`{"input":"10.20.30.15","tools":[{"type":"function","name":"10.20.30.15"}]}`), "a", "b", uint8(6), uint8(0), uint8(3))
	f.Add([]byte(`{"a":"{\"b\":[\"ocid1.instance.oc1.phx.anyhqljrabcdefghijklmnop\"]}"}`), "{", "}", uint8(1), uint8(2), uint8(4))
	f.Add([]byte(`{"n":4111111111111111,"k":1e999}`), "", "", uint8(4), uint8(4), uint8(5))
	f.Add([]byte(`[1,2]`), "", "", uint8(0), uint8(0), uint8(0))
	f.Add([]byte(`{"a":"\u0031\u0030.20.30.15"}`), "", "", uint8(0), uint8(0), uint8(0))
	f.Add([]byte("\xff{}"), "", "", uint8(0), uint8(0), uint8(0))
	ds, err := detect.Build([]config.Rule{{Name: "builtin", Type: "builtin"}}, "")
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, data []byte, r1, r2 string, a, b, sep uint8) {
		// A fresh vault per input: a shared one would fill the small IPv4
		// synthetic pool and turn most inputs into (allowed) errors.
		san := transform.NewSanitizer(ds, vault.New(testKey), nil)
		out, err := sanitizeRequest(data, san)
		if err == nil && !json.Valid(out) {
			t.Fatalf("sanitized request is not valid JSON:\n in %q\nout %q", data, out)
		}

		doc := map[string]any{}
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		if dec.Decode(&doc) != nil || dec.More() {
			doc = map[string]any{}
		}
		before, err := json.Marshal(doc)
		if err != nil {
			return
		}
		sa, sb := fuzzReals[int(a)%len(fuzzReals)], fuzzReals[int(b)%len(fuzzReals)]
		sp := string(fuzzSeps[int(sep)%len(fuzzSeps)])
		doc["input"] = []any{map[string]any{
			"type": "message", "role": "user",
			"content": []any{map[string]any{"type": "input_text", "text": r1 + sp + sa.text + sp + r2}},
		}}
		doc["x"+sp+sb.text+sp+r1] = r2 + sp + sa.text + sp + "q"
		seeded, err := json.Marshal(doc)
		if err != nil {
			return
		}
		out, err = sanitizeRequest(seeded, san)
		if err != nil {
			return // blocked: allowed
		}
		if !json.Valid(out) {
			t.Fatalf("sanitized seeded request is not valid JSON: %q", out)
		}
		// Compare decoded text: JSON escapes cannot hide a leak.
		var got any
		d := json.NewDecoder(bytes.NewReader(out))
		d.UseNumber()
		if err := d.Decode(&got); err != nil {
			t.Fatal(err)
		}
		canon, _ := json.Marshal(got)
		for _, s := range []string{sa.real, sb.real} {
			if bytes.Contains(before, []byte(s)) || strings.Contains(r1+"\x00"+r2, s) {
				continue // present in the fuzzed part itself, possibly at an opaque position
			}
			if bytes.Contains(canon, []byte(s)) || bytes.Contains(out, []byte(s)) {
				t.Fatalf("seed %q leaked:\n in %q\nout %q", s, seeded, out)
			}
		}
	})
}

// FuzzSSERewrite feeds an arbitrary byte stream through the SSE reader and
// the Codex response rewriter (with synthetics in the vault, so restores
// happen). It must never panic; errors are allowed. Every emitted event is
// either byte-identical to an input event's data or valid JSON.
func FuzzSSERewrite(f *testing.F) {
	e := newEnv(f)
	syn := e.pseudo(f, "ipv4", "10.20.30.15")
	host := e.pseudo(f, "hostname", "db01.prod.agency.internal")
	raw := strings.NewReplacer("{{A1}}", syn[:5], "{{A2}}", syn[5:], "{{IP}}", syn).Replace(string(readFixture(f, "stream_toolcall.sse")))
	f.Add([]byte(raw))
	f.Add([]byte("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"item_id\":\"m\",\"output_index\":0,\"content_index\":0,\"delta\":\"" + host[:3] + "\"}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"item_id\":\"m\",\"output_index\":0,\"content_index\":0,\"delta\":\"" + host[3:] + "\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"" + host + "\"}]}]}}\n\n"))
	f.Add([]byte("data: {\"type\":\"response.function_call_arguments.delta\",\"item_id\":\"f\",\"output_index\":0,\"delta\":\"{\\\"cmd\\\":\\\"" + syn + "\\\"}\"}\n\n"))
	f.Add([]byte("data: {\"type\":\"response.reasoning_summary_text.delta\",\"item_id\":\"r\",\"output_index\":0,\"summary_index\":0,\"delta\":\"" + syn + "\"}\n\ndata: {\"type\":\"error\"}\n\n"))
	f.Add([]byte("data: not json " + syn + "\n\n: comment\n\ndata: {\"type\":\"x\",\"a\":[[[[[[]]]]]]}\n\n"))
	f.Add([]byte("\xef\xbb\xbfdata: {\"type\":\"response.output_text.delta\",\"delta\":\"\\ud800\"}\r\n\r\n"))
	f.Fuzz(func(t *testing.T, stream []byte) {
		rw := New().NewResponseRewriter(e.r)
		rd := sse.NewReader(bytes.NewReader(stream))
		var inputs [][]byte
		check := func(evs []sse.Event) {
			for _, ev := range evs {
				if json.Valid(ev.Data) {
					continue
				}
				known := false
				for _, in := range inputs {
					if bytes.Equal(in, ev.Data) {
						known = true
						break
					}
				}
				if !known {
					t.Fatalf("rewriter emitted invalid, new event data %q", ev.Data)
				}
				var buf bytes.Buffer
				_ = sse.Write(&buf, ev)
			}
		}
		for {
			ev, err := rd.Next()
			if errors.Is(err, io.EOF) {
				check(rw.Flush())
				return
			}
			if err != nil {
				return
			}
			inputs = append(inputs, ev.Data)
			evs, err := rw.Event(ev)
			if err != nil {
				return // the proxy cuts the stream: allowed
			}
			check(evs)
		}
	})
}
