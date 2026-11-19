package detect

import (
	"strings"
	"testing"

	"cui-proxy/internal/config"
)

// Sample values here are assembled from parts so that the source holds no
// literal address or mailbox.

func builtinForTest(t *testing.T) []Detector {
	t.Helper()
	ds, err := Build([]config.Rule{{Name: "builtin", Type: "builtin"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

// covered reports whether detected spans cover every byte of value in text.
func covered(ds []Detector, text, value string) bool {
	start := strings.Index(text, value)
	seen := make([]bool, len(value))
	for _, sp := range Scan(ds, text) {
		for i := max(sp.Start, start); i < min(sp.End, start+len(value)); i++ {
			seen[i-start] = true
		}
	}
	for _, ok := range seen {
		if !ok {
			return false
		}
	}
	return start >= 0
}

// An apostrophe is valid in a mailbox local part. The part before it must
// not be left behind.
func TestEmailWithApostropheIsFullyMasked(t *testing.T) {
	ds := builtinForTest(t)
	domain := "example" + ".org"
	for _, local := range []string{"o" + "'" + "neil", "d" + "'" + "arcy.lee", "a" + "'" + "b" + "'" + "c"} {
		value := local + "@" + domain
		for _, text := range []string{
			"write to " + value + " today",
			"set to '" + value + "' by default",
			`{"to": "` + value + `"}`,
		} {
			if !covered(ds, text, value) {
				t.Errorf("mailbox with %d apostrophe(s) not fully covered in a %d-byte context", strings.Count(local, "'"), len(text))
			}
		}
	}
}

// A quote that only surrounds a mailbox is not part of it.
func TestEmailQuotesAreNotPartOfTheMatch(t *testing.T) {
	ds := builtinForTest(t)
	value := "sam" + "@" + "example" + ".org"
	text := "from='" + value + "' to='" + value + "'"
	spans := Scan(ds, text)
	if len(spans) != 2 {
		t.Fatalf("got %d spans, want 2", len(spans))
	}
	for _, sp := range spans {
		if text[sp.Start:sp.End] != value {
			t.Errorf("span of %d bytes is not exactly the %d-byte mailbox", sp.End-sp.Start, len(value))
		}
	}
}

// An address that ends in "::" can be directly followed by a colon, as in
// "<address>: accepted". The extra colon must not hide the address.
func TestIPv6EndingInDoubleColonBeforeColon(t *testing.T) {
	ds := builtinForTest(t)
	value := strings.Join([]string{"2001", "db8", "5"}, ":") + "::"
	for _, text := range []string{value + ": accepted", "route " + value + ":", value + ":\n"} {
		if !covered(ds, text, value) {
			t.Errorf("address ending in a double colon not covered in a %d-byte context", len(text))
		}
		for _, sp := range Scan(ds, text) {
			if sp.Class == "ipv6" && text[sp.Start:sp.End] != value {
				t.Errorf("ipv6 span of %d bytes is not exactly the %d-byte address", sp.End-sp.Start, len(value))
			}
		}
	}
}
