package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestSuccessfulUnrestorableResponsesBlocked(t *testing.T) {
	for _, tc := range []struct{ name, contentType, encoding, body string }{
		{"json", "application/json", "", `{"output":"synthetic reply"}`},
		{"plain", "text/plain", "", "synthetic reply"},
		{"missing type", "", "", "synthetic reply"},
		{"misleading MIME", "text/event-streaming", "", "data: {}\n\n"},
		{"encoded missing type", "", "gzip", "synthetic reply"},
		{"probe exhausted", "", "", strings.Repeat("\n", 40) + "data: {}\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header()["Content-Type"] = nil
				if tc.contentType != "" {
					w.Header().Set("Content-Type", tc.contentType)
				}
				if tc.encoding != "" {
					w.Header().Set("Content-Encoding", tc.encoding)
				}
				w.WriteHeader(http.StatusOK)
				io.WriteString(w, tc.body)
			})
			res, body := h.post(userBody(realIP))
			var e apiError
			if err := json.Unmarshal(body, &e); err != nil || res.StatusCode != http.StatusBadGateway || e.Error.Code != reasonStreamRewrite {
				t.Errorf("unrestored success relayed: status=%d body=%q", res.StatusCode, body)
			}
			if a := h.nextAudit(); a.Blocked != reasonStreamRewrite {
				t.Errorf("audit reason=%q", a.Blocked)
			}
		})
	}
}
