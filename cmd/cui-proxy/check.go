package main

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"
)

// runCheck validates a config the way serve would: schema, synth classes,
// rule files, and every configured upstream URL. It warns on stderr, as
// serve logs, when policy.on_error is passthrough.
func runCheck(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("check-config", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "path to the YAML config")
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
	if cfg.Policy.OnError == "passthrough" {
		fmt.Fprintln(stderr, warnPassthrough)
	}
	fmt.Fprintf(stdout, "ok: %d rules\n", len(cfg.Rules))
	return exitOK
}
