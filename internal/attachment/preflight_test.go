package attachment

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakePreflightTools(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	for _, name := range []string{"pdfinfo", "pdftotext", "pdftoppm", "tesseract"} {
		body := "#!/bin/sh\nexit 0\n"
		if name == "tesseract" {
			body = "#!/bin/sh\n" + script
		}
		if os.WriteFile(filepath.Join(dir, name), []byte(body), 0700) != nil {
			t.Fatal("tool setup failed")
		}
	}
}
func TestPreflightMissingTool(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	checks := CheckTools(context.Background())
	if len(checks) != 5 {
		t.Fatal("missing checks")
	}
	for _, c := range checks {
		if c.Ready {
			t.Fatal("missing dependency reported ready")
		}
	}
}
func TestPreflightUsesExtractionEnvironment(t *testing.T) {
	t.Setenv("PRIVATE_TEST_MARKER", "must-not-inherit")
	fakePreflightTools(t, `if [ -n "$PRIVATE_TEST_MARKER" ] || [ "$LC_ALL" != C ] || [ "$OMP_THREAD_LIMIT" != 1 ] || [ "$1" != --list-langs ]; then exit 1; fi
printf 'List of available languages (1):\neng\n'
`)
	for _, c := range CheckTools(context.Background()) {
		if !c.Ready {
			t.Fatal("valid dependency not ready")
		}
	}
}
func TestPreflightMissingEnglishAndSafeOutput(t *testing.T) {
	fakePreflightTools(t, "printf 'private-diagnostic-location\nfra\n'\n")
	checks := CheckTools(context.Background())
	if checks[4].Code != "missing_english" {
		t.Fatal("missing language not diagnosed")
	}
	b, _ := json.Marshal(checks)
	if strings.Contains(string(b), "private-diagnostic") {
		t.Fatal("tool output leaked")
	}
}
func TestPreflightTimeout(t *testing.T) {
	fakePreflightTools(t, "exec /bin/sleep 10\n")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	checks := CheckTools(ctx)
	if time.Since(start) > 2*time.Second || checks[4].Ready {
		t.Fatal("probe not bounded")
	}
}
func TestPreflightOutputBound(t *testing.T) {
	fakePreflightTools(t, "while :; do printf '%01024d' 0; done\n")
	checks := CheckTools(context.Background())
	if checks[4].Code != "probe_limit" {
		t.Fatal("probe output not bounded")
	}
}
