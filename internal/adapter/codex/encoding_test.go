package codex

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"cui-proxy/internal/adapter"
	"cui-proxy/internal/config"
	"cui-proxy/internal/detect"
	"cui-proxy/internal/transform"
	"cui-proxy/internal/vault"
)

func TestEmbeddedJSONEscapes(t *testing.T) {
	for _, sep := range []string{"\n", "\r", "\t", "\b", "\f", "\\", "\"", "\u0001", "\u2028"} {
		for _, real := range []string{"git@github.com", "10.20.30.15", "512-34-5678"} {
			t.Run(fmt.Sprintf("%q/%s", sep, real), func(t *testing.T) {
				e := newEnv(t)
				text := sep + real + sep
				inner, _ := json.Marshal(map[string]string{"text": text})
				nested, _ := json.Marshal(map[string]string{"payload": string(inner)})
				for _, input := range []string{string(inner), string(nested)} {
					body, _ := json.Marshal(map[string]string{"input": input})
					out := mustSanitize(t, e, body)
					var got map[string]string
					if err := json.Unmarshal(out, &got); err != nil {
						t.Fatal(err)
					}
					if !json.Valid([]byte(got["input"])) {
						t.Fatal("embedded JSON invalid")
					}
					// No second-turn remasking of synthetics or escape damage.
					if again := mustSanitize(t, e, out); string(again) != string(out) {
						t.Fatalf("not idempotent: %s", again)
					}
					var leaf map[string]string
					if err := json.Unmarshal([]byte(got["input"]), &leaf); err != nil {
						t.Fatal(err)
					}
					if payload, ok := leaf["payload"]; ok {
						if err := json.Unmarshal([]byte(payload), &leaf); err != nil {
							t.Fatal(err)
						}
					}
					if leaf["text"] == text || e.r.Text(leaf["text"]) != text {
						t.Fatal("mask or exact decoded restoration failed")
					}
				}
			})
		}
	}
}

func TestEmbeddedProtocolKeysAreContent(t *testing.T) {
	ds, err := detect.Build([]config.Rule{{Name: "custom", Type: "regex", Pattern: "session_id", Synth: "secret"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	v := vault.New(testKey)
	san := transform.NewSanitizer(ds, v, nil)
	out, err := New().SanitizeRequest([]byte(`{"input":"{\"session_id\":\"clean\"}"}`), san)
	if err != nil || strings.Contains(string(out), "session_id") {
		t.Fatalf("key not masked: %s %v", out, err)
	}
}

func TestEmbeddedStructuralMatchesFailClosed(t *testing.T) {
	for _, action := range []string{"sanitize", "block"} {
		ds, err := detect.Build([]config.Rule{{Name: "structure", Type: "regex", Pattern: `"account"\s*:\s*"private"`, Synth: "secret", Action: action}}, "")
		if err != nil {
			t.Fatal(err)
		}
		out, err := New().SanitizeRequest([]byte(`{"input":"{\"account\":\"private\"}"}`), transform.NewSanitizer(ds, vault.New(testKey), nil))
		if err == nil || out != nil {
			t.Fatal("structural match forwarded")
		}
		if action == "block" && !errors.Is(err, transform.ErrBlocked) {
			t.Fatalf("policy weakened: %v", err)
		}
		if action == "sanitize" && !errors.Is(err, adapter.ErrBadBody) {
			t.Fatalf("wrong failure: %v", err)
		}
	}
}
