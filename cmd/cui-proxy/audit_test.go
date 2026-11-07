package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// post sends text with an optional extra header and returns the response
// status and its request id.
func post(t *testing.T, proxyAddr, text, hdr, val string) (int, string) {
	t.Helper()
	req, err := http.NewRequest("POST", "http://"+proxyAddr+"/v1/responses", strings.NewReader(userBody(text)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if hdr != "" {
		req.Header.Set(hdr, val)
	}
	res, err := testClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode, res.Header.Get("X-Cui-Proxy-Request-Id")
}

func TestServeRecordsDecisionsThenVerifyAndExplain(t *testing.T) {
	up := newUpstream(t, nil)
	dir, cfg := testDir(t, cfgSpec{Upstream: up.url()})
	logs := &logBuf{}
	a := startApp(t, cfg, logs, nil)
	px, _ := a.Addrs()

	st, okID := post(t, px, "ssh "+realIP, "", "")
	if st != http.StatusOK {
		t.Fatalf("status %d", st)
	}
	up.next(t)
	st, blockedID := post(t, px, "hi", "X-Note", "on "+hostA)
	if st != http.StatusUnprocessableEntity {
		t.Fatalf("header block: status %d", st)
	}
	a.Close()

	path := filepath.Join(dir, "decisions.jsonl")
	if !strings.Contains(logs.String(), "audit: decision log "+path+" (on_error=warn, rotated at 50 MB, rotated files kept 90 days)") {
		t.Errorf("startup log lacks the audit line:\n%s", logs.String())
	}
	raw := readFile(t, path)
	assertNoValues(t, "decision log", raw, realIP, hostA, "ssh")

	code, out, errOut := runCLI("", "audit", "verify", "-config", cfg)
	if code != 0 || !strings.Contains(out, "Records checked  2") || !strings.Contains(out, "genesis") ||
		!strings.Contains(out, "Result: OK") || !strings.Contains(out, "tamper-evident, not tamper-proof") {
		t.Fatalf("verify: code %d out %s err %s", code, out, errOut)
	}
	if code2, out2, _ := runCLI("", "audit", "verify", "-file", path); code2 != 0 || out2 != out {
		t.Errorf("verify -file differs from -config: %d\n%s", code2, out2)
	}

	code, out, errOut = runCLI("", "explain", "-config", cfg, blockedID)
	for _, want := range []string{"Request " + blockedID, "decision  blocked", "reason    header",
		"blocked before anything was sent upstream", "Headers are never rewritten", "line 2"} {
		if code != 0 || !strings.Contains(out, want) {
			t.Errorf("explain blocked: code %d, lacks %q:\n%s%s", code, want, out, errOut)
		}
	}
	code, out, _ = runCLI("", "explain", "-file", path, okID)
	if code != 0 || !strings.Contains(out, "decision  sanitized") || !strings.Contains(out, "rules     builtin.ipv4 (1)") ||
		!strings.Contains(out, "forwarded with the detected values masked") {
		t.Errorf("explain sanitized: code %d\n%s", code, out)
	}
	assertNoValues(t, "explain", out, realIP, hostA)

	if code, _, errOut := runCLI("", "explain", "-config", cfg, "0123456789abcdef"); code != 1 || !strings.Contains(errOut, "no record for request") {
		t.Errorf("unknown id: code %d err %q", code, errOut)
	}
}

func TestAuditCommandUsage(t *testing.T) {
	_, cfg := testDir(t, cfgSpec{})
	for _, args := range [][]string{
		{"audit"},
		{"audit", "nope"},
		{"audit", "verify", "-file", "x", "-config", cfg},
		{"audit", "verify", "extra"},
		{"explain"},
		{"explain", "-config", cfg},
		{"explain", "-config", cfg, "not-an-id"},
		{"explain", "-config", cfg, "0123456789abcdef", "more"},
	} {
		if code, _, errOut := runCLI("", args...); code != exitUsage || !strings.HasPrefix(errOut, "cui-proxy: ") {
			t.Errorf("%q: code %d err %q", args, code, errOut)
		}
	}
	// No log yet, or an unreadable path: exit 1.
	if code, _, errOut := runCLI("", "audit", "verify", "-config", cfg); code != 1 || !strings.Contains(errOut, "no decision log found") {
		t.Errorf("verify without a log: code %d err %q", code, errOut)
	}
	if code, _, _ := runCLI("", "explain", "-file", filepath.Join(t.TempDir(), "none"), "0123456789abcdef"); code != 1 {
		t.Errorf("explain without a log: code %d", code)
	}
	_, off := testDir(t, cfgSpec{Audit: "{ enabled: false }"})
	if code, _, errOut := runCLI("", "audit", "verify", "-config", off); code != 1 || !strings.Contains(errOut, "audit is disabled") {
		t.Errorf("verify, audit disabled: code %d err %q", code, errOut)
	}
	if code, out, _ := runCLI("", "help"); code != 0 || !strings.Contains(out, "audit verify") || !strings.Contains(out, "explain [-config P] REQUEST-ID") {
		t.Errorf("usage lacks the audit commands:\n%s", out)
	}
}

func TestAuditVerifyReportsTamperingAndExits1(t *testing.T) {
	up := newUpstream(t, nil)
	dir, cfg := testDir(t, cfgSpec{Upstream: up.url()})
	a := startApp(t, cfg, &logBuf{}, nil)
	px, _ := a.Addrs()
	for range 3 {
		post(t, px, "hi", "", "")
		up.next(t)
	}
	a.Close()
	path := filepath.Join(dir, "decisions.jsonl")
	lines := strings.SplitAfter(readFile(t, path), "\n")
	writeFile(t, path, lines[0]+lines[2]) // delete the middle line
	code, out, _ := runCLI("", "audit", "verify", "-config", cfg)
	if code != 1 || !strings.Contains(out, "FIRST BREAK: "+path+" line 2: prev_hash_mismatch") || !strings.Contains(out, "Result: BROKEN") {
		t.Fatalf("code %d\n%s", code, out)
	}
}

// A damaged log refuses to start where a failed record blocks (strict), and
// starts a new, reported segment under warn (the sanitize default).
func TestStartupWithADamagedLog(t *testing.T) {
	strictRules := "  - name: builtin\n    type: builtin\n"
	for _, c := range []struct {
		profile string
		starts  bool
	}{{"strict", false}, {"sanitize", true}} {
		up := newUpstream(t, nil)
		dir, cfg := testDir(t, cfgSpec{Upstream: up.url(), Profile: c.profile, Rules: strictRules})
		path := filepath.Join(dir, "decisions.jsonl")
		writeFile(t, path, "{\"not\":\"a record\"}\n")
		logs := &logBuf{}
		a, err := NewApp(AppOptions{ConfigPath: cfg, Logf: logs.logf})
		if !c.starts {
			if err == nil || !strings.Contains(err.Error(), "cui-proxy audit verify -file "+path) {
				t.Errorf("%s: NewApp = %v; want a refusal naming audit verify", c.profile, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", c.profile, err)
		}
		a.Close()
		if !strings.Contains(logs.String(), "WARN decision log damaged at line 1 (malformed)") {
			t.Errorf("%s: logs %s", c.profile, logs.String())
		}
		if code, out, _ := runCLI("", "audit", "verify", "-file", path); code != 1 || !strings.Contains(out, "FIRST BREAK") {
			t.Errorf("%s: verify after a new segment: code %d\n%s", c.profile, code, out)
		}
	}
}

// Fail closed end to end: under strict, a decision log that cannot be
// written blocks the request and the upstream is never called.
func TestStrictBlocksWhenTheLogCannotBeWritten(t *testing.T) {
	up := newUpstream(t, nil)
	_, cfg := testDir(t, cfgSpec{Upstream: up.url(), Profile: "strict", Rules: "  - name: builtin\n    type: builtin\n"})
	logs := &logBuf{}
	a := startApp(t, cfg, logs, nil)
	px, _ := a.Addrs()
	a.decisions.Close() // every later write fails
	st, _ := post(t, px, "explain the build", "", "")
	if st != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503", st)
	}
	select {
	case s := <-up.texts:
		t.Fatalf("the upstream was called with %q", s)
	default:
	}
	if !strings.Contains(logs.String(), "request blocked (reason=audit kind=closed)") {
		t.Errorf("logs %s", logs.String())
	}
}

// check-config and policy check read the config; they never create the
// decision log or its directory.
func TestChecksCreateNoDecisionLog(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	_, cfg := testDir(t, cfgSpec{Audit: "{ file: " + filepath.Join(state, "d.jsonl") + " }"})
	if code, _, errOut := runCLI("", "check-config", "-config", cfg); code != 0 {
		t.Fatalf("check-config: %d %s", code, errOut)
	}
	code, out, errOut := runCLI("", "policy", "check", "-config", cfg)
	if code != 0 || !strings.Contains(out, "audit        enabled, "+filepath.Join(state, "d.jsonl")+" (created by serve), on_error warn") {
		t.Fatalf("policy check: %d\n%s%s", code, out, errOut)
	}
	if _, err := os.Lstat(state); !os.IsNotExist(err) {
		t.Errorf("a check created %s: %v", state, err)
	}
}

func TestPolicyCheckReportsAudit(t *testing.T) {
	cfg := initPolicy(t, "-profile", "strict")
	code, out, _ := runCLI("", "policy", "check", "-config", cfg)
	for _, want := range []string{
		"on_error block, rotated at 50 MB, rotated files kept 90 days; tamper-evident, not tamper-proof",
		"is recorded before anything is forwarded, in a hash-chained decision log; a decision that cannot be recorded blocks the request",
		"Syslog output and an external anchor",
	} {
		if code != 0 || !strings.Contains(out, want) {
			t.Errorf("strict: lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "A persisted, tamper-evident decision log (ADR 0001, phase 2)") {
		t.Error("the decision log is still listed as not implemented")
	}

	_, off := testDir(t, cfgSpec{Audit: "{ enabled: false }"})
	if _, out, _ := runCLI("", "policy", "check", "-config", off); !strings.Contains(out, "audit        disabled") ||
		!strings.Contains(out, "WARNING: audit.enabled is false") {
		t.Errorf("disabled:\n%s", out)
	}
	_, warn := testDir(t, cfgSpec{Audit: "{ file: d.jsonl, on_error: warn }"})
	if _, out, _ := runCLI("", "policy", "check", "-config", warn); !strings.Contains(out, "WARNING: audit.on_error is warn (UNSAFE)") {
		t.Errorf("warn:\n%s", out)
	}
	// A symlink at audit.file is refused, as serve would.
	dir, link := testDir(t, cfgSpec{Audit: "{ file: link.jsonl }"})
	if err := os.Symlink(filepath.Join(dir, "elsewhere"), filepath.Join(dir, "link.jsonl")); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := runCLI("", "policy", "check", "-config", link); code != 1 || !strings.Contains(errOut, "symlink") {
		t.Errorf("symlink: code %d err %q", code, errOut)
	}
	if _, err := NewApp(AppOptions{ConfigPath: link, Logf: (&logBuf{}).logf}); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Errorf("serve with a symlinked audit.file: %v", err)
	}
}

func TestReloadKeepsAuditSettings(t *testing.T) {
	up := newUpstream(t, nil)
	spec := cfgSpec{Upstream: up.url()}
	_, cfg := testDir(t, spec)
	logs := &logBuf{}
	a := startApp(t, cfg, logs, nil)
	before := a.cur.Load().cfg.Audit
	spec.Audit = "{ file: other.jsonl, on_error: block, max_size_mb: 5, retention_days: 0, enabled: false }"
	writeFile(t, cfg, spec.render())
	if err := a.Reload(); err != nil {
		t.Fatal(err)
	}
	after := a.cur.Load().cfg.Audit
	if after.File != before.File || after.OnError != before.OnError || after.MaxBytes() != before.MaxBytes() ||
		after.Retention() != before.Retention() || !after.IsEnabled() {
		t.Errorf("audit settings applied on reload: %+v", after)
	}
	if !strings.Contains(logs.String(), "WARN restart required for: audit.enabled, audit.file, audit.on_error, audit.max_size_mb, audit.retention_days") {
		t.Errorf("logs:\n%s", logs.String())
	}
}

func TestStartupLogsAuditDisabledOrWarn(t *testing.T) {
	up := newUpstream(t, nil)
	_, cfg := testDir(t, cfgSpec{Upstream: up.url(), Audit: "{ enabled: false }"})
	logs := &logBuf{}
	startApp(t, cfg, logs, nil)
	if !strings.Contains(logs.String(), warnAuditOff) {
		t.Errorf("logs:\n%s", logs.String())
	}
	if !strings.Contains(logs.String(), "WARN audit.enabled=false") {
		t.Error("audit disabled is not a WARN")
	}
	_, cfg2 := testDir(t, cfgSpec{Upstream: up.url()})
	logs2 := &logBuf{}
	startApp(t, cfg2, logs2, nil)
	if !strings.Contains(logs2.String(), "WARN audit.on_error=warn: UNSAFE") {
		t.Errorf("logs:\n%s", logs2.String())
	}
}

// The commented audit block in examples/config.yaml is valid once
// uncommented, and states the defaults.
func TestExampleAuditBlockValid(t *testing.T) {
	src := readFile(t, filepath.Join("..", "..", "examples", "config.yaml"))
	var out []string
	in := false
	for _, ln := range strings.Split(src, "\n") {
		switch {
		case ln == "# audit:":
			in, ln = true, "audit:"
		case in && strings.HasPrefix(ln, "#   ") && strings.Contains(ln, ":") && !strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(ln, "#")), "#"):
			ln = "  " + strings.TrimPrefix(ln, "#   ")
		case in && !strings.HasPrefix(ln, "#"):
			in = false
		}
		out = append(out, ln)
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "hosts.txt"), "x.internal\n")
	p := filepath.Join(dir, "config.yaml")
	writeFile(t, p, strings.Join(out, "\n"))
	cfg, _, err := loadRules(p)
	if err != nil {
		t.Fatalf("uncommented example: %v\n%s", err, strings.Join(out, "\n"))
	}
	a := cfg.Audit
	if !a.IsEnabled() || a.OnError != "warn" || a.MaxBytes() != 50<<20 || a.Retention() != 90 || !strings.HasSuffix(a.File, "/cui-proxy/decisions.jsonl") {
		t.Errorf("uncommented example audit = %+v", a)
	}
}
