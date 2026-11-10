package proxy

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cui-proxy/internal/config"
	"cui-proxy/internal/detect"
	"cui-proxy/internal/transform"
	"cui-proxy/internal/vault"
)

func TestProxyToolImageSanitizationAndRefusal(t *testing.T) {
	for _, tc := range []struct {
		name, mode, profile, onError, script string
		status                               int
	}{
		{"extract", "extract_text", "sanitize", "block", "printf hiddenlabel", 200},
		{"strict", "extract_text", "strict", "block", "printf hiddenlabel", 422},
		{"disabled", "block", "sanitize", "block", "printf hiddenlabel", 422},
		{"failed", "extract_text", "sanitize", "block", "exit 1", 422},
		{"empty_passthrough", "extract_text", "sanitize", "passthrough", "exit 0", 422},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "tesseract"), []byte("#!/bin/sh\n"+tc.script+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir)
			var b bytes.Buffer
			if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
				t.Fatal(err)
			}
			body := fmt.Sprintf(`{"input":[{"type":"custom_tool_call_output","call_id":"call-test","output":[{"type":"input_image","image_url":"data:image/png;base64,%s"}]}]}`, base64.StdEncoding.EncodeToString(b.Bytes()))
			forwarded := make(chan []byte, 1)
			var h *harness
			h = newHarness(t, func(w http.ResponseWriter, r *http.Request) {
				var root struct {
					Input []struct{ Output []struct{ Text string } }
				}
				// The harness has already captured and consumed the upstream body.
				data := h.upstreamGot().Body
				forwarded <- data
				if json.Unmarshal(data, &root) != nil || len(root.Input) != 1 || len(root.Input[0].Output) != 1 {
					t.Error("invalid forwarded tool output")
					w.WriteHeader(500)
					return
				}
				sseHeaders(w)
				writeEvent(w, "response.output_text.delta", textDelta(root.Input[0].Output[0].Text))
				writeEvent(w, "response.completed", `{"type":"response.completed"}`)
			})
			defer h.close()
			ds, err := detect.BuildForProfile([]config.Rule{{Name: "label", Type: "regex", Pattern: "hiddenlabel", Synth: "secret"}}, "", tc.profile)
			if err != nil {
				t.Fatal(err)
			}
			v := vault.New(bytes.Repeat([]byte{8}, 32))
			h.cur.Store(&set{v: v, s: transform.NewSanitizer(ds, v, nil), r: transform.NewRestorer(v)})
			h.cfg.Policy.Attachments = tc.mode
			h.cfg.Policy.OnError = tc.onError
			h.cfg.Policy.Profile = tc.profile
			res, response := h.post(body)
			if res.StatusCode != tc.status {
				t.Fatalf("unexpected status: %d", res.StatusCode)
			}
			if tc.status == 200 {
				got := <-forwarded
				if bytes.Contains(got, []byte("hiddenlabel")) || bytes.Contains(got, []byte("image_url")) {
					t.Fatal("upstream received original image or OCR value")
				}
				if !bytes.Contains(response, []byte("hiddenlabel")) {
					t.Fatal("stream did not restore OCR value")
				}
			} else {
				h.noUpstreamCall()
			}
			a := h.nextAudit()
			metadata, _ := json.Marshal(a)
			if bytes.Contains(metadata, []byte("hiddenlabel")) || strings.Contains(h.logs.String(), "hiddenlabel") || strings.Contains(h.logs.String(), "data:image") {
				t.Fatal("attachment content leaked to metadata")
			}
			if tc.status == 200 && a.Rules["label"] != 1 {
				t.Fatal("OCR replacement missing from audit counts")
			}
		})
	}
}
