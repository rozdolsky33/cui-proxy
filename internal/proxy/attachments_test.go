package proxy

import (
	"encoding/base64"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"

	"cui-proxy/internal/adapter/codex"
	"cui-proxy/internal/transform"
)

func TestAttachmentDiagnosticsPersistAndStayValueFree(t *testing.T) {
	for _, mode := range []string{"block", "extract_text"} {
		t.Run(mode, func(t *testing.T) {
			h := newHarness(t, nil)
			h.cfg.Policy.Attachments = mode
			h.cfg.Policy.OnError = "passthrough"
			res, rb := h.post(`{"input":[{"role":"user","content":[{"type":"input_file","file_id":"private-secret-file"}]}]}`)
			if res.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("status %d", res.StatusCode)
			}
			h.noUpstreamCall()
			detail := "attachment_file_disabled"
			if mode == "extract_text" {
				detail = "attachment_file_reference"
			}
			log, err := os.ReadFile(h.decPath)
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range []string{string(rb), string(log), h.logs.String()} {
				if !strings.Contains(s, detail) || strings.Contains(s, "private-secret-file") {
					t.Fatalf("missing safe detail: %s", s)
				}
			}
			if !strings.Contains(string(rb), "inline") {
				t.Fatalf("no actionable explanation: %s", rb)
			}
			if !strings.Contains(h.logs.String(), "inline") {
				t.Fatalf("no remedy in log: %s", h.logs.String())
			}
			h.records() // verifies the persisted chain
		})
	}
}

func TestAttachmentModeNeverFallsBackToOriginal(t *testing.T) {
	h := newHarness(t, nil, withAdapter(&fakeAdapter{Adapter: codex.New(), sanitize: func([]byte, *transform.Sanitizer) ([]byte, error) { return nil, errors.New("failure") }}))
	h.cfg.Policy.OnError = "passthrough"
	h.cfg.Policy.Attachments = "extract_text"
	res, _ := h.post(userBody(realIP))
	if res.StatusCode != 422 {
		t.Fatalf("status %d; original must not pass on conversion mode failure", res.StatusCode)
	}
	h.noUpstreamCall()
}

func TestProxyPDFSendsOnlySanitizedText(t *testing.T) {
	for _, tool := range []string{"pdfinfo", "pdftotext"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip("requires Poppler")
		}
	}
	b, err := os.ReadFile("../attachment/testdata/text.pdf")
	if err != nil {
		t.Fatal(err)
	}
	data := "data:application/pdf;base64," + base64.StdEncoding.EncodeToString(b)
	h := newHarness(t, nil)
	h.cfg.Policy.Attachments = "extract_text"
	res, rb := h.post(`{"input":[{"role":"user","content":[{"type":"input_file","filename":"private.pdf","file_data":"` + data + `"}]}]}`)
	if res.StatusCode != 200 {
		t.Fatalf("status %d: %s", res.StatusCode, rb)
	}
	select {
	case req := <-h.reqs:
		s := string(req.Body)
		for _, bad := range []string{"private.pdf", "file_data", data, "10.20.30.15", "alice@example.test"} {
			if strings.Contains(s, bad) {
				t.Fatalf("original data reached upstream: %s", s)
			}
		}
		if !strings.Contains(s, `"type":"input_text"`) {
			t.Fatalf("not text: %s", s)
		}
	default:
		t.Fatal("no upstream request")
	}
	h.records()
}
