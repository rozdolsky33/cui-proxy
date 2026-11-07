package main

import (
	"bytes"
	_ "embed"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// menubarSource is the Swift menu-bar app. It is compiled on the user's own
// machine by `menubar install`, so there is no prebuilt, unsigned download for
// Gatekeeper to object to.
//
//go:embed menubar/CuiProxyMenu.swift
var menubarSource []byte

const (
	menubarBundleID = "io.github.rozdolsky33.cui-proxy-menu"
	menubarAppName  = "CuiProxy.app"
	menubarExeName  = "CuiProxyMenu"
)

const menubarUsage = `usage: cui-proxy menubar <install|uninstall|status> [flags]

  install [-config PATH] [-binary PATH] [-dir DIR] [-no-launch]
        compile and install the menu-bar app into ~/Applications (or DIR) and
        start it. The app runs the proxy in the background.
  uninstall [-dir DIR]   quit and remove the app (config, dictionary and logs stay)
  status [-dir DIR]      show whether it is installed and what it sees
`

// runMenubar installs, removes or inspects the macOS menu-bar app.
func runMenubar(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return usageError(stderr, "menubar needs a subcommand\n"+menubarUsage)
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "install", "uninstall", "status":
	default:
		return usageError(stderr, fmt.Sprintf("menubar: unknown subcommand %q\n%s", sub, menubarUsage))
	}

	fs := flag.NewFlagSet("menubar "+sub, flag.ContinueOnError)
	dir := fs.String("dir", "", "directory to install the app into (default ~/Applications)")
	var cfgFlag, binFlag *string
	var noLaunch *bool
	if sub == "install" {
		cfgFlag = fs.String("config", "", "config the app should run (default ~/.config/cui-proxy/config.yaml)")
		binFlag = fs.String("binary", "", "cui-proxy binary the app should run (default: the one on PATH)")
		noLaunch = fs.Bool("no-launch", false, "install but do not start the app")
	}
	if code, ok := parseFlags(fs, rest, stderr, nil); !ok {
		return code
	}
	if runtime.GOOS != "darwin" {
		return fail(stderr, errors.New("menubar: the menu-bar app is only available on macOS"))
	}

	appsDir, err := menubarAppsDir(*dir)
	if err != nil {
		return fail(stderr, err)
	}
	switch sub {
	case "install":
		return menubarInstall(appsDir, *cfgFlag, *binFlag, !*noLaunch, stdout, stderr)
	case "uninstall":
		return menubarUninstall(appsDir, stdout, stderr)
	default:
		return menubarStatus(appsDir, stdout, stderr)
	}
}

func menubarAppsDir(dir string) (string, error) {
	if dir != "" {
		return filepath.Abs(dir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("menubar: cannot find a home directory (use -dir): %w", err)
	}
	return filepath.Join(home, "Applications"), nil
}

// menubarInfoPlist is the app's Info.plist. binary and config are hints the
// app falls back on when its own search finds nothing; both are XML-escaped.
func menubarInfoPlist(binary, config, ver string) []byte {
	esc := func(s string) string {
		var b bytes.Buffer
		_ = xml.EscapeText(&b, []byte(s))
		return b.String()
	}
	short := strings.TrimPrefix(ver, "v")
	if short == "" || short == "dev" {
		short = "0.0.0"
	}
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleIdentifier</key><string>` + menubarBundleID + `</string>
  <key>CFBundleName</key><string>CuiProxy</string>
  <key>CFBundleDisplayName</key><string>cui-proxy</string>
  <key>CFBundleExecutable</key><string>` + menubarExeName + `</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>` + esc(short) + `</string>
  <key>CFBundleVersion</key><string>` + esc(short) + `</string>
  <key>LSMinimumSystemVersion</key><string>12.0</string>
  <key>LSUIElement</key><true/>
  <key>NSHighResolutionCapable</key><true/>
  <key>CUIProxyBinary</key><string>` + esc(binary) + `</string>
  <key>CUIProxyConfig</key><string>` + esc(config) + `</string>
</dict>
</plist>
`)
}

// runQuiet runs a command and returns its combined output, trimmed.
func runQuiet(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func menubarInstall(appsDir, cfgFlag, binFlag string, launch bool, stdout, stderr io.Writer) int {
	swiftc, err := exec.LookPath("swiftc")
	if err != nil {
		return fail(stderr, errors.New("menubar: swiftc not found; install Apple's Command Line Tools with: xcode-select --install"))
	}

	binary := binFlag
	if binary == "" {
		if p, err := exec.LookPath("cui-proxy"); err == nil {
			binary = p
		} else if p, err := os.Executable(); err == nil {
			binary = p
		}
	}
	if binary == "" {
		return fail(stderr, errors.New("menubar: cannot find the cui-proxy binary (use -binary)"))
	}
	if abs, err := filepath.Abs(binary); err == nil {
		binary = abs
	}
	config := cfgFlag
	if config == "" {
		d, err := defaultConfigDir()
		if err != nil {
			return fail(stderr, err)
		}
		config = filepath.Join(d, "config.yaml")
	}
	if abs, err := filepath.Abs(config); err == nil {
		config = abs
	}

	if err := os.MkdirAll(appsDir, 0o755); err != nil {
		return fail(stderr, fmt.Errorf("menubar: %w", err))
	}
	// Build next to the destination so the final rename stays on one volume.
	work, err := os.MkdirTemp(appsDir, ".cuiproxy-build-")
	if err != nil {
		return fail(stderr, fmt.Errorf("menubar: %w", err))
	}
	defer os.RemoveAll(work)

	app := filepath.Join(work, menubarAppName)
	macos := filepath.Join(app, "Contents", "MacOS")
	if err := os.MkdirAll(macos, 0o755); err != nil {
		return fail(stderr, fmt.Errorf("menubar: %w", err))
	}
	src := filepath.Join(work, "CuiProxyMenu.swift")
	if err := os.WriteFile(src, menubarSource, 0o600); err != nil {
		return fail(stderr, fmt.Errorf("menubar: %w", err))
	}
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), menubarInfoPlist(binary, config, version), 0o644); err != nil {
		return fail(stderr, fmt.Errorf("menubar: %w", err))
	}

	fmt.Fprintln(stdout, "Compiling the menu-bar app (this takes a few seconds)...")
	exe := filepath.Join(macos, menubarExeName)
	if out, err := runQuiet(swiftc, "-O", "-swift-version", "5", "-o", exe, src); err != nil {
		return fail(stderr, fmt.Errorf("menubar: swiftc failed: %w\n%s", err, out))
	}
	// An ad-hoc signature (no developer account needed) binds the Info.plist.
	if out, err := runQuiet("codesign", "--force", "--sign", "-", "--identifier", menubarBundleID, app); err != nil {
		fmt.Fprintf(stderr, "cui-proxy: warning: codesign failed (%v); continuing with the compiler's own signature\n%s\n", err, out)
	}

	dest := filepath.Join(appsDir, menubarAppName)
	if err := stopMenubarApp(dest); err != nil {
		return fail(stderr, err)
	}
	if err := os.RemoveAll(dest); err != nil {
		return fail(stderr, fmt.Errorf("menubar: %w", err))
	}
	if err := os.Rename(app, dest); err != nil {
		return fail(stderr, fmt.Errorf("menubar: %w", err))
	}
	fmt.Fprintf(stdout, "Installed: %s\n", dest)

	if launch {
		if out, err := runQuiet("/usr/bin/open", dest); err != nil {
			return fail(stderr, fmt.Errorf("menubar: could not start the app: %w\n%s", err, out))
		}
		fmt.Fprintln(stdout, "Started. Look for the shield icon in your menu bar.")
	}
	fmt.Fprint(stdout, `
Click the icon for: Open Dashboard, Copy Codex command, Start/Stop, Reload rules,
Open Config Folder, Open Log, and Start at Login.
`)
	if _, err := os.Stat(config); err != nil {
		fmt.Fprintf(stdout, "\nNo config at %s yet: click the icon and choose \"Create starter config...\",\nor run: cui-proxy init\n", config)
	}
	fmt.Fprintln(stdout, "\nTo remove it: cui-proxy menubar uninstall")
	return exitOK
}

// appPIDs returns the PIDs of running copies of the installed app.
func appPIDs(app string) []int {
	// pgrep -f takes a regular expression: match the app's path literally.
	pattern := regexp.QuoteMeta(filepath.Join(app, "Contents", "MacOS", menubarExeName))
	out, err := exec.Command("pgrep", "-f", "--", pattern).Output()
	if err != nil {
		return nil
	}
	var pids []int
	for _, f := range strings.Fields(string(out)) {
		if n, err := strconv.Atoi(f); err == nil && n != os.Getpid() {
			pids = append(pids, n)
		}
	}
	return pids
}

// stopMenubarApp asks a running copy to quit with SIGTERM (the app stops the
// proxy it started, saves the vault, then exits) and waits for it.
func stopMenubarApp(app string) error {
	pids := appPIDs(app)
	for _, p := range pids {
		_ = syscall.Kill(p, syscall.SIGTERM)
	}
	deadline := time.Now().Add(20 * time.Second)
	for len(appPIDs(app)) > 0 {
		if time.Now().After(deadline) {
			return errors.New("menubar: the running menu-bar app did not quit; quit it from its menu and retry")
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil
}

func menubarUninstall(appsDir string, stdout, stderr io.Writer) int {
	dest := filepath.Join(appsDir, menubarAppName)
	if err := stopMenubarApp(dest); err != nil {
		return fail(stderr, err)
	}
	if home, err := os.UserHomeDir(); err == nil {
		plist := filepath.Join(home, "Library", "LaunchAgents", menubarBundleID+".plist")
		if _, err := os.Stat(plist); err == nil {
			// Best effort: drop the login item (and unload it if loaded).
			_, _ = runQuiet("launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid())+"/"+menubarBundleID)
			if err := os.Remove(plist); err != nil {
				return fail(stderr, fmt.Errorf("menubar: %w", err))
			}
			fmt.Fprintln(stdout, "Removed the Start at Login item.")
		}
	}
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		fmt.Fprintln(stdout, "Menu-bar app not installed.")
		return exitOK
	}
	if err := os.RemoveAll(dest); err != nil {
		return fail(stderr, fmt.Errorf("menubar: %w", err))
	}
	fmt.Fprintf(stdout, "Removed %s\nYour config, dictionary and ~/Library/Logs/cui-proxy are untouched.\n", dest)
	return exitOK
}

func menubarStatus(appsDir string, stdout, stderr io.Writer) int {
	dest := filepath.Join(appsDir, menubarAppName)
	exe := filepath.Join(dest, "Contents", "MacOS", menubarExeName)
	if _, err := os.Stat(exe); err != nil {
		fmt.Fprintln(stdout, "Menu-bar app: not installed (install with: cui-proxy menubar install)")
		return exitOK
	}
	fmt.Fprintf(stdout, "Menu-bar app: installed at %s\n", dest)
	if len(appPIDs(dest)) > 0 {
		fmt.Fprintln(stdout, "Menu-bar app: running")
	} else {
		fmt.Fprintf(stdout, "Menu-bar app: not running (start it with: open %q)\n", dest)
	}
	if out, _ := exec.Command(exe, "--check").CombinedOutput(); len(out) > 0 {
		fmt.Fprintf(stdout, "\nWhat the app sees:\n%s", out)
	}
	return exitOK
}
