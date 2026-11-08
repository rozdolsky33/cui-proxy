package codex

import (
	"bytes"
	"context"
	"cui-proxy/internal/adapter"
	"cui-proxy/internal/transform"
	"encoding/base64"
	"errors"
	"image"
	"image/png"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestDefaultAttachmentDiagnostic(t *testing.T) {
	for _, typ := range []string{"input_file", "input_image", "input_audio"} {
		out, err := New().SanitizeRequest([]byte(`{"input":[{"role":"user","content":[{"type":"`+typ+`"}]}]}`), newEnv(t).s)
		if out != nil || err == nil || !strings.Contains(err.Error(), "attachment_") {
			t.Fatalf("want attachment diagnostic: %s, %v", out, err)
		}
	}
}

func inlinePDF(t *testing.T) string {
	t.Helper()
	for _, tool := range []string{"pdfinfo", "pdftotext"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip("requires Poppler")
		}
	}
	b, err := os.ReadFile("../../attachment/testdata/text.pdf")
	if err != nil {
		t.Fatal(err)
	}
	return "data:application/pdf;base64," + base64.StdEncoding.EncodeToString(b)
}
func attachmentBody(part string) []byte {
	return []byte(`{"input":[{"role":"user","content":[` + part + `]}]}`)
}
func TestAttachmentPDFReplacementAndStrict(t *testing.T) {
	data := inlinePDF(t)
	body := attachmentBody(`{"type":"input_file","filename":"private-secret.pdf","file_data":"` + data + `"}`)
	out, err := New().SanitizeRequestWithPolicy(context.Background(), body, newEnv(t).s, "extract_text")
	if err != nil {
		t.Fatal(err)
	}
	assertAbsent(t, out, "10.20.30.15", "alice@example.test", "private-secret.pdf", "file_data", data)
	if !strings.Contains(string(out), `"type":"input_text"`) {
		t.Fatalf("not converted: %s", out)
	}
	out, err = New().SanitizeRequestWithPolicy(context.Background(), body, strictSanitizer(t), "extract_text")
	if out != nil || !errors.Is(err, transform.ErrBlocked) {
		t.Fatalf("strict: %s, %v", out, err)
	}
}
func TestAttachmentAmbiguitiesBlock(t *testing.T) {
	for _, part := range []string{
		`{"type":"input_file","file_id":"file-secret"}`,
		`{"type":"input_file","file_url":"https://example.invalid/private"}`,
		`{"type":"input_image","image_url":"https://example.invalid/private"}`,
		`{"type":"input_image","image_url":"data:image/png;base64,aGk=","image_url":"other"}`,
		`{"type":"input_file","type":"input_text","file_data":"data:application/pdf;base64,aGk="}`,
		`{"type":"input_file","file_data":"data:application/pdf;base64,aGk=","unknown":"secret"}`,
		`{"type":"input_audio","data":"secret"}`,
	} {
		out, err := New().SanitizeRequestWithPolicy(context.Background(), attachmentBody(part), newEnv(t).s, "extract_text")
		if out != nil || !errors.Is(err, adapter.ErrBadBody) {
			t.Fatalf("not refused: %s %v", out, err)
		}
		if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "example.invalid") {
			t.Fatalf("value leaked: %v", err)
		}
	}
}
func TestAttachmentsOnlyInUserContent(t *testing.T) {
	data := inlinePDF(t)
	part := `{"type":"input_file","file_data":"` + data + `"}`
	for _, body := range []string{
		`{"input":[{"role":"assistant","content":[` + part + `]}]}`,
		`{"input":[{"role":"user","role":"user","content":[` + part + `]}]}`,
		`{"input":[{"role":"user","output":[` + part + `]}]}`,
		`{"client_metadata":` + part + `}`,
	} {
		out, err := New().SanitizeRequestWithPolicy(context.Background(), []byte(body), newEnv(t).s, "extract_text")
		if out != nil || !errors.Is(err, adapter.ErrBadBody) {
			t.Fatalf("misplaced: %s %v", out, err)
		}
	}
}

func TestImageOCRTextIsSanitized(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	if err := os.WriteFile(dir+"/tesseract", []byte("#!/bin/sh\n/bin/cat >/dev/null\nprintf 'Contact 10.20.30.15'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, 10, 10))); err != nil {
		t.Fatal(err)
	}
	data := "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes())
	body := attachmentBody(`{"type":"input_image","image_url":"` + data + `","detail":"high"}`)
	out, err := New().SanitizeRequestWithPolicy(context.Background(), body, newEnv(t).s, "extract_text")
	if err != nil {
		t.Fatal(err)
	}
	assertAbsent(t, out, "10.20.30.15", data, "image_url", "detail")
	if !strings.Contains(string(out), `"type":"input_text"`) {
		t.Fatalf("not text: %s", out)
	}
	out, err = New().SanitizeRequestWithPolicy(context.Background(), body, strictSanitizer(t), "extract_text")
	if out != nil || !errors.Is(err, transform.ErrBlocked) {
		t.Fatalf("strict OCR: %s %v", out, err)
	}
}
