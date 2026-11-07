package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// Fix round 1: upstream redirects, request header/query scanning, Expect,
// audit Method, partial final SSE event.

func TestUpstreamRedirectBlocked(t *testing.T) {
	for _, status := range []int{300, 301, 302, 303, 307, 308} {
		for _, route := range []struct{ method, path, body string }{
			{"POST", "/v1/responses", userBody("hi")},
			{"GET", "/v1/models", ""},
		} {
			t.Run(fmt.Sprint(status, route.path), func(t *testing.T) {
				h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Location", "https://elsewhere.example/steal")
					w.Header().Set("Content-Type", "text/html")
					w.WriteHeader(status)
					io.WriteString(w, `<a href="https://elsewhere.example/steal">moved</a>`)
				})
				res, body := h.do(h.req(route.method, route.path, route.body))
				if res.StatusCode != http.StatusBadGateway {
					t.Errorf("status = %d, want 502", res.StatusCode)
				}
				if loc := res.Header.Values("Location"); len(loc) != 0 || bytes.Contains(body, []byte("elsewhere")) {
					t.Errorf("redirect relayed: Location %q body %q", loc, body)
				}
				var e apiError
				if err := json.Unmarshal(body, &e); err != nil || e.Error.Code != "upstream_redirect" {
					t.Errorf("body = %q", body)
				}
				a := h.nextAudit()
				if a.Status != http.StatusBadGateway || a.Blocked != "upstream_redirect" {
					t.Errorf("audit = %+v", a)
				}
				if s := h.reg.Snapshot(); s.Errors != 1 || s.Blocked != 0 {
					t.Errorf("errors = %d, blocked = %d", s.Errors, s.Blocked)
				}
			})
		}
	}
}

func TestUpstream304PassedThrough(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Etag", `"v1"`)
		w.WriteHeader(http.StatusNotModified)
	})
	req := h.req("GET", "/v1/models", "")
	req.Header.Set("If-None-Match", `"v1"`)
	res, body := h.do(req)
	if res.StatusCode != http.StatusNotModified || len(body) != 0 || res.Header.Get("Etag") != `"v1"` {
		t.Errorf("got %d %q %v", res.StatusCode, body, res.Header)
	}
	if a := h.nextAudit(); a.Status != 304 || a.Blocked != "" {
		t.Errorf("audit = %+v", a)
	}
}

// The header shapes real Codex 0.166.0 sends (synthetic ids) must pass.
func TestRealCodexHeadersNotBlocked(t *testing.T) {
	h := newHarness(t, nil)
	const (
		u1 = "0f8e3c2a-6b1d-4c7e-9a52-3d4b5c6e7f80"
		u2 = "1a2b3c4d-5e6f-4a1b-8c2d-3e4f5a6b7c8d"
		u3 = "9d8c7b6a-5f4e-4d3c-b2a1-0f9e8d7c6b5a"
		u4 = "4e5f6a7b-8c9d-4e0f-a1b2-c3d4e5f6a7b8"
		u5 = "7c6b5a49-3827-4165-b0f9-e8d7c6b5a493"
	)
	meta := `{"installation_id":"` + u1 + `","session_id":"` + u2 + `","thread_id":"` + u3 +
		`","turn_id":"` + u4 + `","window_id":"` + u5 + `:0","model":"gpt-5.5","reasoning_effort":"high",` +
		`"sandbox":"seatbelt","turn_started_at_unix_ms":1794014035169}`
	codexHeaders := map[string]string{
		"Accept":                "text/event-stream",
		"Content-Type":          "application/json",
		"Originator":            "codex_exec",
		"Session-Id":            u2,
		"Thread-Id":             u3,
		"X-Client-Request-Id":   u4,
		"User-Agent":            "codex_exec/0.166.0 (Mac OS 26.5.2; arm64) ghostty/1.4.0 (codex_exec; 0.166.0)",
		"X-Codex-Beta-Features": "remote_compaction_v2",
		"X-Codex-Window-Id":     u5 + ":0",
		"X-Codex-Turn-Metadata": meta,
		"Authorization":         "Bearer eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiIxMjM0In0.c2lnbmF0dXJl",
		"Chatgpt-Account-Id":    u1,
	}
	for name, value := range codexHeaders {
		if name == "Authorization" || name == "Chatgpt-Account-Id" {
			continue // credential headers are not scanned
		}
		if sp := h.set().s.Detect(value); len(sp) != 0 {
			t.Errorf("header %s trips detector %s (%s)", name, sp[0].Rule, sp[0].Class)
		}
	}
	for _, path := range []string{"/v1/responses", "/v1/models?client_version=0.166.0"} {
		method, body := "POST", userBody("hi")
		if strings.HasPrefix(path, "/v1/models") {
			method, body = "GET", ""
		}
		req := h.req(method, path, body)
		for k, v := range codexHeaders {
			req.Header.Set(k, v)
		}
		if res, b := h.do(req); res.StatusCode != 200 {
			t.Errorf("%s: status %d %s", path, res.StatusCode, b)
		}
		h.upstreamGot()
		h.nextAudit()
	}
	if s := h.reg.Snapshot(); s.Blocked != 0 {
		t.Errorf("blocked = %v", s.BlockedByReason)
	}
}

func TestSensitiveHeaderOrQueryBlocked(t *testing.T) {
	for _, tc := range []struct {
		name, path, header, value string
	}{
		{"ip in turn metadata", "/v1/responses", "X-Codex-Turn-Metadata", `{"turn_id":"t1","cwd_host":"` + realIP + `"}`},
		{"ocid in custom header", "/v1/responses", "X-Custom", realOCID},
		{"email in header", "/v1/responses", "X-Codex-User", realEmail},
		{"ip in query", "/v1/responses?host=" + realIP, "", ""},
		{"encoded ip in query", "/v1/models?h=10%2E20%2E30%2E15", "", ""},
		{"email in query (passthrough route)", "/v1/models?u=alice.smith%40corp-internal.com", "", ""},
		{"ip in header name", "/v1/responses", "X-Host-" + realIP, "1"},
		// A malformed escape elsewhere in the query must not stop the scan.
		{"ip after bad escape", "/v1/responses?a=%zz&b=10%2E20%2E30%2E15", "", ""},
		{"ocid after bad escape", "/v1/models?a=%zz&b=ocid1.instance.oc1..aaaa", "", ""},
		{"ip in encoded key", "/v1/responses?k%3D10%2E20%2E30%2E15=v", "", ""},
		{"ip after semicolon", "/v1/models?a=%zz;b=10.20.30.15", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, nil)
			h.cfg.Policy.OnError = "passthrough" // never relaxes a header block
			method, body := "POST", userBody("hi")
			if strings.HasPrefix(tc.path, "/v1/models") {
				method, body = "GET", ""
			}
			req := h.req(method, tc.path, body)
			if tc.header != "" {
				req.Header.Set(tc.header, tc.value)
			}
			res, b := h.do(req)
			assertBlocked(t, h, res, b, http.StatusUnprocessableEntity, "header")
			h.noUpstreamCall() // explicit: nothing reached the upstream
			for _, real := range []string{realIP, realOCID, realEmail, "corp-internal", "10%2E20"} {
				if strings.Contains(string(b), real) || strings.Contains(h.logs.String(), real) {
					t.Errorf("response or log echoes %q", real)
				}
			}
			if h.reg.Snapshot().Passthrough != 0 {
				t.Error("header block forwarded under passthrough policy")
			}
		})
	}
}

// Credential headers are not scanned (a bearer token is a "secret").
func TestCredentialHeadersNotScanned(t *testing.T) {
	h := newHarness(t, nil)
	req := h.req("POST", "/v1/responses", userBody("hi"))
	req.Header.Set("Authorization", "Bearer abcdefgh12345678")
	req.Header.Set("Cookie", "s=eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiIxMjM0In0.c2lnbmF0dXJl")
	req.Header.Set("Chatgpt-Account-Id", realEmail)
	if res, _ := h.do(req); res.StatusCode != 200 {
		t.Errorf("status %d", res.StatusCode)
	}
}

func TestExpectNotForwarded(t *testing.T) {
	h := newHarness(t, nil)
	req := h.req("POST", "/v1/responses", userBody("hi"))
	req.Header.Set("Expect", "100-continue")
	if res, _ := h.do(req); res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	if v := h.upstreamGot().Header.Values("Expect"); len(v) != 0 {
		t.Errorf("Expect forwarded: %q", v)
	}
}

func TestAuditMethodClosedSet(t *testing.T) {
	h := newHarness(t, nil)
	for _, tc := range []struct{ method, want string }{
		{realIP, "(other)"},
		{"PROPFIND", "(other)"},
		{"DELETE", "DELETE"},
		{"POST", "POST"},
	} {
		h.do(h.req(tc.method, "/v1/nothing", ""))
		if a := h.nextAudit(); a.Method != tc.want {
			t.Errorf("method %q audited as %q, want %q", tc.method, a.Method, tc.want)
		}
	}
}

// A close-delimited upstream that dies mid-event must not look like a clean
// end: the partial event is dropped and the client connection aborted.
func TestPartialFinalEventAborts(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		fmt.Fprint(buf, "HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nConnection: close\r\n\r\n")
		fmt.Fprintf(buf, "event: response.created\ndata: %s\n\n", evCreated)
		fmt.Fprintf(buf, "event: response.output_text.delta\ndata: %s\n\n", textDelta("hello "))
		fmt.Fprint(buf, `event: response.output_text.delta`+"\n"+`data: {"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"wor`)
		buf.Flush()
	})
	res, err := h.client.Do(h.req("POST", "/v1/responses", userBody("hi")))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err == nil {
		t.Errorf("clean end of stream after a partial event: %q", body)
	}
	if bytes.Contains(body, []byte("wor")) {
		t.Errorf("partial event forwarded: %q", body)
	}
	if !bytes.Contains(body, []byte("response.created")) {
		t.Errorf("complete events lost: %q", body)
	}
	a := h.nextAudit()
	if a.Status != 200 {
		t.Errorf("audit = %+v", a)
	}
	if s := h.reg.Snapshot(); s.Errors != 1 {
		t.Errorf("errors = %d", s.Errors)
	}
}

// Only a detector hit blocks: an undecodable but clean query passes.
func TestCleanQueryNotBlocked(t *testing.T) {
	for _, q := range []string{
		"client_version=0.166.0",
		"client_version=0.166.0&x=%zz",
		// Double encoding: components are decoded once only (double decoding is
		// not attempted), so "10%2E20%2E30%2E15" is what gets scanned and no
		// detector fires on it. This test documents that behavior, nothing more.
		"h=10%252E20%252E30%252E15",
	} {
		t.Run(q, func(t *testing.T) {
			h := newHarness(t, nil)
			if res, b := h.do(h.req("GET", "/v1/models?"+q, "")); res.StatusCode != 200 {
				t.Errorf("status %d %s", res.StatusCode, b)
			}
			if got := h.upstreamGot(); got.URI != "/backend-api/codex/models?"+q {
				t.Errorf("upstream URI = %q", got.URI)
			}
		})
	}
}

func TestTolerantUnescape(t *testing.T) {
	for in, want := range map[string]string{
		"a%2Eb":   "a.b",
		"a+b":     "a b",
		"%zz%2E":  "%zz.",
		"%":       "%",
		"x%4":     "x%4",
		"%252E":   "%2E",
		"plain":   "plain",
		"%41%42c": "ABc",
	} {
		if got := tolerantUnescape(in); got != want {
			t.Errorf("tolerantUnescape(%q) = %q, want %q", in, got, want)
		}
	}
}
