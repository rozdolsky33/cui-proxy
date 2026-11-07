package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMenubarUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"menubar"},
		{"menubar", "bogus"},
		{"menubar", "install", "extra"},
		{"menubar", "status", "extra"},
		{"menubar", "install", "-nope"},
	} {
		if code, _, errOut := runCLI("", args...); code != 2 || errOut == "" {
			t.Errorf("%q: exit %d, stderr %q; want 2 with a message", args, code, errOut)
		}
	}
}

func TestMenubarInfoPlist(t *testing.T) {
	got := string(menubarInfoPlist("/opt/homebrew/bin/cui-proxy", "/Users/a&b/<x>/config.yaml", "v1.2.3"))
	for _, want := range []string{
		"<key>CFBundleIdentifier</key><string>io.github.rozdolsky33.cui-proxy-menu</string>",
		"<key>CFBundleExecutable</key><string>CuiProxyMenu</string>",
		"<key>LSUIElement</key><true/>",
		"<key>CFBundleShortVersionString</key><string>1.2.3</string>",
		"<key>CUIProxyBinary</key><string>/opt/homebrew/bin/cui-proxy</string>",
		// XML metacharacters in paths must be escaped, not break the plist.
		"<key>CUIProxyConfig</key><string>/Users/a&amp;b/&lt;x&gt;/config.yaml</string>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Info.plist missing %q:\n%s", want, got)
		}
	}
}

func TestMenubarNotOnOtherPlatforms(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("macOS")
	}
	code, _, errOut := runCLI("", "menubar", "install")
	if code != 1 || !strings.Contains(errOut, "macOS") {
		t.Errorf("code %d, stderr %q; want 1 mentioning macOS", code, errOut)
	}
}

// needMac skips unless this is macOS with a Swift compiler.
func needMac(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only")
	}
	if _, err := exec.LookPath("swiftc"); err != nil {
		t.Skip("swiftc not installed")
	}
}

func TestMenubarInstallStatusUninstall(t *testing.T) {
	needMac(t)
	apps := t.TempDir()
	cfg := filepath.Join(t.TempDir(), "config.yaml")
	app := filepath.Join(apps, "CuiProxy.app")

	install := func() (int, string, string) {
		return runCLI("", "menubar", "install", "-dir", apps, "-binary", "/bin/echo", "-config", cfg, "-no-launch")
	}
	code, out, errOut := install()
	if code != 0 {
		t.Fatalf("install: code %d, stdout %q, stderr %q", code, out, errOut)
	}
	exe := filepath.Join(app, "Contents", "MacOS", "CuiProxyMenu")
	for _, p := range []string{exe, filepath.Join(app, "Contents", "Info.plist")} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}
	if o, err := exec.Command("codesign", "--verify", "--strict", app).CombinedOutput(); err != nil {
		t.Errorf("codesign --verify: %v\n%s", err, o)
	}
	if o, err := exec.Command("plutil", "-lint", filepath.Join(app, "Contents", "Info.plist")).CombinedOutput(); err != nil {
		t.Errorf("plutil -lint: %v\n%s", err, o)
	}

	// The binary and config hints reach the app (headless --check).
	chk := exec.Command(exe, "--check")
	chk.Env = append(os.Environ(), "CUI_PROXY_BIN=", "CUI_PROXY_CONFIG=")
	o, _ := chk.CombinedOutput()
	if !strings.Contains(string(o), "binary=/bin/echo\n") || !strings.Contains(string(o), "config="+cfg+"\n") {
		t.Errorf("--check did not see the installed hints:\n%s", o)
	}

	// status reports it.
	code, out, _ = runCLI("", "menubar", "status", "-dir", apps)
	if code != 0 || !strings.Contains(out, "installed") {
		t.Errorf("status: code %d, out %q", code, out)
	}

	// Installing again replaces it cleanly.
	if code, _, errOut := install(); code != 0 {
		t.Fatalf("second install: code %d, stderr %q", code, errOut)
	}
	if entries, _ := os.ReadDir(apps); len(entries) != 1 {
		t.Errorf("leftover build directories in %s: %v", apps, entries)
	}

	// Uninstall removes it; doing it twice is fine.
	for i := 0; i < 2; i++ {
		if code, _, errOut := runCLI("", "menubar", "uninstall", "-dir", apps); code != 0 {
			t.Fatalf("uninstall #%d: code %d, stderr %q", i+1, code, errOut)
		}
	}
	if _, err := os.Stat(app); !os.IsNotExist(err) {
		t.Errorf("app still present after uninstall (err=%v)", err)
	}
}

func TestMenubarInstallWithoutSwiftc(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only")
	}
	t.Setenv("PATH", t.TempDir()) // nothing on PATH
	code, _, errOut := runCLI("", "menubar", "install", "-dir", t.TempDir(), "-binary", "/bin/echo", "-no-launch")
	if code != 1 || !strings.Contains(errOut, "xcode-select --install") {
		t.Errorf("code %d, stderr %q; want 1 with the xcode-select hint", code, errOut)
	}
}

func TestMenubarStatusNotInstalled(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only")
	}
	code, out, _ := runCLI("", "menubar", "status", "-dir", t.TempDir())
	if code != 0 || !strings.Contains(out, "not installed") {
		t.Errorf("code %d, out %q", code, out)
	}
}
