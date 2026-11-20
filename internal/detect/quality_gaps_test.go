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

// A slice of a longer digit run is treated as a card only when it
// starts like a card of that length. Ordinary long integers otherwise often
// contain such a slice by chance.
func TestShortWindowsInDigitRunsNeedCardPrefix(t *testing.T) {
	luhnTail := func(body string) string { return withCheckDigit(t, body) }
	// pad returns lead + card, with lead chosen so that the whole run is not
	// itself Luhn-valid; only the embedded window can trigger.
	pad := func(card string) string {
		for _, lead := range []string{"9", "8", "7"} {
			if !luhnOK(lead + card) {
				return lead + card
			}
		}
		t.Fatal("no lead digit")
		return ""
	}
	masked := map[string]string{
		"13 digits starting with 4":  luhnTail("4" + "20000000000"[:11]),
		"14 digits starting with 36": luhnTail("36" + "50000000000"[:11]),
		"14 digits starting with 30": luhnTail("30" + "50000000000"[:11]),
		"15 digits starting with 34": luhnTail("34" + "500000000000"[:12]),
		"15 digits starting with 37": luhnTail("37" + "500000000000"[:12]),
		"16 digits starting with 5":  luhnTail("5" + "30000000000000"[:14]),
		"16 digits starting with 6":  luhnTail("6" + "01100000000000"[:14]),
		"16 digits starting with 4":  luhnTail("4" + "90000000000000"[:14]),
		"16 digits starting with 35": luhnTail("35" + "3000000000000"[:13]),
		"16 digits starting with 62": luhnTail("62" + "3000000000000"[:13]),
		"17 digits starting with 62": luhnTail("62" + "30000000000000"[:14]),
		"17 digits starting with 56": luhnTail("56" + "30000000000000"[:14]),
		"18 digits starting with 62": luhnTail("62" + "300000000000000"[:15]),
		"19 digits starting with 62": luhnTail("62" + "3000000000000000"[:16]),
		"19 digits starting with 4":  luhnTail("4" + "90000000000000000"[:17]),
		"19 digits starting with 35": luhnTail("35" + "3000000000000000"[:16]),
	}
	for name, card := range masked {
		run := pad(card)
		if g := builtinScan(t, "id "+run+" end"); len(g) != 1 || g[0].Class != "secret" || g[0].Text != run {
			t.Errorf("%s inside a longer run was not masked whole", name)
		}
	}
	skipped := map[string]string{
		"13 digits starting with 5":  luhnTail("5" + "20000000000"[:11]),
		"13 digits starting with 3":  luhnTail("3" + "20000000000"[:11]),
		"14 digits starting with 4":  luhnTail("4" + "500000000000"[:12]),
		"14 digits starting with 35": luhnTail("35" + "50000000000"[:11]),
		"15 digits starting with 4":  luhnTail("4" + "5000000000000"[:13]),
		"15 digits starting with 36": luhnTail("36" + "500000000000"[:12]),
		"16 digits starting with 33": luhnTail("33" + "7000000000000"[:13]),
		"16 digits starting with 59": luhnTail("59" + "7000000000000"[:13]),
		"16 digits starting with 61": luhnTail("61" + "7000000000000"[:13]),
		"16 digits starting with 69": luhnTail("69" + "7000000000000"[:13]),
		"17 digits starting with 4":  luhnTail("4" + "700000000000000"[:15]),
		"17 digits starting with 53": luhnTail("53" + "70000000000000"[:14]),
		"17 digits starting with 66": luhnTail("66" + "70000000000000"[:14]),
		"18 digits starting with 4":  luhnTail("4" + "7000000000000000"[:16]),
		"18 digits starting with 52": luhnTail("52" + "700000000000000"[:15]),
		"19 digits starting with 52": luhnTail("52" + "7000000000000000"[:16]),
		"19 digits starting with 61": luhnTail("61" + "7000000000000000"[:16]),
		"19 digits starting with 33": luhnTail("33" + "7000000000000000"[:16]),
	}
	for name, slice := range skipped {
		run := pad(slice)
		if cardRunFind(run, 0, len(run)) != nil {
			// Another window of the same run may be a hit; that is a property
			// of the sample, so only report it when the slice is the cause.
			if !otherWindowHit(run, slice) {
				t.Errorf("%s inside a longer run was masked", name)
			}
		}
	}
}

// otherWindowHit reports whether run has a Luhn-valid 13-19 digit window with
// a card prefix other than the given slice.
func otherWindowHit(run, slice string) bool {
	for st := 0; st+13 <= len(run); st++ {
		for n := 13; n <= 19 && st+n <= len(run); n++ {
			w := run[st : st+n]
			if w != slice && n != len(run) && w[0] >= '3' && w[0] <= '6' && cardSlicePrefix(run, st, n) && luhnOK(w) {
				return true
			}
		}
	}
	return false
}

// A whole digit group of card length that passes the Luhn test is still
// masked whatever it starts with. It cannot be told apart from a card.
func TestWholeLuhnValidGroupStillMasked(t *testing.T) {
	for _, body := range []string{"222300004840001", "979200000000000", "100000000000000", "860000000000000"} {
		n := withCheckDigit(t, body)
		if g := builtinScan(t, "n="+n+";"); len(g) != 1 || g[0].Text != n {
			t.Errorf("whole Luhn-valid group not masked")
		}
	}
}

// withCheckDigit appends the digit that makes body pass the Luhn test.
func withCheckDigit(t *testing.T, body string) string {
	t.Helper()
	for d := byte('0'); d <= '9'; d++ {
		if luhnOK(body + string(d)) {
			return body + string(d)
		}
	}
	t.Fatalf("no check digit for a %d-digit body", len(body))
	return ""
}
