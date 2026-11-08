package codex

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
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

// seedMark is spliced into the decoded document and replaced, in the encoded
// bytes, by a seed (raw or \u-escaped).
const seedMark = "QQSEEDMARKQQ"

// visibleStrings calls emit with every string the sanitizer scans in the
// document n, at every decoded level: object keys (protocol field names
// excepted), string leaves, and the keys and leaves of strings that are
// themselves JSON. Strings at opaque protocol positions (opaqueAt, known
// include values) are skipped: the sanitizer passes them byte-identical by
// design. It mirrors reqWalker's positions, not its sanitizing.
func visibleStrings(n *node, p pos, inner bool, emit func(string)) {
	switch n.kind {
	case '{':
		for i, k := range n.keys {
			if !protocolKeys[k] {
				emit(k)
			}
			kid := n.kids[i]
			if !inner && kid.kind == '"' && opaqueAt(p, k, n, kid) {
				continue
			}
			visibleStrings(kid, childPos(p, k), inner, emit)
		}
	case '[':
		ep := elemPos(p)
		for _, kid := range n.kids {
			if !inner && ep == posInclude && kid.kind == '"' && protocolInclude(kid.str) {
				continue
			}
			visibleStrings(kid, ep, inner, emit)
		}
	case '"':
		emit(n.str)
		if c := first(n.str); (c == '{' || c == '[') && json.Valid([]byte(n.str)) {
			if tree, err := parse([]byte(n.str), 0); err == nil {
				visibleStrings(tree, posOther, true, emit)
			}
		}
	}
}

// leakedSeeds returns the seeds that survive in a sanitized request: a
// detected value at a scanned position, not a pass-through synthetic, that
// contains a seed's real part. (A seed glued to other text is not a
// detectable value by design, so it is not counted.)
func leakedSeeds(t testing.TB, san *transform.Sanitizer, out []byte) []string {
	t.Helper()
	root, err := parse(out, 0)
	if err != nil {
		t.Fatalf("sanitized request does not parse: %v", err)
	}
	var leaks []string
	visibleStrings(root, posTop, false, func(str string) {
		for _, sp := range san.Detect(str) {
			for _, seed := range fuzzReals {
				if strings.Contains(str[sp.Start:sp.End], seed.real) {
					leaks = append(leaks, seed.real)
				}
			}
		}
	})
	return leaks
}

// target is a splice point in a decoded document: a string leaf (in an
// object or an array) or an object key.
type target struct {
	obj   map[string]any
	arr   []any
	key   string
	index int
	isKey bool
}

func targets(v any) []target {
	var out []target
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for _, k := range slices.Sorted(maps.Keys(x)) {
				out = append(out, target{obj: x, key: k, isKey: true})
				if _, ok := x[k].(string); ok {
					out = append(out, target{obj: x, key: k})
				}
				walk(x[k])
			}
		case []any:
			for i, e := range x {
				if _, ok := e.(string); ok {
					out = append(out, target{arr: x, index: i})
				}
				walk(e)
			}
		}
	}
	walk(v)
	return out
}

// splice inserts seedMark into the target. form 0: plain; 1: \u-escaped in
// the encoded bytes (see seedBytes); 2: inside an inner-JSON string that
// replaces the leaf (keys get the plain form).
func (tg target) splice(form int, sp string) {
	if tg.isKey {
		v := tg.obj[tg.key]
		nk := tg.key + sp + seedMark + sp
		if _, dup := tg.obj[nk]; !dup {
			delete(tg.obj, tg.key)
			tg.obj[nk] = v
		}
		return
	}
	var cur string
	if tg.obj != nil {
		cur, _ = tg.obj[tg.key].(string)
	} else {
		cur, _ = tg.arr[tg.index].(string)
	}
	val := cur + sp + seedMark + sp
	if form == 2 {
		val = `{"cmd":"ssh ` + seedMark + ` uptime","args":["` + seedMark + `"]}`
	}
	if tg.obj != nil {
		tg.obj[tg.key] = val
	} else {
		tg.arr[tg.index] = val
	}
}

// seedBytes is the encoded form of the seed for a splice form.
func seedBytes(text string, form int) []byte {
	if form != 1 {
		return []byte(text)
	}
	var b strings.Builder
	for i := 0; i < len(text); i++ {
		fmt.Fprintf(&b, "\\u%04x", text[i])
	}
	return []byte(b.String())
}

// FuzzSanitizeRequest feeds arbitrary bytes to the request sanitizer. It
// must never panic, and must either fail (the proxy blocks) or return valid
// JSON with no seed left at a scanned position. Then the bytes are decoded
// (the turn-1 fixture stands in when they are not a JSON object), a seed is
// spliced into a fuzzer-chosen string leaf or key (plain, \u-escaped, or
// inside an inner-JSON string), and the same oracle applies: no seed may
// survive at any decoded level outside the opaque protocol positions.
func FuzzSanitizeRequest(f *testing.F) {
	f.Add(readFixture(f, "request_turn1.json"), uint16(0), uint8(0), uint8(0), uint8(0))
	f.Add(readFixture(f, "request_turn1.json"), uint16(40), uint8(1), uint8(1), uint8(1))
	f.Add(readFixture(f, "request_turn2.json"), uint16(17), uint8(2), uint8(2), uint8(2))
	f.Add(readFixture(f, "request_turn2.json"), uint16(3), uint8(1), uint8(6), uint8(0))
	f.Add([]byte(`{}`), uint16(0), uint8(0), uint8(4), uint8(5))
	f.Add([]byte(`{"tools":[{"type":"function","name":"exec","description":"ssh ops@10.20.30.16","parameters":{"type":"object","properties":{"host":{"type":"string","default":"10.20.30.16"}}}}]}`), uint16(2), uint8(0), uint8(5), uint8(0))
	f.Add([]byte(`{"input":[{"type":"function_call","name":"exec","call_id":"c1","arguments":"{\"cmd\":\"ping 10.20.30.15\"}"}]}`), uint16(5), uint8(2), uint8(1), uint8(0))
	f.Add([]byte(`{"a":"{\"b\":[\"ocid1.instance.oc1.phx.anyhqljrabcdefghijklmnop\"]}"}`), uint16(1), uint8(1), uint8(2), uint8(4))
	f.Add([]byte(`{"a":"\u0031\u0030.20.30.15 up"}`), uint16(1), uint8(1), uint8(0), uint8(0))
	f.Add([]byte(`{"n":4111111111111111,"k":1e999}`), uint16(0), uint8(0), uint8(4), uint8(5))
	f.Add([]byte(`[1,2]`), uint16(0), uint8(0), uint8(0), uint8(0))
	f.Add([]byte("\xff{}"), uint16(0), uint8(0), uint8(0), uint8(0))
	ds, err := detect.Build([]config.Rule{{Name: "builtin", Type: "builtin"}}, "")
	if err != nil {
		f.Fatal(err)
	}
	base := readFixture(f, "request_turn1.json")
	f.Fuzz(func(t *testing.T, data []byte, pick uint16, form, seed, sep uint8) {
		// A fresh vault per input: a shared one would fill the IPv4
		// synthetic pool and turn most inputs into (allowed) errors.
		san := transform.NewSanitizer(ds, vault.New(testKey), nil)
		out, err := sanitizeRequest(data, san)
		if err == nil {
			if !json.Valid(out) {
				t.Fatalf("sanitized request is not valid JSON:\n in %q\nout %q", data, out)
			}
			if l := leakedSeeds(t, san, out); len(l) > 0 {
				t.Fatalf("seeds %q survive:\n in %q\nout %q", l, data, out)
			}
		}

		if bytes.Contains(data, []byte(seedMark)) {
			return
		}
		doc := map[string]any{}
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		if dec.Decode(&doc) != nil || dec.More() {
			doc = map[string]any{}
			dec = json.NewDecoder(bytes.NewReader(base))
			dec.UseNumber()
			if err := dec.Decode(&doc); err != nil {
				t.Fatal(err)
			}
		}
		tgs := targets(doc)
		if len(tgs) == 0 {
			return
		}
		fm := int(form) % 3
		sp := string(fuzzSeps[int(sep)%len(fuzzSeps)])
		tgs[int(pick)%len(tgs)].splice(fm, sp)
		enc, err := json.Marshal(doc)
		if err != nil {
			return
		}
		sd := fuzzReals[int(seed)%len(fuzzReals)]
		seeded := bytes.ReplaceAll(enc, []byte(seedMark), seedBytes(sd.text, fm))
		if !json.Valid(seeded) {
			t.Fatalf("test bug: seeded document is not JSON: %q", seeded)
		}
		out, err = sanitizeRequest(seeded, san)
		if err != nil {
			return // blocked: allowed
		}
		if !json.Valid(out) {
			t.Fatalf("sanitized seeded request is not valid JSON: %q", out)
		}
		if l := leakedSeeds(t, san, out); len(l) > 0 {
			t.Fatalf("seeds %q survive:\n in %q\nout %q", l, seeded, out)
		}
	})
}

// TestFuzzOracleCatchesSkippedTools checks the oracle itself: a sanitizer
// that skips the tools subtree must be caught on a tools input, at every
// splice form, while the real sanitizer passes.
func TestFuzzOracleCatchesSkippedTools(t *testing.T) {
	ds, err := detect.Build([]config.Rule{{Name: "builtin", Type: "builtin"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	// skipTools sanitizes everything but top-level tools, which it puts back
	// unchanged (the bug the oracle must catch).
	skipTools := func(body []byte, s *transform.Sanitizer) ([]byte, error) {
		var doc map[string]json.RawMessage
		if err := json.Unmarshal(body, &doc); err != nil {
			return nil, err
		}
		tools := doc["tools"]
		delete(doc, "tools")
		rest, _ := json.Marshal(doc)
		out, err := sanitizeRequest(rest, s)
		if err != nil {
			return nil, err
		}
		var res map[string]json.RawMessage
		if err := json.Unmarshal(out, &res); err != nil {
			return nil, err
		}
		if tools != nil {
			res["tools"] = tools
		}
		return json.Marshal(res)
	}
	for name, in := range map[string]string{
		"plain":   `{"model":"m","tools":[{"type":"function","name":"exec","description":"ssh ops@10.20.30.15 now"}]}`,
		"escaped": `{"model":"m","tools":[{"type":"function","name":"exec","description":"ssh ops@\u0031\u0030.20.30.15 now"}]}`,
		"inner":   `{"model":"m","tools":[{"type":"function","name":"exec","description":"{\"host\":\"10.20.30.15\"}"}]}`,
		"key":     `{"model":"m","tools":[{"type":"function","name":"exec","parameters":{"properties":{"10.20.30.15":{"type":"string"}}}}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			san := transform.NewSanitizer(ds, vault.New(testKey), nil)
			bad, err := skipTools([]byte(in), san)
			if err != nil {
				t.Fatal(err)
			}
			if len(leakedSeeds(t, san, bad)) == 0 {
				t.Fatal("oracle missed a seed left in tools by a sanitizer that skips tools")
			}
			good, err := sanitizeRequest([]byte(in), san)
			if err != nil {
				t.Fatal(err)
			}
			if l := leakedSeeds(t, san, good); len(l) > 0 {
				t.Fatalf("oracle flags the real sanitizer: %q in %s", l, good)
			}
		})
	}
	// An opaque position is exempt: the model name passes by design.
	san := transform.NewSanitizer(ds, vault.New(testKey), nil)
	out, err := sanitizeRequest([]byte(`{"model":"10.20.30.15"}`), san)
	if err != nil {
		t.Fatal(err)
	}
	if l := leakedSeeds(t, san, out); len(l) > 0 || !bytes.Contains(out, []byte("10.20.30.15")) {
		t.Fatalf("opaque model handling: leaks %q, out %s", l, out)
	}
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
			}
		}
		for {
			ev, err := rd.Next()
			if errors.Is(err, io.EOF) {
				evs, err := rw.Flush()
				if err == nil {
					check(evs)
				}
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
