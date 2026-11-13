// Package recovery supplies closed, value-free refusal guidance.
package recovery

import (
	"cui-proxy/internal/attachment"
	"strings"
	"sync"
	"time"
)

type Guidance struct {
	Code        string `json:"code"`
	Category    string `json:"category"`
	Retry       string `json:"retry"`
	Remedy      string `json:"remedy"`
	Occurrences uint32 `json:"category_occurrences,omitempty"`
}

var catalogue = map[string]Guidance{
	"policy":                  {Category: "policy", Retry: "after_change", Remedy: "Remove the blocked content from the request and retained history. If the client resends old history, start a fresh session with only permitted content. Keep blocking rules enabled."},
	"header":                  {Category: "policy", Retry: "after_change", Remedy: "Correct or remove rejected request metadata. Use the request ID with cui-proxy explain; keep detection enabled."},
	"client":                  {Category: "representation", Retry: "after_change", Remedy: "Use a native client with a loopback base URL."},
	"route":                   {Category: "representation", Retry: "after_change", Remedy: "Use a supported Responses API route. Disable unsupported remote operations in the client or use a compatible client workflow."},
	"compressed":              {Category: "representation", Retry: "after_change", Remedy: "Send an uncompressed JSON request."},
	"bad_request":             {Category: "representation", Retry: "after_change", Remedy: "Correct the request framing and JSON shape before resending."},
	"body_too_large":          {Category: "budget", Retry: "after_change", Remedy: "Reduce the request and retained history to fit the configured body limit."},
	"sanitize_bad_body":       {Category: "representation", Retry: "after_change", Remedy: "Use supported JSON content and attachment positions. Replace unsupported content with text; remove it from retained history before retrying."},
	"sanitize_depth":          {Category: "budget", Retry: "after_change", Remedy: "Reduce nested JSON depth before retrying."},
	"sanitize_pool_exhausted": {Category: "budget", Retry: "after_change", Remedy: "Reduce distinct sensitive values or use a supported synthetic class with sufficient capacity. Do not discard mappings needed by active sessions."},
	"sanitize_pseudonym":      {Category: "representation", Retry: "after_change", Remedy: "Check the reported rule and synthetic class with synthetic test input; correct the rule configuration without disabling protection."},
	"sanitize_ambiguous":      {Category: "representation", Retry: "after_change", Remedy: "Remove ambiguous content from the request and retained history. Preserve mappings for active sessions."},
	"unrestorable":            {Category: "representation", Retry: "after_change", Remedy: "Use the request ID to diagnose the rule and content representation; correct the input before retrying."},
	"sanitize":                {Category: "internal", Retry: "after_repair", Remedy: "Use cui-proxy explain with the request ID and inspect safe diagnostic codes before retrying."},
	"audit":                   {Category: "dependency", Retry: "after_repair", Remedy: "Repair decision-log storage or permissions, then run cui-proxy audit verify. Keep audit blocking enabled."},
	"upstream":                {Category: "transient", Retry: "backoff", Remedy: "Check backend availability and retry with bounded backoff. Delivery may be uncertain; check the client task state first."},
	"upstream_redirect":       {Category: "representation", Retry: "after_change", Remedy: "Configure the final upstream API endpoint; do not follow redirects with original content."},
	"stream_rewrite":          {Category: "representation", Retry: "after_change", Remedy: "Use a supported uncompressed SSE response. Discard incomplete output and inspect the request ID before retrying."},
	"internal":                {Category: "internal", Retry: "after_repair", Remedy: "Inspect service health and safe diagnostic logs using the request ID before retrying."},
}

func init() {
	for _, kind := range []string{"bad_body", "depth", "pool_exhausted", "pseudonym", "ambiguous"} {
		guidance := catalogue["sanitize_"+kind]
		guidance.Remedy = "Correct the request metadata. " + guidance.Remedy
		catalogue["header_"+kind] = guidance
	}
}

func Lookup(reason, kind, detail string) Guidance {
	if attachment.KnownDetail(detail) {
		g := Guidance{Code: detail, Category: "representation", Retry: "after_change", Remedy: attachment.Explain(detail)}
		switch {
		case strings.HasSuffix(detail, "_missing_tool"):
			g.Category, g.Retry = "dependency", "after_repair"
		case strings.HasSuffix(detail, "_limit"):
			g.Category = "budget"
		case strings.HasSuffix(detail, "_busy"):
			g.Category, g.Retry = "overload", "backoff"
		case strings.HasSuffix(detail, "_timeout"):
			g.Category = "budget"
		case strings.HasSuffix(detail, "_failed"):
			g.Category, g.Retry = "dependency", "after_repair"
		}
		if g.Retry == "after_change" {
			g.Remedy += " Remove rejected content from retained history before retrying; if necessary, start a fresh session with permitted content."
		}
		return g
	}
	code := reason
	if reason == "sanitize" || reason == "header" {
		if _, ok := catalogue[reason+"_"+kind]; ok {
			code = reason + "_" + kind
		}
		if kind == "rule_match" {
			code = "policy"
		}
		if kind == "unrestorable" {
			code = "unrestorable"
		}
	}
	g, ok := catalogue[code]
	if !ok {
		code = "internal"
		g = catalogue[code]
	}
	g.Code = code
	return g
}

type occurrence struct {
	start time.Time
	count uint32
}

// Tracker stores only a bounded set of closed category codes. Counts are global
// to one handler, not proof of identical requests or a particular client's retry.
type Tracker struct {
	mu      sync.Mutex
	entries map[string]occurrence
}

func (t *Tracker) Record(code string, now time.Time) uint32 {
	g, known := catalogue[code]
	if attachment.KnownDetail(code) {
		g = Lookup("sanitize", "bad_body", code)
		known = true
	}
	if !known || g.Retry == "backoff" {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.entries == nil {
		t.entries = make(map[string]occurrence)
	}
	for key, e := range t.entries {
		if now.Sub(e.start) >= time.Minute || now.Before(e.start) {
			delete(t.entries, key)
		}
	}
	e, ok := t.entries[code]
	if !ok {
		if len(t.entries) >= 128 {
			return 0
		}
		e.start = now
	}
	if e.count < 1000000 {
		e.count++
	}
	t.entries[code] = e
	return e.count
}
