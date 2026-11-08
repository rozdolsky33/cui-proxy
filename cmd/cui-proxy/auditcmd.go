package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"cui-proxy/internal/attachment"
	"cui-proxy/internal/config"
	"cui-proxy/internal/decisionlog"
	"cui-proxy/internal/proxy"
)

const auditUsage = `usage: cui-proxy audit verify [-file PATH | -config PATH]

  verify  check the decision log's hash chain (active and rotated files);
          exit 0 if intact, 1 on any break or read error
`

// tamperNote is printed by audit verify and policy check: what the chain
// shows, and what it cannot.
const tamperNote = "The log is tamper-evident, not tamper-proof: a verified chain shows that no line was edited, reordered or removed in the middle, but not a full rewrite by someone who can recompute every hash, nor the removal of the newest lines."

// runAudit dispatches the audit subcommands.
func runAudit(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return usageError(stderr, "audit needs a subcommand\n"+auditUsage)
	}
	switch args[0] {
	case "verify":
		return runAuditVerify(args[1:], stdout, stderr)
	}
	return usageError(stderr, fmt.Sprintf("audit: unknown subcommand %q\n%s", args[0], auditUsage))
}

// logFlags are the flags that locate the decision log.
type logFlags struct{ file, config *string }

func addLogFlags(fs *flag.FlagSet) logFlags {
	return logFlags{
		file:   fs.String("file", "", "the decision log file (default: audit.file from -config, else "+displayDefaultAuditFile()+")"),
		config: fs.String("config", "", "read audit.file from this config"),
	}
}

func displayDefaultAuditFile() string {
	if p := config.DefaultAuditFile(); p != "" {
		return p
	}
	return "$XDG_STATE_HOME/cui-proxy/decisions.jsonl"
}

// path resolves the decision log path. It reads the config (never the
// rules' files) and creates nothing.
func (f logFlags) path() (string, error) {
	switch {
	case *f.file != "" && *f.config != "":
		return "", errors.New("use -file or -config, not both")
	case *f.file != "":
		return filepath.Abs(*f.file)
	case *f.config != "":
		abs, err := filepath.Abs(*f.config)
		if err != nil {
			return "", err
		}
		cfg, err := config.Load(abs)
		if err != nil {
			return "", err
		}
		if !cfg.Audit.IsEnabled() {
			return "", fmt.Errorf("audit is disabled in %s (audit.enabled: false); use -file to read a log", abs)
		}
		resolveAuditFile(cfg, abs)
		if cfg.Audit.File == "" {
			return "", errors.New("audit.file is not set and no home directory was found")
		}
		return cfg.Audit.File, nil
	}
	p := config.DefaultAuditFile()
	if p == "" {
		return "", errors.New("no home directory: use -file or -config")
	}
	return p, nil
}

var breakMeaning = map[string]string{
	decisionlog.BreakMalformed: "not a valid record line",
	decisionlog.BreakHash:      "the record does not match its hash: it was edited",
	decisionlog.BreakPrev:      "does not continue the previous record: a line before it was removed, inserted or reordered",
	decisionlog.BreakTruncated: "the last line is cut off",
	decisionlog.BreakHeadCut:   "the first record does not start the chain and no retention note explains it: records or files were removed from the start of the log",
}

func runAuditVerify(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("audit verify", flag.ContinueOnError)
	lf := addLogFlags(fs)
	if code, ok := parseFlags(fs, args, stderr, nil); !ok {
		return code
	}
	path, err := lf.path()
	if err != nil {
		if *lf.file != "" && *lf.config != "" {
			return usageError(stderr, "audit verify: "+err.Error())
		}
		return fail(stderr, fmt.Errorf("audit verify: %w", err))
	}
	fmt.Fprintf(stdout, "cui-proxy audit verify: %s\n\n", path)
	rep, err := decisionlog.Verify(path)
	if rep == nil {
		return fail(stderr, fmt.Errorf("audit verify: %w", err))
	}
	fmt.Fprintln(stdout, "Files, in chain order:")
	for _, f := range rep.Files {
		fmt.Fprintf(stdout, "  %s\n", f)
	}
	if !slices.Contains(rep.Files, path) {
		fmt.Fprintf(stdout, "  (the active file %s does not exist)\n", path)
	}
	fmt.Fprintf(stdout, "\nRecords checked  %d\n", rep.Records)
	switch {
	case rep.Records == 0:
		fmt.Fprintln(stdout, "Trusted start    (no record)")
	case rep.Start == decisionlog.Genesis:
		fmt.Fprintln(stdout, "Trusted start    genesis (the chain is complete from its first record)")
	case rep.StartNote != nil:
		fmt.Fprintf(stdout, "Trusted start    %s\n                 (the first record of %s continues %s, which retention deleted at %s; the chain is verified from here)\n", rep.Start, rep.StartFile, rep.StartNote.File, rep.StartNote.TS)
	default:
		fmt.Fprintf(stdout, "Start            %s (not genesis, and no retention note explains it)\n", rep.Start)
	}
	if rep.Head != "" {
		fmt.Fprintf(stdout, "Head hash        %s (%s line %d)\n", rep.Head, rep.HeadFile, rep.HeadLine)
	}
	if len(rep.Breaks) > 0 {
		b := rep.Breaks[0]
		fmt.Fprintf(stdout, "\nFIRST BREAK: %s line %d: %s (%s)\n", b.File, b.Line, b.Kind, breakMeaning[b.Kind])
		for _, b := range rep.Breaks[1:] {
			fmt.Fprintf(stdout, "  later break: %s line %d: %s\n", b.File, b.Line, b.Kind)
		}
	}
	for _, g := range rep.Gaps {
		fmt.Fprintf(stdout, "GAP: the chain resumes at %s line %d after %s line %d; %d line(s) in between are not part of it\n", g.File, g.Line, g.AfterFile, g.AfterLine, g.Skipped)
	}
	if n := rep.Unverified; n > 0 {
		fmt.Fprintf(stdout, "Unverified lines %d\n", n)
	}
	if rep.InFlight {
		fmt.Fprintln(stdout, "Note: the active file ends in a partial line while serve is running: a record being written, not counted. Run verify again to check it.")
	}
	fmt.Fprintf(stdout, "\n%s\n", tamperNote)
	if err != nil {
		return fail(stderr, fmt.Errorf("audit verify: %w", err))
	}
	if !rep.OK() {
		fmt.Fprintln(stdout, "\nResult: BROKEN")
		return exitFailure
	}
	fmt.Fprintln(stdout, "\nResult: OK")
	return exitOK
}

const explainUsage = "usage: cui-proxy explain [-config PATH | -file PATH] REQUEST-ID"

// runExplain reads the decision log and says, in plain words, what was
// decided for one request and what would change it. It changes nothing.
func runExplain(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("explain", flag.ContinueOnError)
	lf := addLogFlags(fs)
	if code, ok := parseFlagsN(fs, args, stderr, nil, 1); !ok {
		return code
	}
	id, err := decisionlog.ParseID(fs.Arg(0))
	if err != nil {
		return usageError(stderr, "explain: "+err.Error()+" (the request_id from the error message or the X-Cui-Proxy-Request-Id header)\n"+explainUsage)
	}
	path, err := lf.path()
	if err != nil {
		if *lf.file != "" && *lf.config != "" {
			return usageError(stderr, "explain: "+err.Error())
		}
		return fail(stderr, fmt.Errorf("explain: %w", err))
	}
	found, err := decisionlog.Find(path, id)
	if err != nil && len(found) == 0 {
		return fail(stderr, fmt.Errorf("explain: %w", err))
	}
	if len(found) == 0 {
		return fail(stderr, fmt.Errorf("explain: no record for request %s in %s (or its rotated files)", id, path))
	}
	f := found[len(found)-1]
	r := f.Record
	if f.Problem != "" {
		fmt.Fprintf(stdout, "WARNING: this record does NOT verify (%s: %s). The log was changed or damaged; do not rely on what follows. Run: cui-proxy audit verify\n\n", f.Problem, breakMeaning[f.Problem])
	}
	fmt.Fprintf(stdout, "Request %s\n  time      %s\n  adapter   %s\n  profile   %s\n  decision  %s\n", r.RequestID, r.TS, r.Adapter, r.Profile, r.Decision)
	if r.Reason != "" {
		fmt.Fprintf(stdout, "  reason    %s\n", r.Reason)
	}
	if len(r.RuleIDs) > 0 {
		parts := make([]string, 0, len(r.RuleIDs))
		for _, id := range r.RuleIDs {
			parts = append(parts, fmt.Sprintf("%s (%d)", id, r.Counts[id]))
		}
		fmt.Fprintf(stdout, "  rules     %s\n", strings.Join(parts, ", "))
	}
	fmt.Fprintln(stdout)
	passthrough := ""
	for k := range r.Counts {
		if rest, ok := strings.CutPrefix(k, decisionlog.PassthroughKey("")); ok {
			passthrough = rest
		}
	}
	switch {
	case r.Decision == decisionlog.Blocked:
		msg, change, ok := proxy.Explain(r.Reason)
		if !ok {
			msg, change = "blocked", "check the proxy log line"
		}
		fmt.Fprintf(stdout, "The request was blocked before anything was sent upstream: %s (reason=%s).\n", msg, r.Reason)
		if len(r.RuleIDs) > 0 {
			fmt.Fprintf(stdout, "Rule that matched: %s (the first blocking match; scanning stops there).\n", strings.Join(r.RuleIDs, ", "))
		}
		if r.Detail != "" {
			fmt.Fprintf(stdout, "Attachment detail: %s. %s\n", r.Detail, attachment.Explain(r.Detail))
		}
		fmt.Fprintf(stdout, "Why, and what would change it: %s\n", change)
	case passthrough != "":
		fmt.Fprintf(stdout, "The request was forwarded UNSANITIZED because policy.on_error is passthrough (UNSAFE): sanitizing failed (reason=%s).\n", passthrough)
		fmt.Fprintln(stdout, "What would change it: policy.on_error: block refuses such a request instead.")
	case r.Decision == decisionlog.Sanitized:
		if len(r.RuleIDs) > 0 {
			fmt.Fprintln(stdout, "The request was forwarded with the detected values masked; they were restored in the reply.")
			fmt.Fprintln(stdout, "What would change it: under the strict profile, or with action: block on these rules, it would have been blocked instead.")
		} else {
			fmt.Fprintln(stdout, "The request was rewritten before forwarding, for example by converting attachments to text; no detector match was recorded.")
		}
	default:
		fmt.Fprintln(stdout, "The request was forwarded unchanged: no rule matched.")
		fmt.Fprintln(stdout, "What would change it: a rule that matches its content (test with synthetic text: cui-proxy test-rules).")
	}
	fmt.Fprintf(stdout, "\nRecorded in %s line %d. The log holds no values: to see what matched, reproduce with synthetic text (cui-proxy test-rules). Check the log's integrity with: cui-proxy audit verify\n", f.File, f.Line)
	if len(found) > 1 {
		fmt.Fprintf(stdout, "Note: %d records carry this request id; the last is shown. Run cui-proxy audit verify.\n", len(found))
	}
	if f.Problem != "" {
		return exitFailure
	}
	fmt.Fprintln(stdout, "The record verified in place (its hash and its link to the record before it).")
	return exitOK
}
