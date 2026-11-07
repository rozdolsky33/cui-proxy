package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func runCLI(stdin string, args ...string) (code int, stdout, stderr string) {
	var o, e bytes.Buffer
	code = run(args, strings.NewReader(stdin), &o, &e)
	return code, o.String(), e.String()
}

func TestVersion(t *testing.T) {
	code, out, _ := runCLI("", "version")
	if code != 0 || out != "cui-proxy dev\n" {
		t.Fatalf("version: code %d, out %q", code, out)
	}
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"bogus"},
		{"check-config"},          // -config missing
		{"test-rules"},            // -config missing
		{"serve"},                 // -config missing
		{"check-config", "-nope"}, // unknown flag
		{"check-config", "-config", "x", "extra"},
		{"version", "extra"},
	} {
		code, _, errOut := runCLI("", args...)
		if code != 2 {
			t.Errorf("%q: exit %d, want 2", args, code)
		}
		if errOut == "" {
			t.Errorf("%q: no usage message", args)
		}
	}
}

func TestCheckConfigExitCodes(t *testing.T) {
	dir, cfg := testDir(t, cfgSpec{})
	code, out, errOut := runCLI("", "check-config", "-config", cfg)
	if code != 0 || out != "ok: 2 rules\n" {
		t.Fatalf("valid config: code %d, out %q, err %q", code, out, errOut)
	}

	cases := map[string]string{
		"unknown synth":   strings.Replace(cfgSpec{}.render(), "synth: hostname", "synth: nonsense", 1),
		"missing dict":    strings.Replace(cfgSpec{}.render(), "file: a.txt", "file: missing.txt", 1),
		"bad yaml":        "listen: [\n",
		"non-loopback":    strings.Replace(cfgSpec{}.render(), "listen: 127.0.0.1:0\n", "listen: 0.0.0.0:8787\n", 1),
		"bad upstream":    strings.Replace(cfgSpec{}.render(), "http://127.0.0.1:1/codex", "http://chatgpt.example/codex", 1),
		"unknown field":   cfgSpec{Extra: "bogus: 1"}.render(),
		"empty dict file": "",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(dir, "bad.yaml")
			if name == "empty dict file" {
				writeFile(t, filepath.Join(dir, "empty.txt"), "# nothing\n")
				content = strings.Replace(cfgSpec{}.render(), "file: a.txt", "file: empty.txt", 1)
			}
			writeFile(t, p, content)
			code, out, errOut := runCLI("", "check-config", "-config", p)
			if code != 1 || out != "" || !strings.HasPrefix(errOut, "cui-proxy: ") {
				t.Fatalf("code %d, out %q, err %q", code, out, errOut)
			}
			assertNoValues(t, "stderr", errOut, hostA, hostB)
		})
	}

	code, _, errOut = runCLI("", "check-config", "-config", filepath.Join(dir, "absent.yaml"))
	if code != 1 || !strings.HasPrefix(errOut, "cui-proxy: ") {
		t.Fatalf("absent file: code %d, err %q", code, errOut)
	}
}

// check-config warns about policy.on_error: passthrough with the same line
// serve logs; the config is still valid.
func TestCheckConfigWarnsPassthrough(t *testing.T) {
	_, cfg := testDir(t, cfgSpec{OnError: "passthrough"})
	code, out, errOut := runCLI("", "check-config", "-config", cfg)
	if code != 0 || out != "ok: 2 rules\n" || errOut != "WARN policy.on_error=passthrough: UNSAFE: failures forward the original request\n" {
		t.Fatalf("code %d, out %q, err %q", code, out, errOut)
	}
	_, cfg = testDir(t, cfgSpec{})
	if code, _, errOut := runCLI("", "check-config", "-config", cfg); code != 0 || errOut != "" {
		t.Fatalf("block policy: code %d, err %q", code, errOut)
	}
}

func TestTestRulesNeverPrintsVault(t *testing.T) {
	_, cfg := testDir(t, cfgSpec{})
	in := "ssh " + hostA + " then ping " + realIP + " and " + realIP + "; instance " + realOCID + "\n"
	code, out, errOut := runCLI(in, "test-rules", "-config", cfg)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	assertNoValues(t, "stdout", out, hostA, realIP, realOCID)
	assertNoValues(t, "stderr", errOut, hostA, realIP, realOCID)
	if !strings.HasPrefix(out, "ssh ") || !strings.HasSuffix(out, "\n") {
		t.Fatalf("sanitized text not printed as is: %q", out)
	}
	// The synthetics appear on stdout only; stderr has no mapping pairs.
	for _, w := range strings.Fields(out) {
		w = strings.TrimRight(w, ";")
		if strings.Contains(w, ".") && len(w) > 6 && strings.Contains(errOut, w) {
			t.Fatalf("stderr shows a synthetic value %q", w)
		}
	}
	for _, want := range []string{"RULE", "CLASS", "COUNT", "hosts-a", "builtin.ipv4", "builtin.ocid"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("table lacks %q:\n%s", want, errOut)
		}
	}
	// builtin.ipv4 replaced twice.
	for _, line := range strings.Split(errOut, "\n") {
		if strings.HasPrefix(line, "builtin.ipv4") && !strings.HasSuffix(strings.TrimSpace(line), "2") {
			t.Errorf("ipv4 count line: %q", line)
		}
	}
}

func TestTestRulesInputCap(t *testing.T) {
	_, cfg := testDir(t, cfgSpec{})
	big := strings.Repeat("a", maxRulesInput+1)
	code, out, errOut := runCLI(big, "test-rules", "-config", cfg)
	if code != 1 || out != "" || !strings.Contains(errOut, "8 MiB") {
		t.Fatalf("code %d, out len %d, err %q", code, len(out), errOut)
	}
}

func TestServeSignals(t *testing.T) {
	up := newUpstream(t, nil)
	spec := cfgSpec{Upstream: up.url()}
	_, cfg := testDir(t, spec)
	logs := &logBuf{}
	a := startApp(t, cfg, logs, nil)
	before := a.cur.Load()
	spec.Rules = rulesB
	writeFile(t, cfg, spec.render())

	sigs := make(chan os.Signal) // unbuffered: each send waits for the loop
	done := make(chan int, 1)
	go func() { done <- serveLoop(a, sigs, logs.logf, time.Second) }()
	sigs <- syscall.SIGHUP
	sigs <- syscall.SIGTERM // received only after the reload finished
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("serveLoop exit %d:\n%s", code, logs)
		}
	case <-time.After(waitLimit):
		t.Fatal("serveLoop did not stop")
	}
	if a.cur.Load() == before {
		t.Fatal("SIGHUP did not reload")
	}
	if !strings.Contains(logs.String(), "reload ok") || !strings.Contains(logs.String(), "shutting down") {
		t.Fatalf("logs:\n%s", logs)
	}
}

func TestFlagErrorsArePrefixed(t *testing.T) {
	for _, args := range [][]string{
		{"check-config", "-nope"},
		{"serve", "-config"}, // flag needs an argument
		{"test-rules", "-bogus=1"},
	} {
		code, out, errOut := runCLI("", args...)
		if code != 2 {
			t.Errorf("%q: exit %d, want 2", args, code)
		}
		if out != "" {
			t.Errorf("%q: stdout %q", args, out)
		}
		first, _, _ := strings.Cut(errOut, "\n")
		if !strings.HasPrefix(first, "cui-proxy: "+args[0]+": ") {
			t.Errorf("%q: first stderr line %q lacks the cui-proxy prefix", args, first)
		}
		if !strings.Contains(errOut, "-config") {
			t.Errorf("%q: no flag usage in %q", args, errOut)
		}
	}
	for _, cmd := range []string{"serve", "check-config", "test-rules"} {
		code, _, errOut := runCLI("", cmd, "-h")
		if code != 0 {
			t.Errorf("%s -h: exit %d, want 0", cmd, code)
		}
		if !strings.Contains(errOut, "-config") {
			t.Errorf("%s -h: no usage in %q", cmd, errOut)
		}
	}
}
