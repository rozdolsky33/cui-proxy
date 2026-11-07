package detect

import (
	"strings"
	"testing"
	"time"

	"cui-proxy/internal/config"
)

// Long punctuation runs must not cause each address candidate suffix to be
// scanned again. The generous deadline distinguishes linear scanning from
// the previous quadratic path, which needs seconds for only 32 KiB.
func TestLongAddressCandidatesCompletePromptly(t *testing.T) {
	ds, err := Build([]config.Rule{{Name: "builtin", Type: "builtin"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Repeat(".", 128<<10) + " " +
		strings.Repeat(":", 128<<10) + " " +
		strings.Repeat(":", 128<<10) + "/128 " +
		"ffff:ffff:ffff:ffff:ffff:ffff:255.255.255.255 " +
		"ffff:ffff:ffff:ffff:ffff:ffff:255.255.255.255.65535 " +
		"ffff:ffff:ffff:ffff:ffff:ffff:255.255.255.255/128"
	done := make(chan []Span, 1)
	go func() { done <- Scan(ds, text) }()
	select {
	case spans := <-done:
		for _, value := range []string{
			"ffff:ffff:ffff:ffff:ffff:ffff:255.255.255.255",
			"ffff:ffff:ffff:ffff:ffff:ffff:255.255.255.255.65535",
			"ffff:ffff:ffff:ffff:ffff:ffff:255.255.255.255/128",
		} {
			found := false
			for _, sp := range spans {
				found = found || text[sp.Start:sp.End] == value
			}
			if !found {
				t.Errorf("maximum-length address form not detected: %q", value)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("scanning less than 400 KiB of punctuation exceeded five seconds")
	}
}
