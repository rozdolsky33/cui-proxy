package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"text/tabwriter"

	"cui-proxy/internal/config"
)

const policyUsage = `usage: cui-proxy policy <init|check> [flags]

  init [-profile sanitize|strict] [-dir DIR] [-force]
        write a starter config and dictionary (same as cui-proxy init)
  check -config PATH [-require-strict]
        validate the policy and say what is, and is not, enforced
`

// runPolicy dispatches the policy subcommands (ADR 0001).
func runPolicy(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return usageError(stderr, "policy needs a subcommand\n"+policyUsage)
	}
	switch args[0] {
	case "init":
		return runInit(args[1:], stdout, stderr)
	case "check":
		return runPolicyCheck(args[1:], stdout, stderr)
	}
	return usageError(stderr, fmt.Sprintf("policy: unknown subcommand %q\n%s", args[0], policyUsage))
}

// runPolicyCheck validates the config the way serve would, then prints the
// effective policy and a plain statement of what is enforced, what is not
// implemented yet, and what no local tool can do. It changes nothing.
// cleanName makes a config-supplied name safe to print to a terminal: bytes
// outside [A-Za-z0-9._-] become '_' and the result is cut to 64 bytes.
func cleanName(s string) string {
	if len(s) > 64 {
		s = s[:64]
	}
	b := []byte(s)
	for i, c := range b {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			b[i] = '_'
		}
	}
	return string(b)
}

// describeAudit is the one-line audit status for policy check. It only looks
// at the path (Lstat): nothing is created or opened for writing. A path serve
// would refuse is an error.
func describeAudit(cfg *config.Config) (string, error) {
	a := cfg.Audit
	if !a.IsEnabled() {
		return "disabled (no decision log)", nil
	}
	if a.File == "" {
		return "", errors.New("audit.file is not set and no home directory was found")
	}
	state := "exists"
	switch fi, err := os.Lstat(a.File); {
	case errors.Is(err, fs.ErrNotExist):
		state = "created by serve"
	case err != nil:
		return "", fmt.Errorf("audit.file: %w", err)
	case fi.Mode()&fs.ModeSymlink != 0:
		return "", fmt.Errorf("audit.file %s is a symlink; serve refuses it", a.File)
	case !fi.Mode().IsRegular():
		return "", fmt.Errorf("audit.file %s is not a regular file", a.File)
	}
	onError := config.AuditWarn
	if cfg.AuditBlocks() {
		onError = config.AuditBlock
	}
	retention := "rotated files kept forever"
	if d := a.Retention(); d > 0 {
		retention = fmt.Sprintf("rotated files kept %d days", d)
	}
	return fmt.Sprintf("enabled, %s (%s), on_error %s, rotated at %d MB, %s; tamper-evident, not tamper-proof",
		a.File, state, onError, a.MaxBytes()>>20, retention), nil
}

func runPolicyCheck(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("policy check", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "path to the YAML config")
	requireStrict := fs.Bool("require-strict", false, "exit 1 unless policy.profile is strict (for scripts and audits)")
	if code, ok := parseFlags(fs, args, stderr, cfgPath); !ok {
		return code
	}
	abs, err := filepath.Abs(*cfgPath)
	if err != nil {
		return fail(stderr, err)
	}
	cfg, _, err := loadRules(abs)
	if err != nil {
		return fail(stderr, err)
	}
	if _, err := parseUpstreams(cfg.Upstreams); err != nil {
		return fail(stderr, err)
	}
	profile := cfg.Policy.Profile
	if profile == "" {
		profile = config.ProfileSanitize
	}

	fmt.Fprintf(stdout, "cui-proxy policy check: %s\n\n", abs)
	fmt.Fprintf(stdout, "Profile      %s\nunsupported  %s\non_error     %s\n", profile, cfg.Policy.Unsupported, cfg.Policy.OnError)
	audit, err := describeAudit(cfg)
	if err != nil {
		return fail(stderr, fmt.Errorf("policy check: %w", err))
	}
	fmt.Fprintf(stdout, "audit        %s\n\n", audit)

	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "RULE\tTYPE\tACTION")
	anySanitize, anyBlock := false, false
	for _, r := range cfg.Rules {
		a := config.EffectiveAction(profile, r)
		if a == config.ActionSanitize {
			anySanitize = true
		} else {
			anyBlock = true
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", cleanName(r.Name), cleanName(r.Type), a)
	}
	tw.Flush()

	fmt.Fprintln(stdout, "\nEnforced now:")
	if anySanitize {
		fmt.Fprintln(stdout, "  - Detected values are masked and restored in the reply; a rule with action: block refuses the request instead.")
	}
	if profile == config.ProfileStrict || !anySanitize {
		fmt.Fprintln(stdout, "  - Every detection blocks the request before anything is forwarded.")
	}
	if anyBlock {
		fmt.Fprintln(stdout, "  - Because a rule blocks, protocol identifiers (model, ids, role, tool names, include entries) are checked too, and every number in the request is checked, not just text.")
	}
	fmt.Fprintln(stdout, "  - Content the proxy cannot inspect is refused, even under on_error: passthrough: images, files, compressed bodies, unknown routes, oversized, too deeply nested or unparseable bodies.")
	fmt.Fprintln(stdout, "  - Request headers and the query string are scanned; a match blocks the request.")
	fmt.Fprintln(stdout, "  - The upstream must be https (loopback excepted); redirects from it are never relayed.")
	if cfg.Audit.IsEnabled() {
		onFail := "a decision that cannot be recorded blocks the request (audit.on_error: block)"
		if !cfg.AuditBlocks() {
			onFail = "a decision that cannot be recorded is logged as a WARN and the request still goes (audit.on_error: warn)"
		}
		fmt.Fprintf(stdout, "  - Every request's decision (forwarded, sanitized or blocked, with the reason and rule names, never values) is recorded before anything is forwarded, in a hash-chained decision log; %s. Check it with: cui-proxy audit verify -config %s\n", onFail, abs)
		fmt.Fprintf(stdout, "    %s\n", tamperNote)
	}

	fmt.Fprintln(stdout, "\nNot scanned, by design:")
	fmt.Fprintln(stdout, "  - The Authorization, Cookie and Chatgpt-Account-Id headers: they are credentials and are forwarded as they are.")
	fmt.Fprintln(stdout, "  - The content of encrypted_content (the vendor's encrypted reasoning blobs, resent as received); while a rule blocks it must still look like base64 ciphertext, so text cannot ride in it.")

	fmt.Fprintln(stdout, "\nNot implemented yet (see docs/adr):")
	fmt.Fprintln(stdout, "  - Verified approval records (ADR 0002 is waiting on the approving authority).")
	fmt.Fprintln(stdout, "  - Syslog output and an external anchor for the decision log's hash chain (ADR 0001, phase 2 follow-up); ship the file yourself for now.")
	fmt.Fprintln(stdout, "  - Approved upstream and account allowlists, and a lock switch (ADR 0001, phase 4).")

	fmt.Fprintln(stdout, "\nNot something this tool can do:")
	fmt.Fprintln(stdout, "  - Recognize everything sensitive: patterns and dictionaries miss things.")
	fmt.Fprintln(stdout, "  - Stop anyone from running a client without the proxy.")
	fmt.Fprintln(stdout, "  - Review output or record that a person did.")

	warnings := 0
	warn := func(msg string) {
		if warnings == 0 {
			fmt.Fprintln(stdout)
		}
		warnings++
		fmt.Fprintf(stdout, "WARNING: %s\n", msg)
	}
	if anySanitize {
		warn("some rules only mask: detections are masked, not refused. For a policy that forbids sending restricted data at all, use the strict profile (cui-proxy policy init -profile strict).")
	}
	if cfg.Policy.OnError == "passthrough" {
		warn("policy.on_error is passthrough (UNSAFE): when a request cannot be sanitized, the original is forwarded.")
	}
	if len(cfg.Rules) == 0 {
		warn("there are no rules, so nothing is detected.")
	}
	if !cfg.Audit.IsEnabled() {
		warn("audit.enabled is false: no decision log is kept.")
	} else if !cfg.AuditBlocks() {
		warn("audit.on_error is warn (UNSAFE): a request is forwarded even when its decision cannot be recorded.")
	}
	if warnings == 0 {
		fmt.Fprintln(stdout, "\nResult: OK")
	} else {
		fmt.Fprintf(stdout, "\nResult: OK, with %d warning(s)\n", warnings)
	}

	if *requireStrict && profile != config.ProfileStrict {
		return fail(stderr, fmt.Errorf("policy check: policy.profile is %q but strict is required", profile))
	}
	return exitOK
}
