package codex

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"cui-proxy/internal/adapter"
	"cui-proxy/internal/config"
	"cui-proxy/internal/detect"
	"cui-proxy/internal/sse"
	"cui-proxy/internal/transform"
	"cui-proxy/internal/vault"
)

var testKey = []byte("0123456789abcdef0123456789abcdef")

type env struct {
	v *vault.Vault
	s *transform.Sanitizer
	r *transform.Restorer
}

func newEnv(t testing.TB) env {
	t.Helper()
	ds, err := detect.Build([]config.Rule{{Name: "builtin", Type: "builtin"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	v := vault.New(testKey)
	return env{v: v, s: transform.NewSanitizer(ds, v, nil), r: transform.NewRestorer(v)}
}

func (e env) pseudo(t testing.TB, class, real string) string {
	t.Helper()
	s, err := e.v.Pseudonym(class, real)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func readFixture(t testing.TB, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/codex/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// strs returns every string leaf of a JSON document, including the leaves of
// strings that are themselves JSON documents, and object keys.
func strs(t testing.TB, doc []byte) []string {
	t.Helper()
	var v any
	if err := json.Unmarshal(doc, &v); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	var out []string
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			out = append(out, x)
			var inner any
			if t := strings.TrimSpace(x); strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[") {
				if json.Unmarshal([]byte(x), &inner) == nil {
					walk(inner)
				}
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		case map[string]any:
			for k, e := range x {
				out = append(out, k)
				walk(e)
			}
		}
	}
	walk(v)
	return out
}

// assertAbsent fails if any real value appears in the raw bytes or in any
// decoded string (at any JSON nesting level). Only fixture literals are named.
func assertAbsent(t *testing.T, doc []byte, reals ...string) {
	t.Helper()
	for _, real := range reals {
		if bytes.Contains(doc, []byte(real)) {
			t.Errorf("real value %q survives in raw body", real)
		}
		for _, s := range strs(t, doc) {
			if strings.Contains(s, real) {
				t.Errorf("real value %q survives in decoded string", real)
				break
			}
		}
	}
}

func mustSanitize(t *testing.T, e env, body []byte) []byte {
	t.Helper()
	out, err := New().SanitizeRequest(body, e.s)
	if err != nil {
		t.Fatalf("SanitizeRequest: %v", err)
	}
	if !json.Valid(out) {
		t.Fatalf("sanitized body is not valid JSON")
	}
	return out
}

// ---- routes and registry ----

func TestMatch(t *testing.T) {
	a := New()
	tests := []struct {
		method, path string
		want         adapter.Route
		ok           bool
	}{
		{"GET", "/v1/models", adapter.Route{Kind: adapter.KindPassthrough, UpstreamPath: "/models"}, true},
		{"POST", "/v1/responses", adapter.Route{Kind: adapter.KindSanitize, UpstreamPath: "/responses"}, true},
		{"POST", "/v1/models", adapter.Route{}, false},
		{"GET", "/v1/responses", adapter.Route{}, false},
		{"POST", "/v1/responses/compact", adapter.Route{}, false},
		{"POST", "/responses", adapter.Route{}, false},
		{"POST", "/v1/responses/", adapter.Route{}, false},
		{"POST", "/v1/../v1/responses", adapter.Route{}, false},
		{"GET", "/v1/models/../responses", adapter.Route{}, false},
		{"GET", "/v1/models?client_version=1", adapter.Route{}, false}, // query is not part of the path
		{"DELETE", "/v1/responses", adapter.Route{}, false},
	}
	for _, tc := range tests {
		got, ok := a.Match(tc.method, tc.path)
		if ok != tc.ok || got != tc.want {
			t.Errorf("Match(%s %s) = %+v, %v; want %+v, %v", tc.method, tc.path, got, ok, tc.want, tc.ok)
		}
	}
	if a.Name() != "codex" {
		t.Errorf("Name = %q", a.Name())
	}
}

func TestRegistered(t *testing.T) {
	a, ok := adapter.Get("codex")
	if !ok || a.Name() != "codex" {
		t.Fatalf("codex adapter not registered")
	}
}

// ---- request side ----

func TestSanitizeRequestScansEveryStringLeaf(t *testing.T) {
	e := newEnv(t)
	body := []byte(`{
	  "model": "gpt-5-codex",
	  "input": [
	    {"type":"message","role":"user","content":[{"type":"input_text","text":"ping 10.20.30.15 please"}]},
	    {"type":"function_call","name":"exec_command","call_id":"call_1","arguments":"{\"cmd\":\"ssh 10.20.30.16\"}"},
	    {"type":"function_call_output","call_id":"call_1","output":"connected to 10.20.30.17 as bob@corp-internal.com"},
	    {"type":"function_call_output","call_id":"call_2","output":[{"type":"input_text","text":"host 10.20.30.21"}]},
	    {"type":"custom_tool_call","call_id":"call_3","name":"apply_patch","input":"+ server 10.20.30.22"}
	  ],
	  "tools": [{"type":"function","name":"exec_command","description":"e.g. ssh 10.20.30.18","parameters":{"type":"object","properties":{"cmd":{"type":"string","description":"default 10.20.30.23"}}}}],
	  "instructions": "admin is carol@corp-internal.com",
	  "client_metadata": {"note":"from 10.20.30.19","x-codex-turn-metadata":"{\"agent\":\"ops@10.20.30.20\"}"},
	  "a_new_unknown_field": {"deep":["ocid1.instance.oc1.phx.anyhqljrabcdefghijklmnop"]}
	}`)
	out := mustSanitize(t, e, body)
	assertAbsent(t, out, "10.20.30.15", "10.20.30.16", "10.20.30.17", "10.20.30.18", "10.20.30.19",
		"10.20.30.20", "10.20.30.21", "10.20.30.22", "10.20.30.23",
		"bob@corp-internal.com", "carol@corp-internal.com", "ocid1.instance.oc1.phx.anyhqljrabcdefghijklmnop")
	if got := e.r.Text(string(out)); !strings.Contains(got, "ping 10.20.30.15 please") {
		t.Errorf("sanitized text does not restore to the original")
	}
}

func TestSanitizeTurn1Fixture(t *testing.T) {
	e := newEnv(t)
	out := mustSanitize(t, e, readFixture(t, "request_turn1.json"))
	assertAbsent(t, out, "10.20.30.15", "10.20.30.16", "10.20.30.17",
		"alice.smith@corp-internal.com", "ocid1.instance.oc1.phx.anyhqljrabcdefghijklmnop")
	// Protocol strings are not mangled.
	for _, keep := range []string{`"include":["reasoning.encrypted_content"]`, `"model":"gpt-5-codex"`, `"tool_choice":"auto"`, `"stream":true`} {
		if !bytes.Contains(out, []byte(keep)) {
			t.Errorf("protocol field changed: want %s", keep)
		}
	}
}

func TestOpaqueKeysUntouched(t *testing.T) {
	e := newEnv(t)
	ip := "10.20.30.15"
	opaque := map[string]string{
		"id":                `"msg_` + ip + `"`,
		"call_id":           `"call_` + ip + `"`,
		"item_id":           `"item_` + ip + `"`,
		"encrypted_content": `"gAAAA` + ip + `\u002b=="`,
		"signature":         `"sig ` + ip + `"`,
		"data":              `"QUJD ` + ip + `"`,
		"prompt_cache_key":  `"pck ` + ip + `"`,
		"model":             `"model-` + ip + `"`,
		"type":              `"message"`,
		"role":              `"user"`,
		"status":            `"st ` + ip + `"`,
		"name":              `"fn_` + ip + `"`,
	}
	var b strings.Builder
	b.WriteString(`{"input":[{`)
	for k, v := range opaque {
		b.WriteString(`"` + k + `":` + v + `,`)
	}
	b.WriteString(`"content":[{"type":"input_text","text":"real ` + ip + `"}]}],`)
	b.WriteString(`"prompt_cache_key":` + opaque["prompt_cache_key"] + `,"model":` + opaque["model"] + `}`)
	out := mustSanitize(t, e, []byte(b.String()))
	for k, v := range opaque {
		if !bytes.Contains(out, []byte(`"`+k+`":`+v)) {
			t.Errorf("opaque key %q not byte-identical", k)
		}
	}
	if bytes.Contains(out, []byte(`"real `+ip+`"`)) {
		t.Errorf("non-opaque text was not sanitized")
	}
}

// [RF 5]
func TestEscapedJSONStringsSanitized(t *testing.T) {
	e := newEnv(t)
	for _, args := range []string{
		`"{\"cmd\":\"ping 10.20.30.15\"}"`,
		`"{\"cmd\":\"ping 10.20.30.1\\u0035\"}"`,              // escape inside the inner JSON
		`"{\"cmd\":\"ping 10\\u002e20\\u002e30\\u002e15\"}"`,  // escaped dots: invisible to a raw-text scan
		`"{\"cmd\":\"ping 10.20.30.1\u0035\"}"`,               // escape in the outer JSON
		`"{\"id\":\"10.20.30.15\",\"type\":\"10.20.30.15\"}"`, // inner keys are not opaque
		`"{\"id\":\"10.20.30.1\\u0035\"}"`,                    // inner opaque-named key, escaped value
		`"{\"id\":\"10\\u002e20\\u002e30\\u002e15\"}"`,        // same, under an inner opaque-named key
		`"{\"10.20.30.15\":1}"`,                               // value in an inner key (belt-and-braces pass)
		`"[\"10.20.30.15\", {\"x\": [\"10.20.30.15\"]}]"`,
		`"{\"cmd\": \"ping 10.20.30.15\"} trailing"`, // not JSON: scanned raw
	} {
		body := []byte(`{"input":[{"type":"function_call","name":"exec_command","call_id":"c1","arguments":` + args + `}]}`)
		out := mustSanitize(t, e, body)
		assertAbsent(t, out, "10.20.30.15")
		var doc struct {
			Input []struct{ Arguments string } `json:"input"`
		}
		if err := json.Unmarshal(out, &doc); err != nil {
			t.Fatal(err)
		}
		inner := doc.Input[0].Arguments
		if !strings.HasSuffix(inner, "trailing") && !json.Valid([]byte(inner)) {
			t.Errorf("arguments %s: re-encoded inner JSON is invalid", args)
		}
	}
}

func TestImagePartBlocked(t *testing.T) {
	e := newEnv(t)
	blocked := []string{
		`{"input":[{"type":"message","role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,AAAA"}]}]}`,
		`{"input":[{"type":"message","role":"user","content":[{"type":"input_image","file_id":"file-1"}]}]}`,
		`{"input":[{"type":"message","role":"user","content":[{"type":"input_file","filename":"a.pdf","file_data":"AAAA"}]}]}`,
		`{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"x","image_url":"https://example.com/a.png"}]}]}`,
		`{"input":[{"type":"function_call_output","call_id":"c","output":[{"type":"input_image","image_url":"data:,"}]}]}`,
		`{"input":[{"type":"image_generation_call","id":"ig_1","result":"AAAA"}]}`,
		`{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"x","file_id":"file-2"}]}]}`,
		`{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"x","type":"input_audio","input_audio":{"data":"AAAA"}}]}]}`, // duplicate key
	}
	for _, body := range blocked {
		out, err := New().SanitizeRequest([]byte(body), e.s)
		if err == nil || out != nil {
			t.Errorf("not blocked: %s", body)
		}
	}
	// Tool and output-format definitions describe, not carry, data.
	allowed := []string{
		`{"tools":[{"type":"function","name":"view_image","description":"View a local image file","parameters":{"type":"object","properties":{"file_id":{"type":"string"},"image_url":{"type":"string"}}}},{"type":"image_generation"}],"input":[]}`,
		`{"text":{"format":{"type":"json_schema","schema":{"properties":{"file_id":{"type":"string"}}}}},"input":[]}`,
		`{"input":[{"type":"function_call","call_id":"c","name":"f","arguments":"{\"type\":\"input_image\",\"image_url\":\"x\"}"}]}`,
	}
	for _, body := range allowed {
		if _, err := New().SanitizeRequest([]byte(body), e.s); err != nil {
			t.Errorf("blocked: %s: %v", body, err)
		}
	}
}

func TestNonJSONBodyBlocked(t *testing.T) {
	e := newEnv(t)
	for _, body := range []string{
		"", "   ", "not json", `{"input":`, `{"a":1} {"b":2}`, `{"a":1} x`, `[{"a":1}]`, `"str"`, `null`,
		"{\"a\":\"\xff\xfe\"}", // invalid UTF-8
	} {
		out, err := New().SanitizeRequest([]byte(body), e.s)
		if err == nil || out != nil {
			t.Errorf("not blocked: %q", body)
		}
	}
}

// R9: a sanitizer error propagates; no partial bytes, no value in the message.
func TestSanitizerErrorBlocks(t *testing.T) {
	e := newEnv(t)
	body := []byte(`{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"ok 10.20.30.15"},{"type":"input_text","text":"id x123-45-6789y now"}]}]}`)
	out, err := New().SanitizeRequest(body, e.s)
	if out != nil || !errors.Is(err, transform.ErrUnrestorable) {
		t.Fatalf("out=%v err=%v; want nil, ErrUnrestorable", out != nil, err)
	}
	if strings.Contains(err.Error(), "6789") || strings.Contains(err.Error(), "10.20.30.15") {
		t.Errorf("error message contains a value: %q", err)
	}
}

func TestUnchangedBodyIsByteIdentical(t *testing.T) {
	e := newEnv(t)
	body := []byte("{\n  \"model\": \"gpt-5-codex\",\n  \"input\": [{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"input_text\",\"text\":\"say hi \\u00e9\"}]}],\n  \"n\": 1.50e3\n}")
	out := mustSanitize(t, e, body)
	if !bytes.Equal(out, body) {
		t.Errorf("unchanged body was rewritten:\n%s", out)
	}
}

func TestTurn2RoundTripStable(t *testing.T) {
	e := newEnv(t)
	body := readFixture(t, "request_turn2.json")
	reals := []string{"10.20.30.15", "fd00:1234:5678::1", "alice.smith@corp-internal.com"}
	out1 := mustSanitize(t, e, body)
	out2 := mustSanitize(t, e, body)
	if !bytes.Equal(out1, out2) {
		t.Errorf("sanitizing the same body twice differs")
	}
	assertAbsent(t, out1, reals...)
	// Sanitizing sanitized output is a no-op (history arrives in restored or
	// synthetic form across turns).
	if again := mustSanitize(t, e, out1); !bytes.Equal(again, out1) {
		t.Errorf("sanitize is not idempotent")
	}
	// Opaque reasoning content stays byte-identical.
	if !bytes.Contains(out1, []byte(`"gAAAAB-fixture-opaque-blob-zz=="`)) {
		t.Errorf("encrypted_content changed")
	}
	// The restored-then-resent history round-trips.
	restored := e.r.Text(string(out1))
	for _, real := range reals {
		if !strings.Contains(restored, real) {
			t.Errorf("%q not restored", real)
		}
	}
}

// ---- response side ----

func ev(name, data string) sse.Event { return sse.Event{Name: name, Data: []byte(data)} }

func feed(t *testing.T, rw adapter.ResponseRewriter, in []sse.Event) []sse.Event {
	t.Helper()
	var out []sse.Event
	for _, e := range in {
		got, err := rw.Event(e)
		if err != nil {
			t.Fatalf("Event(%s): %v", e.Name, err)
		}
		out = append(out, got...)
	}
	return append(out, rw.Flush()...)
}

func field(t *testing.T, e sse.Event, path ...string) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(e.Data, &v); err != nil {
		t.Fatalf("event %s: invalid JSON: %v", e.Name, err)
	}
	for _, p := range path {
		switch x := v.(type) {
		case map[string]any:
			v = x[p]
		case []any:
			v = x[0]
			if m, ok := v.(map[string]any); ok {
				v = m[p]
			}
		}
	}
	s, _ := v.(string)
	return s
}

func typeOf(t *testing.T, e sse.Event) string { return field(t, e, "type") }

func deltas(t *testing.T, evs []sse.Event, typ string) string {
	var b strings.Builder
	for _, e := range evs {
		if typeOf(t, e) == typ {
			b.WriteString(field(t, e, "delta"))
		}
	}
	return b.String()
}

func find(t *testing.T, evs []sse.Event, typ string) sse.Event {
	t.Helper()
	for _, e := range evs {
		if typeOf(t, e) == typ {
			return e
		}
	}
	t.Fatalf("no %s event", typ)
	return sse.Event{}
}

func readSSE(t *testing.T, raw string) []sse.Event {
	t.Helper()
	r := sse.NewReader(strings.NewReader(raw))
	var out []sse.Event
	for {
		e, err := r.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
}

func assertNoSynthetic(t *testing.T, evs []sse.Event, syn string) {
	t.Helper()
	for _, e := range evs {
		if strings.Contains(string(e.Data), syn) {
			t.Errorf("synthetic survives in %s", typeOf(t, e))
		}
	}
}

// [RF 2]
func TestToolCallArgumentsRestoredAcrossDeltas(t *testing.T) {
	e := newEnv(t)
	syn := e.pseudo(t, "ipv4", "10.20.30.15")
	raw := strings.NewReplacer("{{A1}}", syn[:5], "{{A2}}", syn[5:], "{{IP}}", syn).Replace(string(readFixture(t, "stream_toolcall.sse")))
	out := feed(t, New().NewResponseRewriter(e.r), readSSE(t, raw))

	want := `{"cmd":"echo host=10.20.30.15 done"}`
	if got := deltas(t, out, "response.function_call_arguments.delta"); got != want {
		t.Errorf("concatenated deltas = %q, want %q", got, want)
	}
	if got := field(t, find(t, out, "response.output_item.done"), "item", "arguments"); got != want {
		t.Errorf("output_item.done arguments = %q", got)
	}
	if got := field(t, find(t, out, "response.function_call_arguments.done"), "arguments"); got != want {
		t.Errorf("arguments.done = %q", got)
	}
	if got := field(t, find(t, out, "response.completed"), "response", "output", "arguments"); got != want {
		t.Errorf("completed output arguments = %q", got)
	}
	assertNoSynthetic(t, out, syn)
	// Every delta precedes the done event; other delta fields survive.
	seenDone := false
	for _, ev := range out {
		switch typeOf(t, ev) {
		case "response.function_call_arguments.done":
			seenDone = true
		case "response.function_call_arguments.delta":
			if seenDone {
				t.Errorf("delta after done")
			}
			if field(t, ev, "item_id") != "fc_fixture_0002" || field(t, ev, "obfuscation") == "" {
				t.Errorf("delta lost fields: %s", ev.Data)
			}
			if ev.Name != "response.function_call_arguments.delta" {
				t.Errorf("event name changed: %q", ev.Name)
			}
		}
	}
}

// R4: a real value with JSON-special characters keeps arguments valid JSON.
func TestArgumentsRealValueJSONEscaped(t *testing.T) {
	e := newEnv(t)
	real := "-----BEGIN PRIVATE KEY-----\nab\"c\\d\n-----END PRIVATE KEY-----"
	syn := e.pseudo(t, "secret", real)
	enc := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	args := `{"cmd":"echo ` + syn + ` > k"}`
	in := []sse.Event{
		ev("response.function_call_arguments.delta", `{"type":"response.function_call_arguments.delta","item_id":"fc_1","output_index":0,"delta":`+enc(args[:15])+`}`),
		ev("response.function_call_arguments.delta", `{"type":"response.function_call_arguments.delta","item_id":"fc_1","output_index":0,"delta":`+enc(args[15:])+`}`),
		ev("response.output_item.done", `{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"c","name":"exec_command","arguments":`+enc(args)+`}}`),
	}
	out := feed(t, New().NewResponseRewriter(e.r), in)
	for name, got := range map[string]string{
		"deltas":    deltas(t, out, "response.function_call_arguments.delta"),
		"item.done": field(t, find(t, out, "response.output_item.done"), "item", "arguments"),
	} {
		var obj map[string]string
		if err := json.Unmarshal([]byte(got), &obj); err != nil {
			t.Errorf("%s: arguments are not valid JSON: %v", name, err)
			continue
		}
		if obj["cmd"] != "echo "+real+" > k" {
			t.Errorf("%s: cmd = %q", name, obj["cmd"])
		}
	}
}

func TestOutputTextDeltaRestored(t *testing.T) {
	e := newEnv(t)
	syn := e.pseudo(t, "ipv4", "10.20.30.15")
	enc := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	in := []sse.Event{
		ev("response.output_text.delta", `{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":`+enc("Host "+syn[:4])+`}`),
		ev("response.output_text.delta", `{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":`+enc(syn[4:]+" is up.")+`}`),
		ev("response.output_text.done", `{"type":"response.output_text.done","item_id":"msg_1","output_index":0,"content_index":0,"text":`+enc("Host "+syn+" is up.")+`}`),
		ev("response.content_part.done", `{"type":"response.content_part.done","item_id":"msg_1","output_index":0,"content_index":0,"part":{"type":"output_text","text":`+enc("Host "+syn+" is up.")+`,"annotations":[]}}`),
		ev("response.output_item.done", `{"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":`+enc("Host "+syn+" is up.")+`}]}}`),
	}
	out := feed(t, New().NewResponseRewriter(e.r), in)
	want := "Host 10.20.30.15 is up."
	if got := deltas(t, out, "response.output_text.delta"); got != want {
		t.Errorf("deltas = %q", got)
	}
	if got := field(t, find(t, out, "response.output_text.done"), "text"); got != want {
		t.Errorf("output_text.done = %q", got)
	}
	if got := field(t, find(t, out, "response.content_part.done"), "part", "text"); got != want {
		t.Errorf("content_part.done = %q", got)
	}
	if got := field(t, find(t, out, "response.output_item.done"), "item", "content", "text"); got != want {
		t.Errorf("output_item.done = %q", got)
	}
	assertNoSynthetic(t, out, syn)
}

// R1: reasoning summaries are restored; every other reasoning field and
// encrypted_content pass through byte-identical.
func TestReasoningEventsPassThrough(t *testing.T) {
	e := newEnv(t)
	syn := e.pseudo(t, "ipv4", "10.20.30.15")
	enc := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	added := ev("response.output_item.added", `{"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"gAAAA`+syn+`=="}}`)
	partAdded := ev("response.reasoning_summary_part.added", `{"type":"response.reasoning_summary_part.added","item_id":"rs_1","output_index":0,"summary_index":0,"part":{"type":"summary_text","text":""}}`)
	rawText := ev("response.reasoning_text.delta", `{"type":"response.reasoning_text.delta","item_id":"rs_1","output_index":0,"content_index":0,"delta":"raw `+syn+`"}`)
	rawDone := ev("response.reasoning_text.done", `{"type":"response.reasoning_text.done","item_id":"rs_1","output_index":0,"content_index":0,"text":"raw `+syn+`"}`)
	rw := New().NewResponseRewriter(e.r)
	for _, in := range []sse.Event{added, partAdded, rawText, rawDone} {
		got, err := rw.Event(in)
		if err != nil || len(got) != 1 || !bytes.Equal(got[0].Data, in.Data) || got[0].Name != in.Name {
			t.Errorf("%s not passed through byte-identical", in.Name)
		}
	}
	in := []sse.Event{
		ev("response.reasoning_summary_text.delta", `{"type":"response.reasoning_summary_text.delta","item_id":"rs_1","output_index":0,"summary_index":0,"delta":`+enc("Pinging "+syn)+`}`),
		ev("response.reasoning_summary_text.done", `{"type":"response.reasoning_summary_text.done","item_id":"rs_1","output_index":0,"summary_index":0,"text":`+enc("Pinging "+syn)+`}`),
		ev("response.output_item.done", `{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":`+enc("Pinging "+syn)+`}],"content":[{"type":"reasoning_text","text":"raw `+syn+`"}],"encrypted_content":"gAAAA`+syn+`=="}}`),
	}
	out := feed(t, rw, in)
	if got := deltas(t, out, "response.reasoning_summary_text.delta"); got != "Pinging 10.20.30.15" {
		t.Errorf("summary deltas = %q", got)
	}
	if got := field(t, find(t, out, "response.reasoning_summary_text.done"), "text"); got != "Pinging 10.20.30.15" {
		t.Errorf("summary done = %q", got)
	}
	done := find(t, out, "response.output_item.done")
	if got := field(t, done, "item", "summary", "text"); got != "Pinging 10.20.30.15" {
		t.Errorf("item summary = %q", got)
	}
	if !bytes.Contains(done.Data, []byte(`"encrypted_content":"gAAAA`+syn+`=="`)) {
		t.Errorf("encrypted_content changed")
	}
	if !bytes.Contains(done.Data, []byte(`"text":"raw `+syn+`"`)) {
		t.Errorf("reasoning content changed")
	}
}

func TestUnknownEventPassesThrough(t *testing.T) {
	e := newEnv(t)
	syn := e.pseudo(t, "ipv4", "10.20.30.15")
	rw := New().NewResponseRewriter(e.r)
	for _, in := range []sse.Event{
		ev("response.something_new", `{"type":"response.something_new",  "foo": "bar", "n": 1.0e1}`),
		ev("response.audio.delta", `{"type":"response.audio.delta","item_id":"x","delta":"`+syn+`"}`),
		ev("", "[DONE]"),
		ev("", ""),
		ev("ping", "not json at all "+syn),
		ev("response.in_progress", `{ "type": "response.in_progress", "response": {"id":"resp_`+syn+`","status":"in_progress","output":[]} }`),
	} {
		got, err := rw.Event(in)
		if err != nil || len(got) != 1 || !bytes.Equal(got[0].Data, in.Data) || got[0].Name != in.Name {
			t.Errorf("%q not passed through byte-identical: %v", in.Data, err)
		}
	}
}

// R2: non-delta events of any type have their strings restored.
func TestNonDeltaEventsRestored(t *testing.T) {
	e := newEnv(t)
	syn := e.pseudo(t, "ipv4", "10.20.30.15")
	in := ev("response.created", `{"type":"response.created","response":{"id":"resp_1","instructions":"use `+syn+`","output":[],"metadata":{"k":"`+syn+`"}}}`)
	got, err := New().NewResponseRewriter(e.r).Event(in)
	if err != nil || len(got) != 1 {
		t.Fatalf("got %d events, err %v", len(got), err)
	}
	if field(t, got[0], "response", "instructions") != "use 10.20.30.15" || field(t, got[0], "response", "metadata", "k") != "10.20.30.15" {
		t.Errorf("not restored: %s", got[0].Data)
	}
	if !bytes.HasPrefix(got[0].Data, []byte(`{"type":"response.created","response":{"id":"resp_1",`)) {
		t.Errorf("field order not preserved: %s", got[0].Data)
	}
}

func TestStreamsAreIsolatedPerItem(t *testing.T) {
	e := newEnv(t)
	syn := e.pseudo(t, "ipv4", "10.20.30.15")
	enc := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	d := func(item string, ci int, s string) sse.Event {
		return ev("response.output_text.delta", `{"type":"response.output_text.delta","item_id":"`+item+`","output_index":0,"content_index":`+string(rune('0'+ci))+`,"delta":`+enc(s)+`}`)
	}
	rw := New().NewResponseRewriter(e.r)
	var a, b, c strings.Builder
	collect := func(evs []sse.Event) {
		for _, ev := range evs {
			switch {
			case field(t, ev, "item_id") == "msg_a" && strings.Contains(string(ev.Data), `"content_index":0`):
				a.WriteString(field(t, ev, "delta"))
			case field(t, ev, "item_id") == "msg_a":
				c.WriteString(field(t, ev, "delta"))
			default:
				b.WriteString(field(t, ev, "delta"))
			}
		}
	}
	for _, in := range []sse.Event{
		d("msg_a", 0, "a "+syn[:4]),
		d("msg_b", 0, "b "+syn[4:]),
		d("msg_a", 1, syn[4:]),
		d("msg_a", 0, syn[4:]+" end"),
		d("msg_b", 0, " end"),
	} {
		got, err := rw.Event(in)
		if err != nil {
			t.Fatal(err)
		}
		collect(got)
	}
	collect(rw.Flush())
	if a.String() != "a 10.20.30.15 end" {
		t.Errorf("item a = %q", a.String())
	}
	if b.String() != "b "+syn[4:]+" end" {
		t.Errorf("item b = %q", b.String())
	}
	if c.String() != syn[4:] {
		t.Errorf("item a part 1 = %q", c.String())
	}
}

func TestFlushEmitsHeldBackText(t *testing.T) {
	e := newEnv(t)
	syn := e.pseudo(t, "ipv4", "10.20.30.15")
	rw := New().NewResponseRewriter(e.r)
	got, err := rw.Event(ev("response.output_text.delta", `{"type":"response.output_text.delta","sequence_number":7,"item_id":"msg_1","output_index":0,"content_index":0,"delta":"ip `+syn+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	if s := deltas(t, got, "response.output_text.delta"); s != "ip " {
		t.Errorf("before flush emitted %q, want only the safe prefix", s)
	}
	fl := rw.Flush()
	if len(fl) != 1 {
		t.Fatalf("Flush returned %d events", len(fl))
	}
	if fl[0].Name != "response.output_text.delta" || field(t, fl[0], "delta") != "10.20.30.15" || field(t, fl[0], "item_id") != "msg_1" {
		t.Errorf("flush event = %s %s", fl[0].Name, fl[0].Data)
	}
	if len(rw.Flush()) != 0 {
		t.Errorf("second Flush emitted events")
	}
}

func TestTrailingDeltaBeforeDone(t *testing.T) {
	e := newEnv(t)
	syn := e.pseudo(t, "ipv4", "10.20.30.15")
	rw := New().NewResponseRewriter(e.r)
	if _, err := rw.Event(ev("response.output_text.delta", `{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"ip `+syn+`"}`)); err != nil {
		t.Fatal(err)
	}
	got, err := rw.Event(ev("response.output_text.done", `{"type":"response.output_text.done","item_id":"msg_1","output_index":0,"content_index":0,"text":"ip `+syn+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || typeOf(t, got[0]) != "response.output_text.delta" || field(t, got[0], "delta") != "10.20.30.15" ||
		typeOf(t, got[1]) != "response.output_text.done" || field(t, got[1], "text") != "ip 10.20.30.15" {
		t.Errorf("got %d events: %v", len(got), got)
	}
	// output_item.done also flushes every stream of its item.
	rw = New().NewResponseRewriter(e.r)
	if _, err := rw.Event(ev("response.function_call_arguments.delta", `{"type":"response.function_call_arguments.delta","item_id":"fc_1","output_index":1,"delta":"{\"h\":\"`+syn+`"}`)); err != nil {
		t.Fatal(err)
	}
	got, err = rw.Event(ev("response.output_item.done", `{"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","id":"fc_1","arguments":"{\"h\":\"`+syn+`\"}"}}`))
	if err != nil || len(got) != 2 || typeOf(t, got[0]) != "response.function_call_arguments.delta" {
		t.Fatalf("output_item.done did not flush first: %v %v", got, err)
	}
}

func TestMalformedKnownDeltaBlocked(t *testing.T) {
	e := newEnv(t)
	for _, data := range []string{
		`{"type":"response.output_text.delta","item_id":"m","delta":5}`,
		`{"type":"response.function_call_arguments.delta","item_id":"m"}`,
	} {
		if _, err := New().NewResponseRewriter(e.r).Event(ev("x", data)); err == nil {
			t.Errorf("malformed delta not rejected: %s", data)
		}
	}
}
