package vault

import "testing"

// The vault file identifier is a stable interface (docs/compatibility.md).
// A binary refuses a file whose identifier it does not know, so changing this
// value strands every existing vault. A new format needs a new identifier,
// a reader for this one, and the migration steps that document requires.
func TestStableFileFormatIdentifier(t *testing.T) {
	if got := string(fileMagic); got != "CUIV2" {
		t.Errorf("vault file identifier = %q, want %q", got, "CUIV2")
	}
}
