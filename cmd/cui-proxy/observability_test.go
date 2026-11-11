package main

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"cui-proxy/internal/observe"
)

func TestCorrelationFollowsSuccessfulConfigReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	write := func(enabled string) {
		t.Helper()
		data := "upstreams:\n  codex: http://127.0.0.1:1\naudit:\n  enabled: false\nobservability:\n  correlate: " + enabled + "\n"
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("true")
	app, err := NewApp(AppOptions{ConfigPath: path, Logf: func(string, ...any) {}})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	header := make(http.Header)
	header.Set("session_id", "observation-session")
	token := app.metrics.Correlate(header)
	if token.Identity.Session == "" {
		t.Fatal("startup did not enable correlation")
	}
	app.metrics.ObserveCorrelated(token, observe.Snapshot{})
	if len(app.metrics.Snapshot().RecentRequests) != 1 {
		t.Fatal("enabled request missing")
	}
	write("invalid-boolean")
	if app.Reload() == nil {
		t.Fatal("invalid reload accepted")
	}
	if app.metrics.Correlate(header).Identity.Session == "" {
		t.Fatal("invalid reload changed correlation")
	}
	write("false")
	if err := app.Reload(); err != nil {
		t.Fatal(err)
	}
	if app.metrics.Correlate(header).Identity.Session != "" || len(app.metrics.Snapshot().RecentRequests) != 0 {
		t.Fatal("disable did not clear correlation")
	}
	write("true")
	if err := app.Reload(); err != nil {
		t.Fatal(err)
	}
	app.metrics.ObserveCorrelated(token, observe.Snapshot{})
	if len(app.metrics.Snapshot().RecentRequests) != 0 {
		t.Fatal("old in-flight token repopulated history")
	}
	app.metrics.ObserveCorrelated(app.metrics.Correlate(header), observe.Snapshot{})
	if len(app.metrics.Snapshot().RecentRequests) != 1 {
		t.Fatal("reenabled correlation did not record new request")
	}
}
