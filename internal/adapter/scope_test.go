package adapter

import (
	"os"
	"slices"
	"testing"
)

// The set of adapters is the set of clients the project supports. Adding one
// widens what the project has to review, measure and trial, so it has to be
// a deliberate change: read docs/adding-scope.md, then update this list in
// the same pull request.
var supportedAdapters = []string{"codex"}

func TestAdapterSetIsDeliberate(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		if e.IsDir() && e.Name() != "testdata" {
			got = append(got, e.Name())
		}
	}
	if !slices.Equal(got, supportedAdapters) {
		t.Fatalf("adapter directories = %v, want %v; see docs/adding-scope.md before adding a client", got, supportedAdapters)
	}
}
