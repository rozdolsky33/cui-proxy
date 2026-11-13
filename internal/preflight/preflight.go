// Package preflight reports only safe metadata about the running service.
package preflight

import (
	"context"
	"cui-proxy/internal/attachment"
	"cui-proxy/internal/config"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"time"
)

type Policy struct {
	Profile     string `json:"profile"`
	Attachments string `json:"attachments"`
	OnError     string `json:"on_error"`
	Unsupported string `json:"unsupported"`
}
type Report struct {
	SchemaVersion int                    `json:"schema_version"`
	Ready         bool                   `json:"ready"`
	Policy        Policy                 `json:"policy"`
	Version       string                 `json:"version"`
	GoVersion     string                 `json:"go_version"`
	Revision      string                 `json:"revision"`
	PID           int                    `json:"pid"`
	Tools         []attachment.ToolCheck `json:"tools"`
	ProbeCode     string                 `json:"probe_code"`
}
type Checker struct {
	version, revision string
	probe             func(context.Context) []attachment.ToolCheck
	gate              chan struct{}
	cached            []attachment.ToolCheck
	checked           time.Time
}

func safeBuild(s string) string {
	if s == "" || len(s) > 128 {
		return "unknown"
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._+-", r)) {
			return "unknown"
		}
	}
	return s
}
func New(version string) *Checker {
	revision := "unknown"
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				revision = safeBuild(setting.Value)
			}
		}
	}
	return &Checker{version: safeBuild(version), revision: revision, probe: attachment.CheckTools, gate: make(chan struct{}, 1)}
}
func enum(value, fallback string, allowed ...string) string {
	if value == "" {
		return fallback
	}
	for _, v := range allowed {
		if value == v {
			return v
		}
	}
	return "unknown"
}

// Check caches dependency metadata for ten seconds; policy is read fresh on
// every call. At most one bounded probe runs at a time per service checker.
func (c *Checker) Check(ctx context.Context, cfg *config.Config) Report {
	r := Report{SchemaVersion: 1, Version: c.version, Revision: c.revision, GoVersion: safeBuild(runtime.Version()), PID: os.Getpid(), Tools: []attachment.ToolCheck{}, ProbeCode: "policy_unavailable"}
	if cfg == nil {
		return r
	}
	r.Policy = Policy{Profile: enum(cfg.Policy.Profile, "sanitize", "sanitize", "strict"), Attachments: enum(cfg.Policy.Attachments, "block", "block", "extract_text"), OnError: enum(cfg.Policy.OnError, "block", "block", "passthrough"), Unsupported: enum(cfg.Policy.Unsupported, "block", "block")}
	if r.Policy.Profile == "unknown" || r.Policy.Attachments == "unknown" || r.Policy.OnError == "unknown" || r.Policy.Unsupported == "unknown" {
		return r
	}
	if r.Policy.Attachments == "block" {
		r.Ready = true
		r.ProbeCode = "ready"
		return r
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	select {
	case c.gate <- struct{}{}:
		defer func() { <-c.gate }()
	case <-ctx.Done():
		r.ProbeCode = "probe_timeout"
		return r
	}
	if ctx.Err() != nil {
		r.ProbeCode = "probe_timeout"
		return r
	}
	if c.cached == nil || time.Since(c.checked) >= 10*time.Second {
		c.cached = c.probe(ctx)
		c.checked = time.Now()
	}
	r.Tools = append(r.Tools, c.cached...)
	r.Ready = len(r.Tools) == 5
	for _, tool := range r.Tools {
		r.Ready = r.Ready && tool.Ready
	}
	r.ProbeCode = "dependencies_unavailable"
	if r.Ready {
		r.ProbeCode = "ready"
	}
	return r
}

// Valid checks the versioned value-free shape before a CLI prints a report
// obtained from a configured local endpoint.
func (r Report) Valid() bool {
	if r.SchemaVersion != 1 || r.PID <= 0 || safeBuild(r.Version) != r.Version || safeBuild(r.GoVersion) != r.GoVersion || safeBuild(r.Revision) != r.Revision {
		return false
	}
	if r.ProbeCode != "ready" && r.ProbeCode != "policy_unavailable" && r.ProbeCode != "dependencies_unavailable" && r.ProbeCode != "probe_timeout" {
		return false
	}
	if r.Ready != (r.ProbeCode == "ready") {
		return false
	}
	for _, field := range []struct {
		value   string
		allowed []string
	}{
		{r.Policy.Profile, []string{"", "unknown", "sanitize", "strict"}},
		{r.Policy.Attachments, []string{"", "unknown", "block", "extract_text"}},
		{r.Policy.OnError, []string{"", "unknown", "block", "passthrough"}},
		{r.Policy.Unsupported, []string{"", "unknown", "block"}},
	} {
		found := false
		for _, v := range field.allowed {
			if v == field.value {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	if r.ProbeCode == "policy_unavailable" {
		return !r.Ready && len(r.Tools) == 0
	}
	if enum(r.Policy.Profile, "unknown", "sanitize", "strict") == "unknown" || enum(r.Policy.Attachments, "unknown", "block", "extract_text") == "unknown" || enum(r.Policy.OnError, "unknown", "block", "passthrough") == "unknown" || r.Policy.Unsupported != "block" {
		return false
	}
	if len(r.Tools) > 5 {
		return false
	}
	seen := map[string]bool{}
	for _, t := range r.Tools {
		switch t.Name {
		case "pdfinfo", "pdftotext", "pdftoppm", "tesseract", "tesseract_eng":
		default:
			return false
		}
		if seen[t.Name] {
			return false
		}
		seen[t.Name] = true
		switch t.Code {
		case "ready", "missing_tool", "not_checked", "missing_english", "probe_failed", "probe_timeout", "probe_busy", "probe_limit":
		default:
			return false
		}
		if t.Remedy != attachment.ToolRemedy(t.Code) {
			return false
		}
		if t.Ready != (t.Code == "ready") {
			return false
		}
	}
	if r.Policy.Attachments == "block" {
		return r.Ready && len(r.Tools) == 0
	}
	if r.Ready {
		if len(r.Tools) != 5 {
			return false
		}
		for _, t := range r.Tools {
			if !t.Ready {
				return false
			}
		}
	}
	return true
}
