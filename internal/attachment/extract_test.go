package attachment

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/png"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestExtractPDF(t *testing.T) {
	for _, tool := range []string{"pdfinfo", "pdftotext"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip("requires Poppler")
		}
	}
	b, err := os.ReadFile("testdata/text.pdf")
	if err != nil {
		t.Fatal(err)
	}
	e := New(context.Background())
	defer e.Close()
	text, err := e.Extract(File, "data:application/pdf;base64,"+base64.StdEncoding.EncodeToString(b))
	if err != nil || !strings.Contains(text, "10.20.30.15") {
		t.Fatalf("PDF text %q, %v", text, err)
	}
}

func TestExtractionRefusals(t *testing.T) {
	for _, tc := range []struct {
		data string
		c    Category
		why  Cause
	}{
		{"https://example.invalid/secret.pdf", File, Reference},
		{"data:application/pdf;base64,%%%", File, Invalid},
		{"data:text/plain;base64,aGk=", File, Unsupported},
		{"data:application/pdf;base64,aGk=", File, Invalid},
		{"data:image/png;base64,aGk=", Image, Invalid},
		{"data:image/gif;base64,aGk=", Image, Unsupported},
		{"data:image/png;base64," + strings.Repeat("A", 15<<20), Image, Limit},
	} {
		t.Run(tc.data[:min(25, len(tc.data))], func(t *testing.T) {
			e := New(context.Background())
			defer e.Close()
			out, err := e.Extract(tc.c, tc.data)
			var ae *Error
			if out != "" || !errors.As(err, &ae) || ae.Cause != tc.why {
				t.Fatalf("got %q %v, want %v", out, err, tc.why)
			}
		})
	}
}

func TestExtractionMissingToolAndCancellation(t *testing.T) {
	b, err := os.ReadFile("testdata/text.pdf")
	if err != nil {
		t.Fatal(err)
	}
	data := "data:application/pdf;base64," + base64.StdEncoding.EncodeToString(b)
	t.Setenv("PATH", t.TempDir())
	e := New(context.Background())
	defer e.Close()
	_, err = e.Extract(File, data)
	var ae *Error
	if !errors.As(err, &ae) || ae.Cause != MissingTool {
		t.Fatalf("missing tool: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e2 := New(ctx)
	defer e2.Close()
	_, err = e2.Extract(File, data)
	if !errors.As(err, &ae) || ae.Cause != Timeout {
		t.Fatalf("cancelled: %v", err)
	}
}

func TestCommandBounds(t *testing.T) {
	e := New(context.Background())
	defer e.Close()
	_, why := e.run("sh", nil, 32, "-c", "printf '%100s' x")
	if why != Limit {
		t.Fatalf("oversize output: %v", why)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	e2 := New(ctx)
	defer e2.Close()
	_, why = e2.run("sh", nil, 32, "-c", "exec sleep 5")
	if why != Timeout {
		t.Fatalf("timeout: %v", why)
	}
}

// The fake OCR executable isolates external tool availability. Input validation,
// command invocation, byte budgets and result handling remain real.
func fakeTool(t *testing.T, dir, name, script string) {
	t.Helper()
	if err := os.WriteFile(dir+"/"+name, []byte("#!/bin/sh\n"+script), 0700); err != nil {
		t.Fatal(err)
	}
}
func pngData(t *testing.T, width, height int) string {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes())
}
func TestImageExtractionAndLimits(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	fakeTool(t, dir, "tesseract", `[ "$1 $2 $3 $4" = "stdin stdout -l eng" ] || exit 1
/bin/cat >/dev/null
printf 'Contact 10.20.30.15'
`)
	data := pngData(t, 10, 10)
	e := New(context.Background())
	defer e.Close()
	for i := 0; i < 8; i++ {
		out, err := e.Extract(Image, data)
		if err != nil || out != "Contact 10.20.30.15" {
			t.Fatalf("OCR: %q %v", out, err)
		}
	}
	_, err := e.Extract(Image, data)
	var ae *Error
	if !errors.As(err, &ae) || ae.Cause != Limit {
		t.Fatalf("count: %v", err)
	}
	e2 := New(context.Background())
	defer e2.Close()
	_, err = e2.Extract(Image, pngData(t, 4001, 4000))
	if !errors.As(err, &ae) || ae.Cause != Limit {
		t.Fatalf("pixels: %v", err)
	}
}
func TestImageEmptyAndToolFailure(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	data := pngData(t, 1, 1)
	for _, tc := range []struct {
		script string
		why    Cause
	}{
		{"/bin/cat >/dev/null\n", Empty},
		{"/bin/cat >/dev/null\necho private-secret >&2\nexit 1\n", Failed},
		{"/bin/cat >/dev/null\nprintf '\\377'\n", Invalid},
	} {
		fakeTool(t, dir, "tesseract", tc.script)
		e := New(context.Background())
		out, err := e.Extract(Image, data)
		e.Close()
		var ae *Error
		if out != "" || !errors.As(err, &ae) || ae.Cause != tc.why || strings.Contains(err.Error(), "private-secret") {
			t.Fatalf("failure: %q %v", out, err)
		}
	}
}
func TestPDFPageBudgetIsPerRequest(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	fakeTool(t, dir, "pdfinfo", "printf 'Pages: 11\\nEncrypted: no\\n'\n")
	fakeTool(t, dir, "pdftotext", "printf 'readable page'\n")
	data := "data:application/pdf;base64," + base64.StdEncoding.EncodeToString([]byte("%PDF-1.4 test"))
	e := New(context.Background())
	defer e.Close()
	if _, err := e.Extract(File, data); err != nil {
		t.Fatal(err)
	}
	_, err := e.Extract(File, data)
	var ae *Error
	if !errors.As(err, &ae) || ae.Cause != Limit {
		t.Fatalf("aggregate pages: %v", err)
	}
}

func TestPDFScanUsesBoundedRasterThenOCR(t *testing.T) {
	for _, tool := range []string{"pdfinfo", "pdftotext", "pdftoppm"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip("requires Poppler")
		}
	}
	b, err := os.ReadFile("testdata/text.pdf")
	if err != nil {
		t.Fatal(err)
	}
	// Keep the synthetic PDF's byte offsets valid while making its page blank.
	b = bytes.ReplaceAll(b, []byte("Contact alice@example.test at 10.20.30.15"), []byte(strings.Repeat(" ", len("Contact alice@example.test at 10.20.30.15"))))
	dir := t.TempDir()
	fakeTool(t, dir, "tesseract", `[ "$1 $2 $3 $4" = "stdin stdout -l eng" ] || exit 1
/bin/cat >/dev/null
printf 'readable OCR text'
`)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	e := New(context.Background())
	defer e.Close()
	out, err := e.Extract(File, "data:application/pdf;base64,"+base64.StdEncoding.EncodeToString(b))
	if err != nil || out != "readable OCR text" {
		t.Fatalf("scan: %q %v", out, err)
	}
}
