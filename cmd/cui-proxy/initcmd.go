package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"cui-proxy/examples"
	"cui-proxy/internal/config"
)

// starterFiles are the files `init` writes, in this order. hosts.txt is the
// user's dictionary of sensitive names, so every file is created 0600 in a
// 0700 directory.
var starterFiles = []string{"config.yaml", "hosts.txt"}

// runInit writes a starter config and dictionary so a new install is usable
// in one step. It never overwrites an existing file unless -force is given,
// and never writes through a symlink.
func runInit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	dir := fs.String("dir", "", "directory to write into (default $XDG_CONFIG_HOME/cui-proxy, else ~/.config/cui-proxy)")
	force := fs.Bool("force", false, "overwrite existing files")
	profile := fs.String("profile", config.ProfileSanitize, "policy profile to write: sanitize (mask and restore) or strict (any detection blocks the request)")
	if code, ok := parseFlags(fs, args, stderr, nil); !ok {
		return code
	}
	if *profile != config.ProfileSanitize && *profile != config.ProfileStrict {
		return usageError(stderr, fmt.Sprintf("init: -profile %q must be sanitize or strict", *profile))
	}

	target := *dir
	if target == "" {
		d, err := defaultConfigDir()
		if err != nil {
			return fail(stderr, err)
		}
		target = d
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return fail(stderr, err)
	}

	// Check every target before writing anything, so a refusal leaves the
	// directory untouched. Lstat: a dangling symlink still counts as existing.
	if !*force {
		for _, name := range starterFiles {
			p := filepath.Join(abs, name)
			if _, err := os.Lstat(p); err == nil {
				return fail(stderr, fmt.Errorf("init: %s already exists; use -force to overwrite", p))
			} else if !errors.Is(err, os.ErrNotExist) {
				return fail(stderr, fmt.Errorf("init: %w", err))
			}
		}
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return fail(stderr, fmt.Errorf("init: %w", err))
	}
	for _, name := range starterFiles {
		data, err := starterContent(name, *profile)
		if err != nil {
			return fail(stderr, fmt.Errorf("init: embedded %s: %w", name, err))
		}
		if err := writeStarter(filepath.Join(abs, name), data, *force); err != nil {
			return fail(stderr, fmt.Errorf("init: %w", err))
		}
	}

	cfgPath := filepath.Join(abs, "config.yaml")
	if _, _, err := loadRules(cfgPath); err != nil {
		return fail(stderr, fmt.Errorf("init: the written config does not validate: %w", err))
	}
	fmt.Fprintf(stdout, nextSteps, cfgPath, filepath.Join(abs, "hosts.txt"), cfgPath, cfgPath)
	if *profile == config.ProfileStrict {
		fmt.Fprintf(stdout, "\nPolicy profile: strict. Any detection blocks the whole request before it is forwarded.\nReview it with: cui-proxy policy check -config %s\n", cfgPath)
	} else {
		fmt.Fprintf(stdout, "\nPolicy profile: sanitize (detections are masked and restored, not refused).\nFor a policy that forbids sending restricted data at all: cui-proxy policy init -profile strict\n")
	}
	return exitOK
}

// starterContent returns the embedded starter file, with the policy profile
// set in config.yaml. The example states the default profile explicitly so it
// can be changed with one visible line.
func starterContent(name, profile string) ([]byte, error) {
	data, err := examples.FS.ReadFile(name)
	if err != nil || name != "config.yaml" || profile == config.ProfileSanitize {
		return data, err
	}
	const marker = "\n  profile: sanitize\n"
	if strings.Count(string(data), marker) != 1 {
		return nil, errors.New("the starter config has no single policy.profile line to change")
	}
	return []byte(strings.Replace(string(data), marker, "\n  profile: "+profile+"\n", 1)), nil
}

// writeStarter creates path with mode 0600. Without force it refuses to touch
// an existing file (O_EXCL). With force it writes a temporary file in the same
// directory and renames it over path, which replaces a symlink itself rather
// than writing through it.
func writeStarter(path string, data []byte, force bool) error {
	if !force {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		if _, err := f.Write(data); err != nil {
			f.Close()
			return err
		}
		return f.Close()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".init-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) // no-op after a successful rename
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// defaultConfigDir is $XDG_CONFIG_HOME/cui-proxy, else ~/.config/cui-proxy
// (also on macOS, so the docs have one path).
func defaultConfigDir() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "cui-proxy"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("init: cannot find a home directory (use -dir): %w", err)
	}
	return filepath.Join(home, ".config", "cui-proxy"), nil
}

// nextSteps is printed after a successful init. Verbs: config path, hosts
// path, config path (check-config), config path (serve).
const nextSteps = `Wrote a starter config:
  %s
  %s   (replace the placeholder with your own sensitive host names)

Next:
  1. Edit hosts.txt.
  2. cui-proxy check-config -config %s
  3. cui-proxy serve -config %s
  4. Point Codex at the proxy: add this to ~/.codex/config.toml

       model_provider = "cui-proxy"

       [model_providers.cui-proxy]
       name = "cui-proxy"
       base_url = "http://127.0.0.1:8787/v1"
       wire_api = "responses"
       requires_openai_auth = true

Dashboard: http://127.0.0.1:8788/

Read the known limitations first (README, docs/limitations.md): this tool reduces risk, it is
not a compliance control, and GOVSEC / ISSM approval is still required.
`
