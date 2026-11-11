package attachment

import (
	"bytes"
	"context"
	"cui-proxy/internal/observe"
	"encoding/base64"
	"image"
	_ "image/jpeg" // decode dimensions before OCR
	_ "image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxBytes        = 10 << 20
	maxText         = 2 << 20
	maxPixels       = 16_000_000
	success   Cause = 255
)

var slots = make(chan struct{}, 2)

// Extractor budgets a single request. It is not shared between requests.
type Extractor struct {
	ctx                       context.Context
	cancel                    context.CancelFunc
	count, bytes, text, pages int
}

func New(ctx context.Context) *Extractor {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	return &Extractor{ctx: ctx, cancel: cancel}
}
func (e *Extractor) Close() { e.cancel() }

// Extract accepts only exact, inline base64 MIME forms. No URLs are fetched.
func (e *Extractor) Extract(c Category, data string) (string, error) {
	defer observe.FromContext(e.ctx).Extraction()()
	fail := func(why Cause) (string, error) { return "", &Error{c, why} }
	if e.ctx.Err() != nil {
		return fail(Timeout)
	}
	e.count++
	if e.count > 8 {
		return fail(Limit)
	}
	if !strings.HasPrefix(data, "data:") {
		return fail(Reference)
	}
	header, enc, ok := strings.Cut(data, ",")
	if !ok {
		return fail(Invalid)
	}
	format := ""
	switch {
	case c == File && header == "data:application/pdf;base64":
		c = PDF
	case c == Image && header == "data:image/png;base64":
		format = "png"
	case c == Image && header == "data:image/jpeg;base64":
		format = "jpeg"
	default:
		return fail(Unsupported)
	}
	if len(enc) > base64.StdEncoding.EncodedLen(maxBytes-e.bytes) {
		return fail(Limit)
	}
	b, err := base64.StdEncoding.Strict().DecodeString(enc)
	if err != nil || len(b) == 0 {
		return fail(Invalid)
	}
	e.bytes += len(b)
	if e.bytes > maxBytes {
		return fail(Limit)
	}
	var text string
	var why Cause
	if c == PDF {
		if !bytes.HasPrefix(b, []byte("%PDF-")) {
			return fail(Invalid)
		}
		text, why = e.pdf(b)
	} else {
		dims, actual, err := image.DecodeConfig(bytes.NewReader(b))
		if err != nil || actual != format {
			return fail(Invalid)
		}
		if dims.Width <= 0 || dims.Height <= 0 || int64(dims.Width)*int64(dims.Height) > maxPixels {
			return fail(Limit)
		}
		var out []byte
		out, why = e.run("tesseract", b, maxText-e.text, "stdin", "stdout", "-l", "eng")
		text = string(out)
	}
	if why != success {
		return fail(why)
	}
	if !utf8.ValidString(text) || strings.ContainsRune(text, '\x00') {
		return fail(Invalid)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return fail(Empty)
	}
	e.text += len(text)
	if e.text > maxText {
		return fail(Limit)
	}
	return text, nil
}

func (e *Extractor) pdf(b []byte) (string, Cause) {
	dir, err := os.MkdirTemp("", "cui-attachment-")
	if err != nil {
		return "", Failed
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "input.pdf")
	if err := os.WriteFile(path, b, 0600); err != nil {
		return "", Failed
	}
	info, why := e.run("pdfinfo", nil, 64<<10, path)
	if why != success {
		return "", why
	}
	pages := 0
	for _, line := range strings.Split(string(info), "\n") {
		if value, ok := strings.CutPrefix(line, "Pages:"); ok {
			pages, _ = strconv.Atoi(strings.TrimSpace(value))
		}
		if value, ok := strings.CutPrefix(line, "Encrypted:"); ok && !strings.HasPrefix(strings.TrimSpace(value), "no") {
			return "", Unsupported
		}
	}
	if pages < 1 {
		return "", Invalid
	}
	e.pages += pages
	if e.pages > 20 {
		return "", Limit
	}
	var result strings.Builder
	for page := 1; page <= pages; page++ {
		p := strconv.Itoa(page)
		remaining := maxText - e.text - result.Len()
		out, why := e.run("pdftotext", nil, remaining, "-f", p, "-l", p, "-enc", "UTF-8", "-nopgbrk", path, "-")
		if why != success {
			return "", why
		}
		if len(bytes.TrimSpace(out)) == 0 {
			// Bound raster dimensions before allocation; capture the PNG in memory.
			png, why := e.run("pdftoppm", nil, 64<<20, "-f", p, "-l", p, "-singlefile", "-scale-to", "4000", "-png", path)
			if why != success {
				return "", why
			}
			out, why = e.run("tesseract", png, remaining, "stdin", "stdout", "-l", "eng")
			if why != success {
				return "", why
			}
		}
		// Every page must yield text. A blank or unreadable page is refused.
		if len(bytes.TrimSpace(out)) == 0 {
			return "", Empty
		}
		if len(out)+1 > remaining {
			return "", Limit
		}
		result.Write(out)
		result.WriteByte('\n')
	}
	return result.String(), success
}

// boundedOutput discards bytes beyond the cap and stops the subprocess.
type boundedOutput struct {
	buf      bytes.Buffer
	limit    int
	overflow bool
	cancel   context.CancelFunc
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.buf.Len() {
		b.overflow = true
		b.cancel()
		return 0, io.ErrShortBuffer
	}
	return b.buf.Write(p)
}

func (e *Extractor) run(name string, input []byte, limit int, args ...string) ([]byte, Cause) {
	if e.ctx.Err() != nil {
		return nil, Timeout
	}
	if limit <= 0 {
		return nil, Limit
	}
	trace := observe.FromContext(e.ctx)
	endWait := trace.Measure(observe.ExtractionWait)
	var gauges observe.ExtractorGauges
	if trace != nil {
		gauges = trace.Gauges
	}
	if gauges != nil {
		gauges.ExtractorQueued(1)
	}
	acquired := false
	func() {
		defer endWait()
		if gauges != nil {
			defer gauges.ExtractorQueued(-1)
		}
		select {
		case slots <- struct{}{}:
			acquired = true
		case <-e.ctx.Done():
		}
	}()
	if !acquired {
		return nil, Timeout
	}
	if gauges != nil {
		gauges.ExtractorActive(1)
	}

	defer func() {
		if gauges != nil {
			gauges.ExtractorActive(-1)
		}
		<-slots
	}()
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, MissingTool
	}
	ctx, cancel := context.WithCancel(e.ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	// No shell, user config or inherited credentials. English OCR is explicit.
	cmd.Env = []string{"LC_ALL=C", "OMP_THREAD_LIMIT=1"}
	cmd.Stdin = bytes.NewReader(input)
	out := &boundedOutput{limit: limit, cancel: cancel}
	cmd.Stdout, cmd.Stderr = out, io.Discard
	cmd.WaitDelay = time.Second
	err = cmd.Run()
	if out.overflow {
		return nil, Limit
	}
	if e.ctx.Err() != nil {
		return nil, Timeout
	}
	if err != nil {
		return nil, Failed
	}
	return out.buf.Bytes(), success
}
