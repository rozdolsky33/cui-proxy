package transform

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cui-proxy/internal/config"
	"cui-proxy/internal/detect"
	"cui-proxy/internal/vault"
)

// policyDetectors builds the builtin set plus a dictionary rule under profile.
func policyDetectors(t *testing.T, profile, builtinAction, dictAction string) []detect.Detector {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hosts.txt"), []byte("host1.corp\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ds, err := detect.BuildForProfile([]config.Rule{
		{Name: "builtin", Type: "builtin", Action: builtinAction},
		{Name: "hosts", Type: "dictionary", File: "hosts.txt", Synth: "hostname", Action: dictAction},
	}, dir, profile)
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

func TestStrictBlocksEveryDetection(t *testing.T) {
	for name, text := range map[string]string{
		"ipv4":       "ssh admin@10.20.30.15 now",
		"ocid":       "ocid1.instance.oc1.phx.abcdef0123456789abcdef",
		"ssn":        "ssn 123-45-6789",
		"email":      "mail bob@corp-internal.com",
		"card":       "card 4111 1111 1111 1111",
		"bearer":     "Authorization: Bearer abcdefgh12345678",
		"dictionary": "ping host1.corp",
		"glued":      "id:fe80::1", // would be downgraded to a REDACTED token under sanitize
	} {
		v := vault.New(testKey)
		sink := &recSink{}
		out, err := NewSanitizer(policyDetectors(t, "strict", "", ""), v, sink).Text(text)
		var se *SpanError
		if !errors.Is(err, ErrBlocked) || !errors.As(err, &se) {
			t.Errorf("%s: err = %v; want a SpanError wrapping ErrBlocked", name, err)
			continue
		}
		if out != "" {
			t.Errorf("%s: blocked Text returned text %q", name, out)
		}
		if se.Rule == "" || se.Class == "" {
			t.Errorf("%s: SpanError has no rule/class: %+v", name, se)
		}
		if strings.Contains(se.Error(), "10.20.30.15") || strings.Contains(se.Error(), "host1.corp") || strings.Contains(se.Error(), "bob@") {
			t.Errorf("%s: the error text carries the value: %v", name, se)
		}
		if v.Len() != 0 {
			t.Errorf("%s: a blocked request coined %d vault mappings", name, v.Len())
		}
		if len(sink.evs) != 0 {
			t.Errorf("%s: a blocked request recorded events %+v", name, sink.evs)
		}
	}
}

func TestStrictPassesCleanText(t *testing.T) {
	const text = "list the files in the repo and explain the build"
	out, err := NewSanitizer(policyDetectors(t, "strict", "", ""), vault.New(testKey), nil).Text(text)
	if err != nil || out != text {
		t.Errorf("clean text: %q, %v", out, err)
	}
}

func TestPerRuleBlockUnderSanitizeProfile(t *testing.T) {
	ds := policyDetectors(t, "sanitize", "", "block") // dictionary blocks, builtin sanitizes
	v := vault.New(testKey)
	s := NewSanitizer(ds, v, nil)

	// A builtin value is still masked.
	out, err := s.Text("ssh admin@10.20.30.15")
	if err != nil || strings.Contains(out, "10.20.30.15") {
		t.Fatalf("builtin under sanitize: %q, %v", out, err)
	}
	before := v.Len()
	// The blocking rule refuses, and nothing from the same text is coined first.
	_, err = s.Text("ssh admin@10.99.99.99 to host1.corp")
	var se *SpanError
	if !errors.Is(err, ErrBlocked) || !errors.As(err, &se) || se.Rule != "hosts" {
		t.Fatalf("err = %v; want ErrBlocked from rule hosts", err)
	}
	if v.Len() != before {
		t.Errorf("a blocked text coined mappings: %d -> %d", before, v.Len())
	}
}

// The blocking match sits inside a longer match from a sanitize rule (the
// builtin email contains the blocked host): it must still block.
func TestBlockWinsOverLongerSanitizeMatch(t *testing.T) {
	ds := policyDetectors(t, "sanitize", "", "block")
	v := vault.New(testKey)
	_, err := NewSanitizer(ds, v, nil).Text("mail admin@host1.corp now")
	var se *SpanError
	if !errors.Is(err, ErrBlocked) || !errors.As(err, &se) || se.Rule != "hosts" || se.Class != "hostname" {
		t.Fatalf("err = %v; want ErrBlocked naming rule hosts / class hostname", err)
	}
	if v.Len() != 0 {
		t.Errorf("coined %d mappings", v.Len())
	}
}

// A value that already is a vault synthetic is not a detection of real data:
// history Codex resends after a switch from sanitize to strict must not block.
func TestBlockIgnoresExistingSynthetics(t *testing.T) {
	v := vault.New(testKey)
	sanitized, err := NewSanitizer(policyDetectors(t, "sanitize", "", ""), v, nil).Text("ssh 10.20.30.15")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sanitized, "10.20.30.15") {
		t.Fatalf("setup: %q", sanitized)
	}
	// Same vault, now strict: the synthetic in the resent history passes...
	strict := NewSanitizer(policyDetectors(t, "strict", "", ""), v, nil)
	if out, err := strict.Text(sanitized); err != nil || out != sanitized {
		t.Errorf("resent synthetic: %q, %v; want it left alone", out, err)
	}
	// ...but the real value still blocks.
	if _, err := strict.Text("ssh 10.20.30.15"); !errors.Is(err, ErrBlocked) {
		t.Errorf("real value under strict: %v; want ErrBlocked", err)
	}
}

func TestSanitizeProfileUnchanged(t *testing.T) {
	out, err := NewSanitizer(policyDetectors(t, "sanitize", "", ""), vault.New(testKey), nil).Text("ssh admin@10.20.30.15 and host1.corp")
	if err != nil || strings.Contains(out, "10.20.30.15") || strings.Contains(out, "host1.corp") {
		t.Errorf("sanitize profile: %q, %v", out, err)
	}
}
