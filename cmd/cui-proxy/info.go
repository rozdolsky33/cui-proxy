package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"path/filepath"

	"cui-proxy/internal/config"
)

// runInfo prints where a config's proxy and dashboard listen, as key=value
// lines, without starting anything. The macOS menu-bar app uses it to find
// the dashboard URL; it is also handy in scripts.
func runInfo(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("info", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "path to the YAML config")
	if code, ok := parseFlags(fs, args, stderr, cfgPath); !ok {
		return code
	}
	abs, err := filepath.Abs(*cfgPath)
	if err != nil {
		return fail(stderr, err)
	}
	cfg, err := config.Load(abs)
	if err != nil {
		return fail(stderr, err)
	}
	proxyURL, err := browserURL(cfg.Listen)
	if err != nil {
		return fail(stderr, fmt.Errorf("listen: %w", err))
	}
	uiURL, err := browserURL(cfg.UI.Listen)
	if err != nil {
		return fail(stderr, fmt.Errorf("ui.listen: %w", err))
	}
	fmt.Fprintf(stdout, "config=%s\nproxy_url=%s\ndashboard_url=%s\nversion=%s\n", abs, proxyURL, uiURL, version)
	return exitOK
}

// browserURL turns a listen address into an http URL a client on this machine
// can open. A wildcard or empty host (0.0.0.0, ::, "") is reached via loopback.
func browserURL(listen string) (string, error) {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "", fmt.Errorf("invalid address %q", listen)
	}
	if port == "" {
		return "", fmt.Errorf("invalid address %q: no port", listen)
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/", nil
}
