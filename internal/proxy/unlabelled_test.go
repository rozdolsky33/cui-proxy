package proxy

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"testing"
)

// The live ChatGPT Responses endpoint can omit Content-Type on its SSE stream.
// Header-only classification must not bypass restoration in that case.
func TestUnlabelledResponseRestoresSSE(t *testing.T) {
	for _, prefix := range []string{"", ": keepalive\n\n", "\xef\xbb\xbf", "\n", "\r\n\r\n", "\xef\xbb\xbf\r\n"} {
		t.Run(fmt.Sprintf("prefix_%x", prefix), func(t *testing.T) {
			var h *harness
			h = newHarness(t, func(w http.ResponseWriter, r *http.Request) {
				syn := h.set().pseudo(t, "ipv4", realIP)
				w.Header()["Content-Type"] = nil // suppress net/http's automatic sniffing
				w.WriteHeader(http.StatusOK)
				io.WriteString(w, prefix)
				writeEvent(w, "response.output_text.delta", textDelta("host="+syn+" done"))
				writeEvent(w, "response.completed", evCompleted)
			})
			res, body := h.post(userBody(realIP))
			if res.Header.Get("Content-Type") != "text/event-stream" {
				t.Errorf("Content-Type = %q", res.Header.Get("Content-Type"))
			}
			if got := deltas(t, readEvents(t, body), "response.output_text.delta"); got != "host=10.20.30.15 done" {
				t.Errorf("restored text = %q", got)
			}
			if bytes.Contains(body, []byte(h.set().pseudo(t, "ipv4", realIP))) {
				t.Error("synthetic reached client")
			}
		})
	}
}

func TestUnlabelledResponsePreservesJSON(t *testing.T) {
	const want = `{"error":{"message":"upstream unavailable"}}`
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header()["Content-Type"] = nil
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, want)
	})
	res, body := h.post(userBody("hello"))
	if res.StatusCode != http.StatusBadRequest || string(body) != want {
		t.Errorf("response = %d %q", res.StatusCode, body)
	}
}
