package decisionlog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenRejectsOverflowingRetentionWithoutDeletingHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decisions.jsonl")
	clk := newClock()
	l := open(t, Options{Path: path, MaxBytes: 1, now: clk.now})
	write(t, l, 4)
	l.Close()
	before, err := Files(path)
	if err != nil {
		t.Fatal(err)
	}
	l, err = Open(Options{Path: path, RetentionDays: 106752, now: clk.now})
	if err == nil {
		l.Close()
		t.Error("accepted retention that overflows time.Duration")
	}
	for _, p := range before {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("fresh history removed by overflowing retention: %s: %v", filepath.Base(p), err)
		}
	}
}
