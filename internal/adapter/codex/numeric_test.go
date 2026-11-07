package codex

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"cui-proxy/internal/adapter"
	"cui-proxy/internal/config"
	"cui-proxy/internal/detect"
	"cui-proxy/internal/transform"
	"cui-proxy/internal/vault"
)

// A number does not become safe to forward when a client spells it with a
// decimal point or exponent. Refuse sensitive forms that cannot be masked
// while retaining their original numeric spelling.
func TestSanitizeRefusesSensitiveDecimalAndExponentNumbers(t *testing.T) {
	for _, lit := range []string{
		"4111111111111111.0", "4.111111111111111e15",
		"6.011000990139424124e18", "60110009901394241240e-1",
		"-4.111111111111111E+15",
	} {
		t.Run(lit, func(t *testing.T) {
			body := []byte(`{"input":[],"client_metadata":{"n":` + lit + `}}`)
			out, err := New().SanitizeRequest(body, newEnv(t).s)
			if !errors.Is(err, adapter.ErrBadBody) || out != nil {
				t.Fatalf("sensitive numeric form forwarded: err=%v out=%s", err, out)
			}
			if strings.Contains(err.Error(), lit) {
				t.Fatal("error includes the sensitive value")
			}
		})
	}
}

func TestSanitizeChecksShortNumbersAgainstCustomRules(t *testing.T) {
	ds, err := detect.Build([]config.Rule{{Name: "account", Type: "regex", Pattern: `\b123456\b`, Synth: "secret"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	v := vault.New(testKey)
	s := transform.NewSanitizer(ds, v, nil)
	for _, lit := range []string{"123456", "-123456"} {
		body := []byte(`{"input":[],"client_metadata":{"n":` + lit + `}}`)
		out, err := New().SanitizeRequest(body, s)
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Metadata struct {
				N string `json:"n"`
			} `json:"client_metadata"`
		}
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatalf("sensitive short number was not masked as a string: %v", err)
		}
		if strings.Contains(string(out), "123456") {
			t.Fatalf("custom numeric match survived: %s", out)
		}
		if restored := transform.NewRestorer(v).Text(got.Metadata.N); restored != lit {
			t.Fatalf("numeric spelling changed: got %q, want %q", restored, lit)
		}
	}
	for _, lit := range []string{"123456.0", "1.23456e5"} {
		body := []byte(`{"input":[],"client_metadata":{"n":` + lit + `}}`)
		if out, err := New().SanitizeRequest(body, s); !errors.Is(err, adapter.ErrBadBody) || out != nil {
			t.Fatalf("custom numeric form forwarded: err=%v out=%s", err, out)
		}
	}
}

func TestSanitizePreservesCleanNumericSpellings(t *testing.T) {
	for _, lit := range []string{"0", "-0", "1", "12.5", "1e3", "-0.001E+2", "1700000000004"} {
		body := []byte(`{"input":[],"client_metadata":{"n":` + lit + `}}`)
		out, err := New().SanitizeRequest(body, newEnv(t).s)
		if err != nil || string(out) != string(body) {
			t.Errorf("clean number changed: err=%v out=%s", err, out)
		}
	}
}
