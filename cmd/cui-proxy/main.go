// Command cui-proxy is a local fail-closed proxy that replaces sensitive
// values in Codex CLI traffic with synthetic ones and restores them in the
// responses.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

// Exit codes.
const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

const usage = `usage: cui-proxy <command> [flags]

commands:
  init [-dir PATH] [-force] [-profile P]  write a starter config and dictionary
  policy init|check                    write or check a policy (sanitize or strict profile)
  serve -config PATH [-adapter NAME]   run the proxy and the dashboard
  check-config -config PATH [-adapter NAME]  validate a config and its rule files
  audit verify [-file P | -config P]   check the decision log's hash chain
  explain [-config P] REQUEST-ID       say why a request was forwarded, sanitized or blocked
  info -config PATH                    print the proxy and dashboard URLs
  test-rules -config PATH              sanitize stdin with the rules (throwaway vault)
  menubar install|uninstall|status     macOS: menu-bar app that runs the proxy in the background
  version                              print the version
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// run dispatches a command and returns the process exit code.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitUsage
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "init":
		return runInit(rest, stdout, stderr)
	case "serve":
		return runServe(rest, stderr)
	case "check-config":
		return runCheck(rest, stdout, stderr)
	case "policy":
		return runPolicy(rest, stdout, stderr)
	case "audit":
		return runAudit(rest, stdout, stderr)
	case "explain":
		return runExplain(rest, stdout, stderr)
	case "info":
		return runInfo(rest, stdout, stderr)
	case "menubar":
		return runMenubar(rest, stdout, stderr)
	case "test-rules":
		return runTestRules(rest, stdin, stdout, stderr)
	case "version":
		if len(rest) != 0 {
			return usageError(stderr, "version takes no arguments")
		}
		fmt.Fprintf(stdout, "cui-proxy %s\n", version)
		return exitOK
	case "help", "-h", "-help", "--help":
		fmt.Fprint(stdout, usage)
		return exitOK
	}
	return usageError(stderr, fmt.Sprintf("unknown command %q", cmd))
}

func usageError(stderr io.Writer, msg string) int {
	fmt.Fprintf(stderr, "cui-proxy: %s\n%s", msg, usage)
	return exitUsage
}

// fail prints an error as "cui-proxy: <message>" and returns exitFailure.
// Callers pass only errors that carry no request data or dictionary text.
func fail(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "cui-proxy: %v\n", err)
	return exitFailure
}

// parseFlags parses a subcommand's flags. It returns ok=false with the exit
// code when the command must stop (usage error or -h).
func parseFlags(fs *flag.FlagSet, args []string, stderr io.Writer, needConfig *string) (int, bool) {
	return parseFlagsN(fs, args, stderr, needConfig, 0)
}

// parseFlagsN is parseFlags for a command that takes exactly nargs
// positional arguments after its flags.
func parseFlagsN(fs *flag.FlagSet, args []string, stderr io.Writer, needConfig *string, nargs int) (int, bool) {
	// The flag package's own messages lack the cui-proxy prefix: discard
	// them and print our own instead.
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	printDefaults := func() {
		fmt.Fprintf(stderr, "usage of %s:\n", fs.Name())
		fs.SetOutput(stderr)
		fs.PrintDefaults()
		fs.SetOutput(io.Discard)
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printDefaults()
			return exitOK, false
		}
		fmt.Fprintf(stderr, "cui-proxy: %s: %v\n", fs.Name(), err)
		printDefaults()
		return exitUsage, false
	}
	if fs.NArg() > nargs {
		return usageError(stderr, fmt.Sprintf("%s: unexpected argument %q", fs.Name(), fs.Arg(nargs))), false
	}
	if fs.NArg() < nargs {
		return usageError(stderr, fmt.Sprintf("%s: missing argument", fs.Name())), false
	}
	if needConfig != nil && *needConfig == "" {
		return usageError(stderr, fs.Name()+": -config is required"), false
	}
	return 0, true
}
