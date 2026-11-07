package transform

import (
	"errors"
	"strings"
	"testing"

	"cui-proxy/internal/config"
	"cui-proxy/internal/detect"
	"cui-proxy/internal/synth"
	"cui-proxy/internal/vault"
)

// I2: every span-level sanitizer error is a *SpanError naming the rule and
// class (never the value), still matching its sentinel with errors.Is.
func TestSanitizeErrorsAreSpanErrors(t *testing.T) {
	ds, err := detect.Build([]config.Rule{{Name: "bad", Type: "regex", Pattern: `XCIDR\d+`, Synth: "cidr"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewSanitizer(ds, vault.New(testKey), nil).Text("a XCIDR123 b")
	var se *SpanError
	if !errors.As(err, &se) || se.Rule != "bad" || se.Class != "cidr" || !errors.Is(err, ErrPseudonym) || errors.Is(err, synth.ErrExhausted) {
		t.Fatalf("err = %#v", err)
	}
	if msg := err.Error(); !strings.Contains(msg, "rule=bad class=cidr") || strings.Contains(msg, "XCIDR123") {
		t.Errorf("msg = %q", msg)
	}

	testHookBuilt = func(out string) string { return "x" + out }
	_, err = NewSanitizer(builtins(t), vault.New(testKey), nil).Text("ssn 123-45-6789 ok")
	testHookBuilt = nil
	if !errors.As(err, &se) || se.Rule != "builtin.ssn" || se.Class != "ssn" || !errors.Is(err, ErrUnrestorable) {
		t.Fatalf("err = %#v", err)
	}

	v := vault.New(testKey)
	s := NewSanitizer(builtins(t), v, nil)
	if _, err := s.Text("fill" + ips(0, 762)); err != nil {
		t.Fatal(err)
	}
	out, err := s.Text("one more 10.200.0.1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Text("ping " + strings.TrimPrefix(out, "one more ") + " please")
	if !errors.As(err, &se) || se.Rule != "builtin.ipv4" || se.Class != "ipv4" || !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("err = %#v", err)
	}
}
