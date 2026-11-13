package proxy

import (
	"cui-proxy/internal/recovery"
	"encoding/json"
	"net/http"
	"testing"
)

func TestRepeatedImageRecoveryDoesNotForward(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) { t.Error("upstream called") })
	defer h.close()
	for i := uint32(1); i <= 3; i++ {
		res, body := h.post(`{"input":[{"role":"user","content":[{"type":"input_image","image_url":"https://example.invalid/image"}]}]}`)
		var doc struct {
			Error struct {
				Code, Type, Message, RequestID string
				Recovery                       recovery.Guidance
			}
		}
		if json.Unmarshal(body, &doc) != nil {
			t.Fatal("invalid response")
		}
		if res.StatusCode != 422 || doc.Error.Code != "sanitize" || doc.Error.Type != "cui_proxy_blocked" {
			t.Fatal("compatibility changed")
		}
		g := doc.Error.Recovery
		if g.Code != "attachment_image_disabled" || g.Retry != "after_change" || g.Occurrences != i || g.Remedy == "" {
			t.Fatal("missing actionable recurrence")
		}
		if res.Header.Get("X-Cui-Proxy-Retry") != g.Retry || res.Header.Get("X-Cui-Proxy-Recovery-Code") != g.Code {
			t.Fatal("guidance headers missing")
		}
	}
	h.noUpstreamCall()
}
