package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A missing population or an incorrect class must not produce a successful
// seeded fixture; no timing run may silently start with an empty vault.
func TestSeedRejectsMissingCorpus(t *testing.T) {
	d := t.TempDir()
	if _, err := seed(d); err == nil {
		t.Fatal("missing corpus accepted")
	}
}
func TestSeedPersistsPopulation(t *testing.T) {
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "pairs.json"), []byte(`[["secret","NotebookAlpha"],["secret","NotebookBeta"]]`), 0600); err != nil {
		t.Fatal(err)
	}
	n, err := seed(d)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("entries: %d", n)
	}
	n, err = seed(d)
	if err != nil || n != 2 {
		t.Fatal("repeat seed changed population")
	}
}
