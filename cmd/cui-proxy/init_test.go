package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cui-proxy/examples"
)

func embedded(t *testing.T, name string) string {
	t.Helper()
	b, err := examples.FS.ReadFile(name)
	if err != nil {
		t.Fatalf("embedded %s: %v", name, err)
	}
	return string(b)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInitWritesStarterFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cfg")
	code, out, errOut := runCLI("", "init", "-dir", dir)
	if code != 0 {
		t.Fatalf("init: code %d, stderr %q", code, errOut)
	}
	for _, name := range []string{"config.yaml", "hosts.txt"} {
		p := filepath.Join(dir, name)
		if got := readFile(t, p); got != embedded(t, name) {
			t.Errorf("%s differs from the embedded example", name)
		}
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Errorf("%s mode %v, want 0600", name, st.Mode().Perm())
		}
		if !strings.Contains(out, p) {
			t.Errorf("stdout does not name %s: %q", p, out)
		}
	}
	st, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o700 {
		t.Errorf("dir mode %v, want 0700", st.Mode().Perm())
	}

	// The written config must be valid as it stands.
	code, out, errOut = runCLI("", "check-config", "-config", filepath.Join(dir, "config.yaml"))
	if code != 0 || out != "ok: 3 rules\n" {
		t.Fatalf("check-config on init output: code %d, out %q, err %q", code, out, errOut)
	}
}

func TestInitPrintsNextSteps(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cfg")
	_, out, _ := runCLI("", "init", "-dir", dir)
	for _, want := range []string{
		"check-config -config " + filepath.Join(dir, "config.yaml"),
		"serve -config " + filepath.Join(dir, "config.yaml"),
		`model_provider = "cui-proxy"`,
		`base_url = "http://127.0.0.1:8787/v1"`,
		"requires_openai_auth = true",
		"http://127.0.0.1:8788/",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("init output missing %q:\n%s", want, out)
		}
	}
}

func TestInitRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	const sentinel = "SENTINEL-DICTIONARY-ENTRY\n"
	hosts := filepath.Join(dir, "hosts.txt")
	if err := os.WriteFile(hosts, []byte(sentinel), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := runCLI("", "init", "-dir", dir)
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(errOut, "-force") {
		t.Errorf("error does not mention -force: %q", errOut)
	}
	if strings.Contains(errOut, "SENTINEL") {
		t.Errorf("error leaks file content: %q", errOut)
	}
	if got := readFile(t, hosts); got != sentinel {
		t.Errorf("existing file was modified: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.yaml")); !os.IsNotExist(err) {
		t.Errorf("config.yaml was written although hosts.txt exists (err=%v)", err)
	}
}

func TestInitForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"config.yaml", "hosts.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("old\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	code, _, errOut := runCLI("", "init", "-dir", dir, "-force")
	if code != 0 {
		t.Fatalf("init -force: code %d, stderr %q", code, errOut)
	}
	for _, name := range []string{"config.yaml", "hosts.txt"} {
		p := filepath.Join(dir, name)
		if readFile(t, p) != embedded(t, name) {
			t.Errorf("%s not replaced", name)
		}
		st, _ := os.Stat(p)
		if st.Mode().Perm() != 0o600 {
			t.Errorf("%s mode %v after -force, want 0600", name, st.Mode().Perm())
		}
	}
}

func TestInitForceDoesNotFollowSymlinks(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("keep me\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "hosts.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if code, _, errOut := runCLI("", "init", "-dir", dir, "-force"); code != 0 {
		t.Fatalf("init -force: code %d, stderr %q", code, errOut)
	}
	if got := readFile(t, outside); got != "keep me\n" {
		t.Errorf("the symlink target was overwritten: %q", got)
	}
	st, err := os.Lstat(filepath.Join(dir, "hosts.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode()&os.ModeSymlink != 0 {
		t.Error("hosts.txt is still a symlink")
	}
}

func TestInitRefusesDanglingSymlinkWithoutForce(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(t.TempDir(), "nowhere"), filepath.Join(dir, "config.yaml")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if code, _, _ := runCLI("", "init", "-dir", dir); code != 1 {
		t.Fatalf("exit %d, want 1 (a dangling symlink still counts as existing)", code)
	}
}

func TestInitDefaultDir(t *testing.T) {
	t.Run("XDG_CONFIG_HOME", func(t *testing.T) {
		xdg := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", xdg)
		t.Setenv("HOME", t.TempDir())
		if code, _, errOut := runCLI("", "init"); code != 0 {
			t.Fatalf("code %d, stderr %q", code, errOut)
		}
		if _, err := os.Stat(filepath.Join(xdg, "cui-proxy", "config.yaml")); err != nil {
			t.Error(err)
		}
	})
	t.Run("HOME/.config", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("HOME", home)
		if code, _, errOut := runCLI("", "init"); code != 0 {
			t.Fatalf("code %d, stderr %q", code, errOut)
		}
		if _, err := os.Stat(filepath.Join(home, ".config", "cui-proxy", "hosts.txt")); err != nil {
			t.Error(err)
		}
	})
}

func TestInitUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"init", "extra"},
		{"init", "-nope"},
	} {
		if code, _, errOut := runCLI("", args...); code != 2 || errOut == "" {
			t.Errorf("%q: exit %d, stderr %q; want 2 with a message", args, code, errOut)
		}
	}
}

func TestUsageListsInit(t *testing.T) {
	_, out, _ := runCLI("", "help")
	if !strings.Contains(out, "init") {
		t.Errorf("usage does not list init:\n%s", out)
	}
}
