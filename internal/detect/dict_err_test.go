package detect

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"cui-proxy/internal/config"
)

func TestBuildErrorNeverContainsDictionaryText(t *testing.T) {
	var lines []string
	for i := 0; i < 400000; i++ {
		lines = append(lines, fmt.Sprintf("SENTINEL_%07d_zq", i))
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "d.txt"), []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	rules := []config.Rule{{Name: "people", Type: "dictionary", File: "d.txt", Synth: "email"}}

	check := func(label string) {
		_, err := Build(rules, dir)
		if err == nil {
			t.Logf("%s: dictionary compiled; no error to check", label)
			return
		}
		if strings.Contains(err.Error(), "SENTINEL_") {
			t.Fatalf("%s: error leaks dictionary text", label)
		}
		if len(err.Error()) > 300 {
			t.Fatalf("%s: unexpectedly long error", label)
		}
	}
	check("real") // real ErrLarge if the size limit is hit, else a no-op
	// Forced failure that, like regexp's ErrLarge, embeds the expression.
	old := compileDict
	compileDict = func(expr string) (*regexp.Regexp, error) {
		return nil, fmt.Errorf("error parsing regexp: expression too large: `%s`", expr)
	}
	check("forced")
	compileDict = old
}
