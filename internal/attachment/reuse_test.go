package attachment

import (
	"bytes"
	"context"
	"cui-proxy/internal/observe"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func reuseTool(t *testing.T, output string) (string, *textCache) {
	t.Helper()
	dir := t.TempDir()
	counter := filepath.Join(dir, "calls")
	// Test paths are quoted for the shell; only synthetic output is emitted.
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	fakeTool(t, dir, "tesseract", "printf 'x\n' >> "+quote(counter)+"\nprintf '%s' "+quote(output)+"\n")
	t.Setenv("PATH", dir)
	c := newTextCache(32, 4<<20, 30*time.Second)
	t.Cleanup(c.close)
	return counter, c
}

func reuseExtract(ctx context.Context, c *textCache, data string) (string, error) {
	e := New(ctx)
	e.cache = c
	defer e.Close()
	return e.Extract(Image, data)
}

func reuseCalls(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(b), "\n")
}

func requireReuseCause(t *testing.T, err error, want Cause) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Cause != want {
		t.Fatalf("unexpected extraction cause: %T", err)
	}
}

func TestImageReuseAcrossRequests(t *testing.T) {
	counter, c := reuseTool(t, "  alpha  \n")
	data := pngData(t, 2, 2)
	for range 3 {
		out, err := reuseExtract(context.Background(), c, data)
		if err != nil || out != "alpha" {
			t.Fatal("extraction failed")
		}
	}
	if reuseCalls(t, counter) != 1 {
		t.Fatal("identical images repeated helper work")
	}
	if _, err := reuseExtract(context.Background(), c, pngData(t, 3, 2)); err != nil {
		t.Fatal(err)
	}
	if reuseCalls(t, counter) != 2 {
		t.Fatal("different image reused output")
	}
}

func TestImageReuseChargesBudgets(t *testing.T) {
	counter, c := reuseTool(t, "  alpha  \n")
	data := pngData(t, 2, 2)
	if _, err := reuseExtract(context.Background(), c, data); err != nil {
		t.Fatal(err)
	}
	for _, setup := range []func(*Extractor){
		func(e *Extractor) { e.count = 8 },
		func(e *Extractor) { e.bytes = maxBytes },
		// Trimmed text fits; the original helper-output bytes do not.
		func(e *Extractor) { e.text = maxText - 7 },
	} {
		e := New(context.Background())
		e.cache = c
		setup(e)
		_, err := e.Extract(Image, data)
		e.Close()
		requireReuseCause(t, err, Limit)
	}
	e := New(context.Background())
	e.cache = c
	defer e.Close()
	if _, err := e.Extract(Image, data); err != nil {
		t.Fatal(err)
	}
	_, encoded, _ := strings.Cut(data, ",")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if e.count != 1 || e.bytes != len(decoded) || e.text != 5 {
		t.Fatal("cache hit did not charge logical budgets")
	}
	if reuseCalls(t, counter) != 1 {
		t.Fatal("budget checks unnecessarily invoked helper")
	}
}

func TestImageReuseRejectsExpiredContext(t *testing.T) {
	counter, c := reuseTool(t, "alpha\n")
	data := pngData(t, 2, 2)
	if _, err := reuseExtract(context.Background(), c, data); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := reuseExtract(ctx, c, data)
	requireReuseCause(t, err, Timeout)
	if reuseCalls(t, counter) != 1 {
		t.Fatal("cancelled hit invoked helper")
	}
}

func TestImageReuseDoesNotCacheFailures(t *testing.T) {
	counter, c := reuseTool(t, "")
	data := pngData(t, 2, 2)
	for range 2 {
		_, err := reuseExtract(context.Background(), c, data)
		requireReuseCause(t, err, Empty)
	}
	if reuseCalls(t, counter) != 2 {
		t.Fatal("empty extraction cached")
	}
	dir := filepath.Dir(counter)
	fakeTool(t, dir, "tesseract", fmt.Sprintf("printf 'x\n' >> '%s'\nexit 1\n", counter))
	for range 2 {
		_, err := reuseExtract(context.Background(), c, data)
		requireReuseCause(t, err, Failed)
	}
	if reuseCalls(t, counter) != 4 {
		t.Fatal("failed extraction cached")
	}
}

func TestImageReuseCancellationDoesNotPoisonOtherCaller(t *testing.T) {
	counter, c := reuseTool(t, "unused")
	dir := filepath.Dir(counter)
	release := filepath.Join(dir, "release")
	fakeTool(t, dir, "tesseract", fmt.Sprintf("printf 'x\n' >> '%s'\nwhile [ ! -f '%s' ]; do :; done\nprintf 'alpha\n'\n", counter, release))
	data := pngData(t, 2, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var first, second error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, first = reuseExtract(ctx, c, data) }()
	go func() { defer wg.Done(); _, second = reuseExtract(context.Background(), c, data) }()
	deadline := time.Now().Add(time.Second)
	for reuseCalls(t, counter) < 2 {
		if time.Now().After(deadline) {
			cancel()
			os.WriteFile(release, nil, 0600)
			wg.Wait()
			t.Fatal("independent cold helpers did not start")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	requireReuseCause(t, first, Timeout)
	if second != nil {
		t.Fatal("cancelled caller poisoned independent extraction")
	}
	if _, err := reuseExtract(context.Background(), c, data); err != nil {
		t.Fatal(err)
	}
	if reuseCalls(t, counter) != 2 {
		t.Fatal("independent success was not reusable")
	}
}

func TestImageReuseChecksCurrentHelper(t *testing.T) {
	counter, c := reuseTool(t, "alpha\n")
	data := pngData(t, 2, 2)
	if _, err := reuseExtract(context.Background(), c, data); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(counter)
	fakeTool(t, dir, "tesseract", fmt.Sprintf("printf 'x\n' >> '%s'\nprintf 'beta\n'\n", counter))
	out, err := reuseExtract(context.Background(), c, data)
	if err != nil || out != "beta" {
		t.Fatal("helper replacement reused stale result")
	}
	if reuseCalls(t, counter) != 2 {
		t.Fatal("helper replacement did not run")
	}
	if err := os.Remove(filepath.Join(dir, "tesseract")); err != nil {
		t.Fatal(err)
	}
	_, err = reuseExtract(context.Background(), c, data)
	requireReuseCause(t, err, MissingTool)
}

func TestImageReuseDoesNotCacheInvalidOrLimitedOutput(t *testing.T) {
	counter, c := reuseTool(t, "alpha\n")
	data := pngData(t, 2, 2)
	for range 2 {
		e := New(context.Background())
		e.cache = c
		e.text = maxText - 3
		_, err := e.Extract(Image, data)
		e.Close()
		requireReuseCause(t, err, Limit)
	}
	if reuseCalls(t, counter) != 2 {
		t.Fatal("limited output cached")
	}
	fakeTool(t, filepath.Dir(counter), "tesseract", fmt.Sprintf("printf 'x\n' >> '%s'\nprintf '\\377'\n", counter))
	for range 2 {
		_, err := reuseExtract(context.Background(), c, data)
		requireReuseCause(t, err, Invalid)
	}
	if reuseCalls(t, counter) != 4 {
		t.Fatal("invalid output cached")
	}
}

func TestImageReuseEmitsCacheHitAccounting(t *testing.T) {
	_, c := reuseTool(t, "alpha\n")
	data := pngData(t, 2, 2)
	if _, err := reuseExtract(context.Background(), c, data); err != nil {
		t.Fatal(err)
	}
	trace := observe.NewTrace()
	ctx := observe.WithTrace(context.Background(), trace)
	if _, err := reuseExtract(ctx, c, data); err != nil {
		t.Fatal(err)
	}
	s := trace.Snapshot()
	if s.ExtractionCacheHits != 1 || !s.Observed[observe.ExtractionWait] || s.Duration[observe.ExtractionWait] != 0 || !s.Observed[observe.ExtractionRun] {
		t.Fatal("cached extraction accounting incomplete")
	}
}

func TestJPEGReuseAcrossRequests(t *testing.T) {
	counter, c := reuseTool(t, "alpha\n")
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	data := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes())
	for range 2 {
		out, err := reuseExtract(context.Background(), c, data)
		if err != nil || out != "alpha" {
			t.Fatal("JPEG reuse failed")
		}
	}
	if reuseCalls(t, counter) != 1 {
		t.Fatal("JPEG image repeated helper work")
	}
}
