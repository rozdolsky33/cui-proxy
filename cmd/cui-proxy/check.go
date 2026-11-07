package main

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"sort"
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
	names := make([]string, 0, len(cfg.Upstreams))
	for name := range cfg.Upstreams {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, err := parseUpstream(name, cfg.Upstreams[name]); err != nil {
			return fail(stderr, err)
		}
	}
	fmt.Fprintf(stdout, "ok: %d rules\n", len(cfg.Rules))
	return exitOK
}
