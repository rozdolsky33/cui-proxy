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
