package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func initPolicy(t *testing.T, args ...string) (cfg string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "cfg")
	full := append([]string{"policy", "init", "-dir", dir}, args...)
	if code, _, errOut := runCLI("", full...); code != 0 {
		t.Fatalf("%q: code %d, stderr %q", full, code, errOut)
	}
	return filepath.Join(dir, "config.yaml")
}

func TestPolicyInitWritesProfile(t *testing.T) {
	for profile, want := range map[string]string{"strict": "profile: strict", "sanitize": "profile: sanitize"} {
		cfg := initPolicy(t, "-profile", profile)
		if !strings.Contains(readFile(t, cfg), want) {
			t.Errorf("-profile %s: config lacks %q", profile, want)
		}
		if code, out, errOut := runCLI("", "check-config", "-config", cfg); code != 0 || out != "ok: 3 rules\n" {
			t.Errorf("-profile %s: check-config: code %d, out %q, err %q", profile, code, out, errOut)
		}
	}
	// No -profile means the default, which is written out explicitly.
	if cfg := initPolicy(t); !strings.Contains(readFile(t, cfg), "profile: sanitize") {
		t.Error("default init does not state the profile")
	}
}

func TestInitAcceptsProfileToo(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cfg")
	if code, _, errOut := runCLI("", "init", "-dir", dir, "-profile", "strict"); code != 0 {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	if !strings.Contains(readFile(t, filepath.Join(dir, "config.yaml")), "profile: strict") {
		t.Error("init -profile strict did not write the strict profile")
	}
}

func TestPolicyInitRefusesBadProfileAndOverwrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cfg")
	if code, _, errOut := runCLI("", "policy", "init", "-dir", dir, "-profile", "lenient"); code != 2 || !strings.Contains(errOut, "lenient") {
		t.Errorf("bad profile: code %d, stderr %q; want 2 naming it", code, errOut)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("a bad profile still created %s", dir)
	}
	cfg := initPolicy(t, "-profile", "strict")
	other := filepath.Dir(cfg)
	if code, _, _ := runCLI("", "policy", "init", "-dir", other); code != 1 {
		t.Errorf("second init without -force: exit %d, want 1", code)
	}
	if !strings.Contains(readFile(t, cfg), "profile: strict") {
		t.Error("a refused init changed the existing file")
	}
}

func TestPolicyCheckStrict(t *testing.T) {
	cfg := initPolicy(t, "-profile", "strict")
	code, out, errOut := runCLI("", "policy", "check", "-config", cfg)
	if code != 0 {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out, errOut)
	}
	for _, want := range []string{
		"Profile", "strict",
		"RULE", "ACTION", "builtin", "customer-hosts", "aws-keys",
		"Every detection blocks the request",
		"Content the proxy cannot inspect is refused",
		"Request headers and the query string are scanned",
		"protocol identifiers", // strict also checks the fields normally passed through unscanned
		"even under on_error: passthrough",
		"Not scanned, by design",
		"Authorization", "encrypted_content",
		"every number in the request is checked",
		"Not implemented yet",
		"approval", "decision log",
		"Not something this tool can do",
		"Result: OK",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if (strings.HasPrefix(line, "builtin") || strings.HasPrefix(line, "customer-hosts") || strings.HasPrefix(line, "aws-keys")) && !strings.Contains(line, "block") {
			t.Errorf("under strict every rule must show block: %q", line)
		}
	}
	if strings.Contains(out, "WARNING") {
		t.Errorf("a strict policy should have no warnings:\n%s", out)
	}
}

func TestPolicyCheckSanitizeWarnsAndShowsPerRuleActions(t *testing.T) {
	_, cfg := testDir(t, cfgSpec{Rules: rulesBlockHosts})
	code, out, _ := runCLI("", "policy", "check", "-config", cfg)
	if code != 0 {
		t.Fatalf("code %d: %s", code, out)
	}
	if !strings.Contains(out, "masked and restored") || !strings.Contains(out, "WARNING") || !strings.Contains(out, "masked, not refused") {
		t.Errorf("a sanitize policy must say plainly that detections are masked, not refused:\n%s", out)
	}
	rows := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) == 3 && (f[0] == "builtin" || f[0] == "hosts-a") {
			rows[f[0]] = f[2]
		}
	}
	if rows["builtin"] != "sanitize" || rows["hosts-a"] != "block" {
		t.Errorf("effective actions = %v; want builtin sanitize, hosts-a block", rows)
	}
}

func TestPolicyCheckWarnsAboutPassthrough(t *testing.T) {
	_, cfg := testDir(t, cfgSpec{OnError: "passthrough"})
	code, out, errOut := runCLI("", "policy", "check", "-config", cfg)
	if code != 0 || !strings.Contains(out+errOut, "UNSAFE") {
		t.Errorf("code %d; passthrough must be flagged UNSAFE:\n%s%s", code, out, errOut)
	}
}

func TestPolicyCheckRequireStrict(t *testing.T) {
	strict := initPolicy(t, "-profile", "strict")
	if code, _, errOut := runCLI("", "policy", "check", "-config", strict, "-require-strict"); code != 0 {
		t.Errorf("strict config with -require-strict: code %d, stderr %q", code, errOut)
	}
	_, loose := testDir(t, cfgSpec{})
	code, _, errOut := runCLI("", "policy", "check", "-config", loose, "-require-strict")
	if code != 1 || !strings.Contains(errOut, "strict") {
		t.Errorf("sanitize config with -require-strict: code %d, stderr %q; want 1 naming strict", code, errOut)
	}
}

func TestPolicyCheckInvalidAndUsage(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "bad.yaml")
	writeFile(t, bad, "policy: { profile: lenient }\n")
	if code, _, errOut := runCLI("", "policy", "check", "-config", bad); code != 1 || !strings.Contains(errOut, "policy.profile") {
		t.Errorf("invalid config: code %d, stderr %q", code, errOut)
	}
	for _, args := range [][]string{{"policy"}, {"policy", "bogus"}, {"policy", "check"}, {"policy", "check", "-config", "x", "extra"}} {
		if code, _, errOut := runCLI("", args...); code != 2 || errOut == "" {
			t.Errorf("%q: exit %d, stderr %q; want 2 with a message", args, code, errOut)
		}
	}
	if _, out, _ := runCLI("", "help"); !strings.Contains(out, "policy") {
		t.Errorf("usage does not list policy:\n%s", out)
	}
}

func TestTestRulesReportsPolicyBlock(t *testing.T) {
	cfg := initPolicy(t, "-profile", "strict")
	code, out, errOut := runCLI("ssh admin@10.20.30.15\n", "test-rules", "-config", cfg)
	if code != 1 || out != "" {
		t.Fatalf("code %d, stdout %q; want 1 and no sanitized output", code, out)
	}
	if !strings.Contains(errOut, "policy") || !strings.Contains(errOut, "rule=") || strings.Contains(errOut, "10.20.30.15") {
		t.Errorf("stderr must say it was blocked by policy and by which rule, without the value: %q", errOut)
	}
}

// The active profile is logged on every start and reload; a reload that
// weakens the policy is a loud, explicit WARN (ADR 0001: weaker is an
// explicit, logged opt-in).
func TestStartupAndReloadLogThePolicy(t *testing.T) {
	up := newUpstream(t, nil)
	strictSpec := cfgSpec{Upstream: up.url(), Profile: "strict"}
	_, cfg := testDir(t, strictSpec)
	logs := &logBuf{}
	a := startApp(t, cfg, logs, nil)
	if !strings.Contains(logs.String(), "policy: profile=strict blocking_rules=2 of 2") {
		t.Errorf("startup does not log the profile:\n%s", logs.String())
	}
	if strings.Contains(logs.String(), "weakened") {
		t.Errorf("a first start is not a weakening:\n%s", logs.String())
	}

	// strict -> sanitize
	writeFile(t, cfg, cfgSpec{Upstream: up.url(), Profile: "sanitize"}.render())
	if err := a.Reload(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"policy: profile=sanitize blocking_rules=0 of 2",
		"WARN policy weakened by reload: profile strict -> sanitize",
	} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("reload log lacks %q:\n%s", want, logs.String())
		}
	}

	// sanitize -> strict is a tightening: no warning.
	n := strings.Count(logs.String(), "weakened")
	writeFile(t, cfg, strictSpec.render())
	if err := a.Reload(); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(logs.String(), "weakened"); got != n {
		t.Errorf("tightening the policy logged a weakening:\n%s", logs.String())
	}

	// Staying on sanitize but dropping a rule's action: block is a weakening too.
	writeFile(t, cfg, cfgSpec{Upstream: up.url(), Rules: rulesBlockHosts}.render())
	if err := a.Reload(); err != nil {
		t.Fatal(err)
	}
	writeFile(t, cfg, cfgSpec{Upstream: up.url()}.render())
	if err := a.Reload(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "WARN policy weakened by reload: no longer blocking: hosts-a") {
		t.Errorf("removing a blocking rule is not reported by name:\n%s", logs.String())
	}

	// Swapping WHICH rule blocks, at an equal count, is a weakening too.
	writeFile(t, cfg, cfgSpec{Upstream: up.url(), Rules: rulesBlockHosts}.render())
	if err := a.Reload(); err != nil {
		t.Fatal(err)
	}
	before := strings.Count(logs.String(), "no longer blocking: hosts-a") // the earlier step logged it once
	swapped := strings.Replace(strings.Replace(rulesBlockHosts, "    action: block\n", "", 1),
		"  - name: builtin\n    type: builtin\n", "  - name: builtin\n    type: builtin\n    action: block\n", 1)
	writeFile(t, cfg, cfgSpec{Upstream: up.url(), Rules: swapped}.render())
	if err := a.Reload(); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(logs.String(), "no longer blocking: hosts-a"); got != before+1 {
		t.Errorf("swapping the blocking rule at an equal count is not reported (%d -> %d warnings):\n%s", before, got, logs.String())
	}
}

// Rule names come from the user's own config but may hold control characters:
// they must not reach the terminal raw.
func TestPolicyCheckAndTestRulesCleanRuleNames(t *testing.T) {
	_, cfg := testDir(t, cfgSpec{Profile: "strict"})
	body := strings.Replace(readFile(t, cfg), "name: builtin", "name: \"bad\\e[31mname\\tx\"", 1)
	writeFile(t, cfg, body)
	code, out, _ := runCLI("ssh 10.20.30.15\n", "policy", "check", "-config", cfg)
	if code != 0 || strings.Contains(out, "\x1b") {
		t.Errorf("policy check printed a control character: code %d %q", code, out)
	}
	code, _, errOut := runCLI("ssh 10.20.30.15\n", "test-rules", "-config", cfg)
	if code != 1 || strings.Contains(errOut, "\x1b") {
		t.Errorf("test-rules printed a control character: code %d %q", code, errOut)
	}
}

func TestPolicyCheckRefusesStrictWithoutRules(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "c.yaml")
	writeFile(t, cfg, "upstreams: {codex: 'https://chatgpt.com/backend-api/codex'}\npolicy: { profile: strict }\nrules: []\n")
	code, _, errOut := runCLI("", "policy", "check", "-config", cfg, "-require-strict")
	if code != 1 || !strings.Contains(errOut, "rule") {
		t.Errorf("strict with no rules: code %d, stderr %q; want it refused", code, errOut)
	}
}
