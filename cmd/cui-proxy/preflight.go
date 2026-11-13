package main

import (
	"context"
	"cui-proxy/internal/config"
	"cui-proxy/internal/preflight"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

var errPreflight = errors.New("preflight: running service unavailable or incompatible; start the service and check its dashboard address")

func fetchPreflight(ctx context.Context, base string) (preflight.Report, error) {
	var report preflight.Report
	u, err := url.Parse(base)
	if err != nil {
		return report, errPreflight
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback())) {
		return report, errPreflight
	}
	u.Path = "/api/preflight"
	u.RawPath = ""
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return report, errPreflight
	}
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return report, errPreflight
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return report, errPreflight
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, (64<<10)+1))
	if err != nil || len(raw) > 64<<10 {
		return report, errPreflight
	}
	if json.Unmarshal(raw, &report) != nil || !report.Valid() {
		return preflight.Report{}, errPreflight
	}
	return report, nil
}

func runPreflight(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("preflight", flag.ContinueOnError)
	path := fs.String("config", "", "configuration identifying the running dashboard")
	if code, ok := parseFlags(fs, args, stderr, path); !ok {
		return code
	}
	cfg, err := config.Load(*path)
	if err != nil {
		return fail(stderr, errors.New("preflight: configuration unavailable"))
	}
	base, err := browserURL(cfg.UI.Listen)
	if err != nil {
		return fail(stderr, errPreflight)
	}
	report, err := fetchPreflight(context.Background(), base)
	if err != nil {
		return fail(stderr, err)
	}
	if err := json.NewEncoder(stdout).Encode(report); err != nil {
		return fail(stderr, errors.New("preflight: output failed"))
	}
	if !report.Ready {
		fmt.Fprintln(stderr, "preflight: service is not ready; follow the reported dependency remedies and check again")
		return exitFailure
	}
	return exitOK
}
