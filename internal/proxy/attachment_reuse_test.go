package proxy

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
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
)

func TestCachedImageReappliesReloadedPolicy(t *testing.T) {
	dir := t.TempDir()
	counter := filepath.Join(dir, "calls")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	script := "#!/bin/sh\nprintf 'x\n' >> " + quote(counter) + "\nprintf 'reuse_marker\n'\n"
	if err := os.WriteFile(filepath.Join(dir, "tesseract"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	var pngBytes bytes.Buffer
	if err := png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	data := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes.Bytes())
	request, err := json.Marshal(map[string]any{"input": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_image", "image_url": data}}}}})
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) { defaultStream(w) })
	defer h.close()
	h.cfg.Policy.Attachments = "extract_text"
	first, _ := h.post(string(request))
	if first.StatusCode != http.StatusOK {
		t.Fatal("initial extraction refused")
	}
	h.upstreamGot()
	ds, err := detect.Build([]config.Rule{{Name: "cache-block", Type: "regex", Pattern: "reuse_marker", Action: "block"}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	next := newSet(t)
	next.s = transform.NewSanitizer(ds, next.v, nil)
	h.cur.Store(next)
	second, _ := h.post(string(request))
	if second.StatusCode != http.StatusUnprocessableEntity {
		t.Fatal("cached text bypassed current blocking policy")
	}
	h.noUpstreamCall()
	calls, err := os.ReadFile(counter)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(calls), "\n") != 1 {
		t.Fatal("reload test did not exercise cached extraction")
	}
}
