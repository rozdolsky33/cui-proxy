package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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
	t.Setenv("HOME", t.TempDir()) // uninstall looks at ~/Library/LaunchAgents: never the real one
	apps := t.TempDir()
	cfg := filepath.Join(t.TempDir(), "config.yaml")
	app := filepath.Join(apps, "CuiProxy.app")

	install := func() (int, string, string) {
		return runCLI("", "menubar", "install", "-dir", apps, "-binary", "/bin/echo", "-config", cfg, "-no-launch")
	}
	// A build directory left by an interrupted install is cleaned up.
	if err := os.MkdirAll(filepath.Join(apps, ".cuiproxy-build-stale", "CuiProxy.app"), 0o755); err != nil {
		t.Fatal(err)
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
	chk.Env = append(os.Environ(), "CUI_PROXY_BIN=", "CUI_PROXY_CONFIG=", "XDG_CONFIG_HOME=")
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

// fakeLaunchAgent writes ~/Library/LaunchAgents/<bundle id>.plist under home,
// launching exe.
func fakeLaunchAgent(t *testing.T, home, exe string) string {
	t.Helper()
	dir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, menubarBundleID+".plist")
	writeFile(t, p, `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>`+menubarBundleID+`</string>
<key>ProgramArguments</key><array><string>`+exe+`</string></array>
<key>RunAtLoad</key><true/>
</dict></plist>
`)
	return p
}

// fakeApp makes <apps>/CuiProxy.app with the given bundle id (no executable).
func fakeApp(t *testing.T, apps, bundleID string) string {
	t.Helper()
	app := filepath.Join(apps, menubarAppName)
	if err := os.MkdirAll(filepath.Join(app, "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	plist := strings.Replace(string(menubarInfoPlist("/x", "/y", "v1.0.0")), menubarBundleID, bundleID, 1)
	writeFile(t, filepath.Join(app, "Contents", "Info.plist"), plist)
	return app
}

func TestMenubarUninstallOnlyRemovesItsOwnLoginItem(t *testing.T) {
	needMac(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	apps := t.TempDir()
	app := fakeApp(t, apps, menubarBundleID)
	exe := filepath.Join(app, "Contents", "MacOS", menubarExeName)

	// A login item that launches some OTHER copy stays.
	plist := fakeLaunchAgent(t, home, "/Applications/CuiProxy.app/Contents/MacOS/"+menubarExeName)
	if code, _, errOut := runCLI("", "menubar", "uninstall", "-dir", apps); code != 0 {
		t.Fatalf("uninstall: code %d, stderr %q", code, errOut)
	}
	if _, err := os.Stat(plist); err != nil {
		t.Errorf("a login item for a different copy was removed: %v", err)
	}
	if _, err := os.Stat(app); !os.IsNotExist(err) {
		t.Errorf("the app was not removed (err=%v)", err)
	}

	// A login item that launches THIS copy goes with it.
	_ = fakeApp(t, apps, menubarBundleID)
	_ = fakeLaunchAgent(t, home, exe)
	if code, out, errOut := runCLI("", "menubar", "uninstall", "-dir", apps); code != 0 || !strings.Contains(out, "Start at Login") {
		t.Fatalf("uninstall: code %d, out %q, stderr %q", code, out, errOut)
	}
	if _, err := os.Stat(plist); !os.IsNotExist(err) {
		t.Errorf("the login item for this copy was not removed (err=%v)", err)
	}
}

func TestMenubarUninstallRefusesForeignApp(t *testing.T) {
	needMac(t)
	t.Setenv("HOME", t.TempDir())
	apps := t.TempDir()
	app := fakeApp(t, apps, "com.example.someone-elses-app")
	code, _, errOut := runCLI("", "menubar", "uninstall", "-dir", apps)
	if code != 1 || !strings.Contains(errOut, "not the cui-proxy menu-bar app") {
		t.Errorf("code %d, stderr %q; want 1 refusing to remove it", code, errOut)
	}
	if _, err := os.Stat(app); err != nil {
		t.Errorf("a foreign app with our name was removed: %v", err)
	}
}

// startFake runs /bin/sleep with argv[0] set to path, like a process whose
// command line is that path.
func startFake(t *testing.T, path string) int {
	t.Helper()
	cmd := exec.Command("/bin/sleep", "60")
	cmd.Args[0] = path
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	return cmd.Process.Pid
}

func TestMenubarAppPIDsMatchesOnlyThatApp(t *testing.T) {
	needMac(t)
	root := t.TempDir()
	app := filepath.Join(root, "Users", "me", "Applications", menubarAppName)
	pid := startFake(t, filepath.Join(app, "Contents", "MacOS", menubarExeName))
	waitFor := func(app string, want bool) bool {
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			found := false
			for _, p := range appPIDs(app) {
				if p == pid {
					found = true
				}
			}
			if found == want {
				return true
			}
			time.Sleep(50 * time.Millisecond)
		}
		return false
	}
	if !waitFor(app, true) {
		t.Fatalf("appPIDs(%s) did not find pid %d", app, pid)
	}
	// The same path as a suffix of another directory must not match: asking
	// about /Applications must not find ~/Applications.
	suffix := filepath.Join(string(filepath.Separator)+"Users", "me", "Applications", menubarAppName)[len("/Users/me"):]
	if !waitFor(suffix, false) {
		t.Errorf("appPIDs(%s) matched a copy in a different directory", suffix)
	}
	if !waitFor(app+".bak", false) {
		t.Errorf("appPIDs matched a neighbouring directory name")
	}
}
