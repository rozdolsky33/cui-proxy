package codex

import (
	"errors"
	"strings"
	"testing"

	"cui-proxy/internal/config"
	"cui-proxy/internal/detect"
	"cui-proxy/internal/transform"
	"cui-proxy/internal/vault"
)

func strictSanitizer(t *testing.T) *transform.Sanitizer {
	t.Helper()
	ds, err := detect.BuildForProfile([]config.Rule{{Name: "builtin", Type: "builtin"}}, "", "strict")
	if err != nil {
		t.Fatal(err)
	}
	return transform.NewSanitizer(ds, vault.New([]byte("0123456789abcdef0123456789abcdef")), nil)
}

// Under strict, a value anywhere the request carries one blocks the whole
// request: leaves, object keys, JSON inside strings, escaped forms.
func TestStrictBlocksEverywhereInARequest(t *testing.T) {
	wrap := func(inner string) string {
		return `{"model":"gpt-5-codex","stream":true,"input":[` + inner + `]}`
	}
	msg := func(text string) string {
		return `{"type":"message","role":"user","content":[{"type":"input_text","text":"` + text + `"}]}`
	}
	for name, body := range map[string]string{
		"message text":      wrap(msg("ssh admin@10.20.30.15")),
		"tool output":       wrap(`{"type":"function_call_output","call_id":"c1","output":"host 10.20.30.15 is up"}`),
		"arguments value":   wrap(`{"type":"function_call","call_id":"c1","name":"sh","arguments":"{\"cmd\":\"ping 10.20.30.15\"}"}`),
		"arguments key":     wrap(`{"type":"function_call","call_id":"c1","name":"sh","arguments":"{\"10.20.30.15\":1}"}`),
		"escaped dots":      wrap(msg(`ssh 10.20.30.15`)),
		"client_metadata":   `{"model":"m","input":[],"client_metadata":{"note":"from 10.20.30.15"}}`,
		"tools description": `{"model":"m","input":[],"tools":[{"type":"function","name":"f","description":"on 10.20.30.15","parameters":{"type":"object"}}]}`,
		"large integer":     `{"model":"m","input":[],"client_metadata":{"n":4111111111111111}}`,
		"ocid in a key":     `{"model":"m","input":[],"client_metadata":{"ocid1.instance.oc1.phx.abcdef0123456789abcdef":"x"}}`,
	} {
		out, err := New().SanitizeRequest([]byte(body), strictSanitizer(t))
		if err == nil || out != nil {
			t.Errorf("%s: forwarded under strict (err=%v, out=%q)", name, err, out)
			continue
		}
		if !errors.Is(err, transform.ErrBlocked) {
			t.Errorf("%s: err = %v; want ErrBlocked", name, err)
		}
		for _, leak := range []string{"10.20.30.15", "4111111111111111", "abcdef0123456789abcdef"} {
			if strings.Contains(err.Error(), leak) {
				t.Errorf("%s: the error carries %q: %v", name, leak, err)
			}
		}
	}
}

func TestStrictForwardsACleanRequestUnchanged(t *testing.T) {
	body := `{"model":"gpt-5-codex","stream":true,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"explain the build"}]}]}`
	out, err := New().SanitizeRequest([]byte(body), strictSanitizer(t))
	if err != nil || string(out) != body {
		t.Errorf("clean request: %q, %v", out, err)
	}
}

// Fields the adapter normally passes through unscanned (opaque protocol
// positions) are still checked when any rule blocks: a value there must not
// slip past strict. encrypted_content, vendor ciphertext, is the one
// exception.
func TestStrictChecksOpaqueProtocolPositions(t *testing.T) {
	const v = "10.20.30.15"
	msg := func(extra string) string {
		return `{"type":"message","role":"user",` + extra + `"content":[{"type":"input_text","text":"hi"}]}`
	}
	for name, body := range map[string]string{
		"model":                  `{"model":"` + v + `","input":[]}`,
		"prompt_cache_key":       `{"model":"m","prompt_cache_key":"ssn 123-45-6789","input":[]}`,
		"item id":                `{"model":"m","input":[` + msg(`"id":"`+v+`",`) + `]}`,
		"item call_id":           `{"model":"m","input":[{"type":"function_call_output","call_id":"` + v + `","output":"ok"}]}`,
		"item role":              `{"model":"m","input":[{"type":"message","role":"` + v + `","content":[]}]}`,
		"item status":            `{"model":"m","input":[` + msg(`"status":"`+v+`",`) + `]}`,
		"function_call name":     `{"model":"m","input":[{"type":"function_call","call_id":"c","name":"` + v + `","arguments":"{}"}]}`,
		"part id":                `{"model":"m","input":[{"type":"message","role":"user","content":[{"type":"input_text","id":"` + v + `","text":"hi"}]}]}`,
		"tool name":              `{"model":"m","input":[],"tools":[{"type":"function","name":"` + v + `","parameters":{"type":"object"}}]}`,
		"tool type":              `{"model":"m","input":[],"tools":[{"type":"` + v + `"}]}`,
		"include entry":          `{"model":"m","input":[],"include":["ocid1.instance.oc1.phx.abcdef0123456789abcdef"]}`,
		"float number":           `{"model":"m","input":[],"client_metadata":{"n":4111111111111111.0}}`,
		"exponent number":        `{"model":"m","input":[],"client_metadata":{"n":4.111111111111111e15}}`,
		"first leaf clean, last": `{"model":"m","input":[` + msg("") + `,{"type":"function_call_output","call_id":"c","output":"fine"},{"type":"function_call_output","call_id":"d","output":"` + v + `"}]}`,
	} {
		out, err := New().SanitizeRequest([]byte(body), strictSanitizer(t))
		if err == nil || out != nil || !errors.Is(err, transform.ErrBlocked) {
			t.Errorf("%s: err=%v out=%q; want ErrBlocked and nothing forwarded", name, err, out)
		}
	}
	// A custom rule that matches a plain short number blocks it as a number.
	rules := []config.Rule{{Name: "acct", Type: "regex", Pattern: `\b\d{6}\b`, Synth: "secret"}}
	ds, err := detect.BuildForProfile(rules, "", "strict")
	if err != nil {
		t.Fatal(err)
	}
	san := transform.NewSanitizer(ds, vault.New([]byte("0123456789abcdef0123456789abcdef")), nil)
	if _, err := New().SanitizeRequest([]byte(`{"model":"m","input":[],"client_metadata":{"n":123456}}`), san); !errors.Is(err, transform.ErrBlocked) {
		t.Errorf("short number matching a rule: %v; want ErrBlocked", err)
	}
}

// A Codex-shaped request with nothing sensitive in it is forwarded unchanged
// under strict, including a large opaque ciphertext full of digits and UUID
// ids: scanning the opaque protocol fields must not wedge a session.
func TestStrictForwardsARealisticCleanRequestUnchanged(t *testing.T) {
	cipher := strings.Repeat("gAAAAABm1234567890123456xYz+/9876543210987654", 400)
	body := `{"model":"gpt-5.5","stream":true,"store":false,"parallel_tool_calls":true,"tool_choice":"auto",` +
		`"prompt_cache_key":"01a1b391-bc82-7fc1-af74-707b50231d77","include":["reasoning.encrypted_content"],` +
		`"reasoning":{"effort":"high"},"text":{"verbosity":"medium"},` +
		`"client_metadata":{"turn_id":"01a1b391-bcdb-78f0-963e-cfc2a5d77cbd","thread_id":"01a1b391-bc82-7fc1-af74-707b50231d77","window_number":0},` +
		`"tools":[{"type":"function","name":"exec_command","description":"Runs a command","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}},{"type":"web_search"}],` +
		`"input":[{"type":"message","role":"developer","content":[{"type":"input_text","text":"You are a coding agent."}]},` +
		`{"type":"reasoning","id":"rs_01a1b391bc827fc1af74707b","summary":[],"encrypted_content":"` + cipher + `"},` +
		`{"type":"function_call","id":"fc_1","call_id":"call_01a1b391","name":"exec_command","arguments":"{\"cmd\":\"ls -la\"}"},` +
		`{"type":"function_call_output","call_id":"call_01a1b391","output":"total 8\ndrwxr-xr-x  2 user  staff  64 Nov  7 09:00 ."},` +
		`{"type":"message","role":"user","status":"completed","content":[{"type":"input_text","text":"explain the build"}]}]}`
	out, err := New().SanitizeRequest([]byte(body), strictSanitizer(t))
	if err != nil {
		t.Fatalf("a clean request was blocked under strict: %v", err)
	}
	if string(out) != body {
		t.Error("a clean request was modified under strict")
	}
}

// encrypted_content is vendor ciphertext resent as received: it is never a
// place the user's text goes, and scanning megabytes of base64 invites false
// positives. It is the one opaque field strict does not check.
func TestStrictDoesNotScanEncryptedContent(t *testing.T) {
	body := `{"model":"m","input":[{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"AAAA 10.20.30.15 AAAA"}]}`
	if _, err := New().SanitizeRequest([]byte(body), strictSanitizer(t)); err != nil {
		t.Errorf("encrypted_content was scanned: %v", err)
	}
}
