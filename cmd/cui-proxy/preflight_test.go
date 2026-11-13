package main

import (
	"context"
	"cui-proxy/internal/config"
	"cui-proxy/internal/preflight"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPreflightQueriesServiceAndRefusesRedirect(t *testing.T) {
	cfg := &config.Config{}
	report := preflight.New("service-build").Check(context.Background(), cfg)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/preflight" {
			t.Error("wrong service path")
		}
		_ = json.NewEncoder(w).Encode(report)
	}))
	defer server.Close()
	got, err := fetchPreflight(context.Background(), server.URL)
	if err != nil || got.Version != "service-build" || !got.Ready {
		t.Fatal("service report not used")
	}
	redirects := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, server.URL, http.StatusFound) }))
	defer redirects.Close()
	if _, err := fetchPreflight(context.Background(), redirects.URL); err == nil {
		t.Fatal("redirect followed")
	}
	if _, err := fetchPreflight(context.Background(), "https://example.invalid"); err == nil {
		t.Fatal("non-local target accepted")
	}
}
func TestPreflightRejectsInvalidServiceResponse(t *testing.T) {
	for _, body := range []string{`{}`, `{"schema_version":99}`, `null`, "not-json"} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
		_, err := fetchPreflight(context.Background(), server.URL)
		server.Close()
		if err == nil {
			t.Fatal("unrecognized service response accepted")
		}
	}
}

func TestPreflightRejectsUntrustedMetadata(t *testing.T) {
	report := preflight.New("service-build").Check(context.Background(), &config.Config{})
	report.Version = "untrusted\nmetadata"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(report) }))
	defer server.Close()
	if _, err := fetchPreflight(context.Background(), server.URL); err == nil {
		t.Fatal("untrusted metadata accepted")
	}
}
