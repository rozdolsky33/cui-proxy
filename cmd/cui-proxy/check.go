package main

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"
)

// runCheck validates a config the way serve would: schema, synth classes,
// rule files, and every configured upstream URL.
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
	fmt.Fprintf(stdout, "ok: %d rules\n", len(cfg.Rules))
	return exitOK
}
