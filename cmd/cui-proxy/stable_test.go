package main

import (
	"bytes"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// stableCommands is the command-line surface declared stable in
// docs/compatibility.md: every command with its flags. Adding a command or a
// flag is a compatible change. Removing or renaming one breaks scripts and
// service definitions and must follow the deprecation rules in that document.
// Update this table in the same change.
var stableCommands = map[string][]string{
	"init":              {"dir", "force", "profile"},
	"policy init":       {"dir", "force", "profile"},
	"policy check":      {"config", "require-strict"},
	"serve":             {"adapter", "config"},
	"check-config":      {"adapter", "config"},
	"audit verify":      {"config", "file"},
	"explain":           {"config", "file"},
	"preflight":         {"config"},
	"info":              {"config"},
	"test-rules":        {"config"},
	"menubar install":   {"binary", "config", "dir", "no-launch"},
	"menubar uninstall": {"dir"},
	"menubar status":    {"dir"},
}

var helpFlag = regexp.MustCompile(`(?m)^  -([a-z][a-z-]*)`)

func TestStableCommandSurface(t *testing.T) {
	for cmd, want := range stableCommands {
		var stdout, stderr bytes.Buffer
		args := append(strings.Fields(cmd), "-h")
		if code := run(args, strings.NewReader(""), &stdout, &stderr); code != exitOK {
			t.Errorf("%s -h: exit %d: stable command removed or renamed (see docs/compatibility.md)", cmd, code)
			continue
		}
		var got []string
		for _, m := range helpFlag.FindAllStringSubmatch(stderr.String(), -1) {
			got = append(got, m[1])
		}
		slices.Sort(got)
		for _, f := range want {
			if !slices.Contains(got, f) {
				t.Errorf("%s: stable flag -%s removed or renamed (see docs/compatibility.md)", cmd, f)
			}
		}
		for _, f := range got {
			if !slices.Contains(want, f) {
				t.Errorf("%s: flag -%s is not recorded in stableCommands (adding a flag is compatible; record it here)", cmd, f)
			}
		}
	}
	for _, cmd := range []string{"version", "help"} {
		var stdout, stderr bytes.Buffer
		if code := run([]string{cmd}, strings.NewReader(""), &stdout, &stderr); code != exitOK {
			t.Errorf("%s: exit %d: stable command removed or renamed", cmd, code)
		}
	}
}

func TestStableExitCodes(t *testing.T) {
	if exitOK != 0 || exitFailure != 1 || exitUsage != 2 {
		t.Errorf("exit codes are a stable interface: got ok=%d failure=%d usage=%d, want 0, 1, 2", exitOK, exitFailure, exitUsage)
	}
}
