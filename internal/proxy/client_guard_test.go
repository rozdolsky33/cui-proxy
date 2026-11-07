package proxy

import (
	"net/http"
	"strings"
	"testing"
)

func TestProxyRejectsBrowserAndReboundHosts(t *testing.T) {
	for _, tc := range []struct{ name, host, header, value string }{
		{"rebound host", "attacker.example:8787", "", ""},
		{"browser origin", "", "Origin", "https://attacker.example"},
		{"same origin browser", "", "Sec-Fetch-Site", "same-origin"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, nil)
			req := h.req("POST", "/v1/responses", userBody("hello"))
			if tc.host != "" {
				req.Host = tc.host
			}
			if tc.header != "" {
				req.Header.Set(tc.header, tc.value)
			}
			res, body := h.do(req)
			if res.StatusCode != http.StatusForbidden {
				t.Errorf("untrusted client accepted: %d %s", res.StatusCode, body)
			}
			h.noUpstreamCall()
			if r := h.recordFor(res); r.Decision != "blocked" || r.Reason != "client" {
				t.Errorf("decision=%+v", r)
			}
			if strings.Contains(string(body), "attacker.example") || strings.Contains(h.logs.String(), "attacker.example") {
				t.Error("client metadata echoed")
			}
		})
	}
}

func TestRestoredResponsesAreNotCacheable(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		defaultStream(w)
	})
	res, _ := h.post(userBody("hello"))
	if got := res.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("cache policy=%q", got)
	}
}
