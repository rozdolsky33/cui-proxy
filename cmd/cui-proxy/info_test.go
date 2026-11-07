package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBrowserURL(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1:8787": "http://127.0.0.1:8787/",
		"localhost:8788": "http://localhost:8788/",
		"[::1]:8787":     "http://[::1]:8787/",
		"0.0.0.0:8787":   "http://127.0.0.1:8787/", // wildcard: browse via loopback
		":8787":          "http://127.0.0.1:8787/",
		"[::]:8787":      "http://127.0.0.1:8787/",
		"192.0.2.10:80":  "http://192.0.2.10:80/",
	}
	for in, want := range cases {
		got, err := browserURL(in)
		if err != nil || got != want {
			t.Errorf("browserURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "nonsense", "127.0.0.1", "127.0.0.1:"} {
		if got, err := browserURL(bad); err == nil {
			t.Errorf("browserURL(%q) = %q; want an error", bad, got)
		}
	}
}

func TestInfoPrintsAddresses(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cfg")
	if code, _, errOut := runCLI("", "init", "-dir", dir); code != 0 {
		t.Fatalf("init: %d %q", code, errOut)
	}
	cfg := filepath.Join(dir, "config.yaml")
	code, out, errOut := runCLI("", "info", "-config", cfg)
	if code != 0 {
		t.Fatalf("info: code %d, stderr %q", code, errOut)
	}
	want := "config=" + cfg + "\n" +
		"proxy_url=http://127.0.0.1:8787/\n" +
		"dashboard_url=http://127.0.0.1:8788/\n" +
		"version=dev\n"
	if out != want {
		t.Errorf("info output:\n%q\nwant:\n%q", out, want)
	}
}

func TestInfoWildcardListenUsesLoopback(t *testing.T) {
	_, cfg := testDir(t, cfgSpec{})
	body := strings.Replace(readFile(t, cfg), "listen: 127.0.0.1:0\n", "listen: 0.0.0.0:8787\nallow_non_loopback: true\n", 1)
	writeFile(t, cfg, body)
	code, out, errOut := runCLI("", "info", "-config", cfg)
	if code != 0 {
		t.Fatalf("info: code %d, stderr %q", code, errOut)
	}
	if !strings.Contains(out, "proxy_url=http://127.0.0.1:8787/\n") {
		t.Errorf("wildcard listen not mapped to loopback:\n%s", out)
	}
}

func TestInfoErrors(t *testing.T) {
	if code, _, _ := runCLI("", "info"); code != 2 {
		t.Errorf("info without -config: exit %d, want 2", code)
	}
	bad := filepath.Join(t.TempDir(), "bad.yaml")
	writeFile(t, bad, "listen: [\n")
	code, out, errOut := runCLI("", "info", "-config", bad)
	if code != 1 || out != "" || errOut == "" {
		t.Errorf("info on invalid config: code %d, out %q, err %q", code, out, errOut)
	}
}
