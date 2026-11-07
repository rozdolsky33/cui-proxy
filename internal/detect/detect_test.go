package detect

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"cui-proxy/internal/config"
)

type got struct{ Class, Text string }

func builtinScan(t *testing.T, s string) []got {
	t.Helper()
	ds, err := Build([]config.Rule{{Name: "builtin", Type: "builtin"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	var out []got
	for _, sp := range Scan(ds, s) {
		out = append(out, got{sp.Class, s[sp.Start:sp.End]})
	}
	return out
}

func TestBuiltinDetectors(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjMifQ.c2lnbmF0dXJl"
	pem := "-----BEGIN RSA PRIVATE KEY-----\nMIIabc\ndef==\n-----END RSA PRIVATE KEY-----"
	tests := []struct {
		name, in string
		want     []got
	}{
		{"ocid", "x ocid1.instance.oc2.us-gov-ashburn-1.abcd1234 y", []got{{"ocid", "ocid1.instance.oc2.us-gov-ashburn-1.abcd1234"}}},
		{"ocid empty region", "ocid1.tenancy.oc2..aaaa", []got{{"ocid", "ocid1.tenancy.oc2..aaaa"}}},
		{"ipv4", "host 10.20.30.15 up", []got{{"ipv4", "10.20.30.15"}}},
		{"ipv4 short", "1.2.3", nil},
		{"ipv4 bad octet", "999.1.1.1", nil},
		{"ipv4 trailing dot", "reach 10.1.2.3.", []got{{"ipv4", "10.1.2.3"}}},
		{"ipv6", "addr fe80::1ff:fe23:4567:890a end", []got{{"ipv6", "fe80::1ff:fe23:4567:890a"}}},
		{"ipv6 after colon", "addr:2001:db8::1", []got{{"ipv6", "2001:db8::1"}}},
		{"ipv6 loopback", "::1", []got{{"ipv6", "::1"}}},
		{"not ipv6 time", "at 12:30:45 today", nil},
		{"not ipv6 mac", "aa:bb:cc:dd:ee:ff", nil},
		{"not ipv6 cpp", "std::string", nil},
		{"cidr", "net 10.0.0.0/24 ok", []got{{"cidr", "10.0.0.0/24"}}},
		{"cidr v6", "net 2001:db8::/32 ok", []got{{"cidr", "2001:db8::/32"}}},
		{"cidr bad prefix", "10.0.0.0/99", []got{{"ipv4", "10.0.0.0"}}},
		{"email", "mail bob.smith+x@corp.example.com now", []got{{"email", "bob.smith+x@corp.example.com"}}},
		{"email none", "a@b", nil},
		{"ssn", "ssn 123-45-6789.", []got{{"ssn", "123-45-6789"}}},
		{"ssn area 000", "000-12-3456", nil},
		{"ssn area 666", "666-12-3456", nil},
		{"ssn area 9xx", "923-12-3456", nil},
		{"ssn group 00", "123-00-6789", nil},
		{"ssn serial 0000", "123-45-0000", nil},
		{"card spaces", "pay 4111 1111 1111 1111 ok", []got{{"card", "4111 1111 1111 1111"}}},
		{"card dashes", "4111-1111-1111-1111", []got{{"card", "4111-1111-1111-1111"}}},
		{"card plain", "4111111111111111", []got{{"card", "4111111111111111"}}},
		{"card luhn bad", "4111 1111 1111 1112", nil},
		{"bearer", "Authorization: Bearer abcdef0123456789xyz", []got{{"secret", "abcdef0123456789xyz"}}},
		{"bearer jwt", "Bearer " + jwt, []got{{"secret", jwt}}},
		{"bearer keyword inside token", "x=Bearer abcdefgh1=0=Bearer abcdefgh12345678=0", []got{{"secret", "abcdefgh1=0=Bearer"}, {"secret", "abcdefgh12345678=0"}}},
		{"jwt", "token=" + jwt + ";", []got{{"secret", jwt}}},
		{"private key", "k:\n" + pem + "\nafter", []got{{"secret", pem}}},
		{"private key unterminated", "-----BEGIN PRIVATE KEY-----\nabc", []got{{"secret", "-----BEGIN PRIVATE KEY-----\nabc"}}},
		{"plain text", "hello world, nothing to see", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if g := builtinScan(t, tc.in); !reflect.DeepEqual(g, tc.want) {
				t.Errorf("Scan(%q) = %v, want %v", tc.in, g, tc.want)
			}
		})
	}
}

func TestIPv4SkipsVersionLikeValues(t *testing.T) {
	for _, in := range []string{"v1.2.3.4", "go1.26.3.1", "1.2.3.4.5", "a1.2.3.4", "11.2.3.45x", "1.2.3.4a"} {
		if g := builtinScan(t, in); g != nil {
			t.Errorf("%q: unexpected spans %v", in, g)
		}
	}
}

func TestScanLongestSpanWins(t *testing.T) {
	g := builtinScan(t, "ocid1.instance.oc2.us-gov-ashburn-1.abcd1234")
	if len(g) != 1 || g[0].Class != "ocid" {
		t.Errorf("ocid: %v", g)
	}
	g = builtinScan(t, "10.0.0.0/24")
	if len(g) != 1 || g[0].Class != "cidr" {
		t.Errorf("cidr: %v", g)
	}
	// The email extends past the ocid-like prefix, so it is not "inside" the
	// ocid: the two partially overlap and merge into one email-class span
	// covering everything (longest wins the class, no unmasked remainder).
	in := "ocid1.user.oc2..bob@corp.example.com"
	if g := builtinScan(t, in); !reflect.DeepEqual(g, []got{{"email", in}}) {
		t.Errorf("ocid/email overlap: %v", g)
	}
}

func TestResolveTiesAndOrdering(t *testing.T) {
	// Equal span: cidr beats ipv4 on specificity; far span untouched.
	got := resolve([]Span{{Start: 5, End: 9, Rule: "a", Class: "ipv4"}, {Start: 5, End: 9, Rule: "b", Class: "cidr"}, {Start: 20, End: 22, Rule: "z", Class: "q"}})
	if len(got) != 2 || got[0].Class != "cidr" || got[1].Start != 20 {
		t.Errorf("tie: %v", got)
	}
}

func TestResolveMergesPartialOverlap(t *testing.T) {
	got := resolve([]Span{
		{Start: 0, End: 6, Rule: "long", Class: "x"},
		{Start: 4, End: 8, Rule: "short", Class: "y"},
	})
	want := []Span{{Start: 0, End: 8, Rule: "long", Class: "x"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("partial overlap: got %v want %v", got, want)
	}
	// Chain: a-b and b-c overlap through b; one union span.
	got = resolve([]Span{{Start: 0, End: 5, Rule: "a", Class: "x"}, {Start: 4, End: 10, Rule: "b", Class: "y"}, {Start: 9, End: 12, Rule: "c", Class: "z"}})
	if len(got) != 1 || got[0].Start != 0 || got[0].End != 12 || got[0].Class != "y" {
		t.Errorf("chain: %v", got)
	}
	// Fully contained span is dropped; touching spans stay separate.
	got = resolve([]Span{{Start: 0, End: 10, Rule: "big", Class: "x"}, {Start: 2, End: 4, Rule: "in", Class: "y"}, {Start: 10, End: 12, Rule: "next", Class: "z"}})
	want = []Span{{Start: 0, End: 10, Rule: "big", Class: "x"}, {Start: 10, End: 12, Rule: "next", Class: "z"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("contained: got %v want %v", got, want)
	}
}

func TestResolveScalesLinearish(t *testing.T) {
	const n = 100000
	spans := make([]Span, n)
	for i := range spans {
		spans[i] = Span{Start: i * 3, End: i*3 + 2, Rule: "r", Class: "x"}
	}
	start := time.Now()
	got := resolve(spans)
	if len(got) != n {
		t.Fatalf("len %d", len(got))
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("resolve of %d disjoint spans took %v", n, d)
	}
}

func writeDict(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDictionaryMatchesWholeEntriesCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	writeDict(t, dir, "hosts.txt", "# internal hosts\n\ndb1.corp.local\nDB1\n  web-01  \nacme corp\n")
	ds, err := Build([]config.Rule{{Name: "hosts", Type: "dictionary", File: "hosts.txt", Synth: "hostname"}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	s := "connect to DB1.Corp.Local and db1, not xdb1 or db10; Web-01, ACME CORP; # internal hosts"
	var gs []got
	for _, sp := range Scan(ds, s) {
		if sp.Rule != "hosts" || sp.Class != "hostname" {
			t.Errorf("span meta: %+v", sp)
		}
		gs = append(gs, got{sp.Class, s[sp.Start:sp.End]})
	}
	want := []got{{"hostname", "DB1.Corp.Local"}, {"hostname", "db1"}, {"hostname", "Web-01"}, {"hostname", "ACME CORP"}}
	if !reflect.DeepEqual(gs, want) {
		t.Errorf("got %v want %v", gs, want)
	}
}

func TestDictionaryAbsolutePathAndMissingFile(t *testing.T) {
	dir := t.TempDir()
	writeDict(t, dir, "e.txt", "# only comments\nx.example\n")
	abs := filepath.Join(dir, "e.txt")
	ds, err := Build([]config.Rule{{Name: "e", Type: "dictionary", File: abs, Synth: "hostname"}}, "/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if sp := Scan(ds, "see x.example"); len(sp) != 1 {
		t.Errorf("abs path dict spans: %v", sp)
	}
	if _, err := Build([]config.Rule{{Name: "m", Type: "dictionary", File: "nope.txt", Synth: "hostname"}}, dir); err == nil {
		t.Error("missing dictionary file must fail Build")
	}
}

// Finding 12: an effectively empty dictionary silently disables the rule.
func TestEmptyDictionaryFileIsAnError(t *testing.T) {
	dir := t.TempDir()
	writeDict(t, dir, "empty.txt", "# only comments\n\n  \n")
	_, err := Build([]config.Rule{{Name: "e", Type: "dictionary", File: "empty.txt", Synth: "hostname"}}, dir)
	if err == nil || !strings.Contains(err.Error(), "empty.txt") {
		t.Errorf("want error naming the file, got %v", err)
	}
}

func TestBuildRegexRule(t *testing.T) {
	ds, err := Build([]config.Rule{{Name: "ticket", Type: "regex", Pattern: `TKT-\d+`, Synth: "secret"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	s := "see TKT-42"
	sp := Scan(ds, s)
	if len(sp) != 1 || sp[0].Rule != "ticket" || sp[0].Class != "secret" || s[sp[0].Start:sp[0].End] != "TKT-42" {
		t.Errorf("%+v", sp)
	}
	ds, err = Build([]config.Rule{{Name: "card", Type: "regex", Pattern: `\d{4}`, Validate: "luhn", Synth: "card"}}, "")
	if err != nil || len(Scan(ds, "1234")) != 0 {
		t.Errorf("luhn validator: %v", err)
	}
	for _, r := range []config.Rule{
		{Name: "x", Type: "regex", Pattern: "(", Synth: "a"},
		{Name: "x", Type: "regex", Pattern: "a", Validate: "bogus", Synth: "a"},
		{Name: "x", Type: "weird", Synth: "a"},
	} {
		if _, err := Build([]config.Rule{r}, ""); err == nil {
			t.Errorf("rule %+v should fail", r)
		}
	}
}

func TestValidators(t *testing.T) {
	if !luhnOK("4111 1111 1111 1111") || luhnOK("4111 1111 1111 1112") || luhnOK("123") {
		t.Error("luhn")
	}
	if !ssnOK("123-45-6789") || ssnOK("000-12-3456") || ssnOK("666-12-3456") || ssnOK("900-12-3456") || ssnOK("123-00-6789") || ssnOK("123-45-0000") {
		t.Error("ssn")
	}
}

// Fix round 1 regressions (findings 1-9).
func TestFixRound1Regressions(t *testing.T) {
	tests := []struct {
		name, in string
		want     []got
	}{
		{"1 card plus CVV", "4111 1111 1111 1111 123", []got{{"card", "4111 1111 1111 1111"}}},
		{"1 card after year", "2024 4111 1111 1111 1111", []got{{"card", "4111 1111 1111 1111"}}},
		{"2 ipv6 after id:", "id:fe80::1", []got{{"ipv6", "fe80::1"}}},
		{"2 ipv6 after via:", "via:fe80::1", []got{{"ipv6", "fe80::1"}}},
		{"2 cidr6 after via:", "via:fe80::/64", []got{{"cidr", "fe80::/64"}}},
		{"3 ipv4 leading zeros", "192.168.001.001", []got{{"ipv4", "192.168.001.001"}}},
		{"3 ipv4 leading zeros still bound", "999.168.001.001", nil},
		{"5 bearer 8 chars", "Bearer abcd1234", []got{{"secret", "abcd1234"}}},
		{"5 bearer 7 chars", "Bearer abcd123", nil},
		{"6 ocid after dash", "opc-ocid1.tenancy.oc2..aaaa", []got{{"ocid", "ocid1.tenancy.oc2..aaaa"}}},
		{"6 ocid after underscore", "x_ocid1.tenancy.oc2..aaaa", []got{{"ocid", "ocid1.tenancy.oc2..aaaa"}}},
		{"6 ocid after letter", "myocid1.tenancy.oc2..aaaa", nil},
		{"7 ssn after underscore", "_123-45-6789", []got{{"ssn", "123-45-6789"}}},
		{"7 card after letter", "x4111111111111111", []got{{"card", "4111111111111111"}}},
		{"7 card after underscore", "id_4111111111111111", []got{{"card", "4111111111111111"}}},
		{"8 email unicode local", "mail josé@corp.com now", []got{{"email", "josé@corp.com"}}},
		{"9 pgp block", "-----BEGIN PGP PRIVATE KEY BLOCK-----\nabc\n-----END PGP PRIVATE KEY BLOCK-----", []got{{"secret", "-----BEGIN PGP PRIVATE KEY BLOCK-----\nabc\n-----END PGP PRIVATE KEY BLOCK-----"}}},
		{"9 pgp unterminated", "-----BEGIN PGP PRIVATE KEY BLOCK-----\nabc", []got{{"secret", "-----BEGIN PGP PRIVATE KEY BLOCK-----\nabc"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if g := builtinScan(t, tc.in); !reflect.DeepEqual(g, tc.want) {
				t.Errorf("Scan(%q) = %v, want %v", tc.in, g, tc.want)
			}
		})
	}
}

// Finding 4. Decision: whole-entry adjacency is judged per entry. When the
// longest entry is rejected at a position (glued to an alphanumeric), shorter
// entries starting there, and entries starting later, are still tried.
// "db1" followed by "." is a valid boundary, so it matches inside "db1.corp.localx".
func TestDictionaryRetriesAfterRejectedLongestMatch(t *testing.T) {
	dir := t.TempDir()
	writeDict(t, dir, "d.txt", "db1.corp.local\ndb1\nfoo bar\nbar\n")
	ds, err := Build([]config.Rule{{Name: "d", Type: "dictionary", File: "d.txt", Synth: "hostname"}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	for in, want := range map[string]string{
		"db1.corp.localx": "db1",
		"xfoo bar":        "bar",
		"xdb1 db1":        "db1",
	} {
		sp := Scan(ds, in)
		if len(sp) != 1 || in[sp[0].Start:sp[0].End] != want {
			t.Errorf("%q: got %v want one span %q", in, sp, want)
		}
	}
	if sp := Scan(ds, "db1.corp.local"); len(sp) != 1 || sp[0].End != len("db1.corp.local") {
		t.Errorf("full entry: %v", sp)
	}
}

// Fix round 2: card windows.
func TestCardAllWindowsAndUnbrokenRuns(t *testing.T) {
	// A spurious Luhn-valid window (1006 0000 0000 4111) overlaps the front of
	// the real card; the union must leave no card digit visible.
	in := "1006 0000 0000 4111 1111 1111 1111"
	g := builtinScan(t, in)
	if len(g) != 1 || g[0].Class != "card" || g[0].Text != in {
		t.Errorf("overlapping windows: %v", g)
	}
	// Unbroken run with a leading extra digit: the strict card sub-window
	// widens to the whole digit run, masked as an opaque secret (Task 4 F1).
	if g := builtinScan(t, "94111111111111111"); !reflect.DeepEqual(g, []got{{"secret", "94111111111111111"}}) {
		t.Errorf("unbroken run: %v", g)
	}
	// 1700000000004 is a Luhn-valid 13-digit millisecond timestamp starting
	// with 1; the issuer-prefix filter must leave it alone.
	if g := builtinScan(t, "ts=1700000000004"); g != nil {
		t.Errorf("timestamp masked: %v", g)
	}
}

func TestCardFindLongRunBounded(t *testing.T) {
	// Behavioural bound (no wall clock): a very long digit run completes,
	// every card window is 13-19 digits, and the run is masked as one secret.
	run := strings.Repeat("4", 200000)
	for _, sp := range cardFind(run, 0, len(run)) {
		if n := sp[1] - sp[0]; n < 13 || n > 19 {
			t.Fatalf("window len %d", n)
		}
	}
	if g := cardRunFind(run+"0", 0, len(run)+1); !reflect.DeepEqual(g, [][2]int{{0, len(run) + 1}}) {
		t.Fatalf("run spans %v", g)
	}
}

// Fix round 3. Behavior chosen: in every digit group of 13+ digits all 13-19
// digit sub-windows are Luhn-tested. A strict sub-window needs a first digit
// of 3-6. The window spanning the whole group is exempt from that filter only
// when the group has 14-19 digits (so 2-series PANs like 2223000048400011 are
// caught); a 13-digit whole group still needs a 3-6 first digit, which keeps
// Luhn-valid millisecond timestamps (1700000000004) unmasked.
func TestCardSubWindowsInEveryLongGroup(t *testing.T) {
	// Task 4 F1: a strict sub-window is reported as the whole digit group, class secret.
	for _, in := range []string{"94111111111111111 5", "94111111111111111 5 items", "id=94111111111111111;"} {
		if g := builtinScan(t, in); !reflect.DeepEqual(g, []got{{"secret", "94111111111111111"}}) {
			t.Errorf("%q: %v", in, g)
		}
	}
	// Whole-group windows are unchanged: still class card.
	for _, in := range []string{"2223000048400011", "x4111111111111111"} {
		if g := builtinScan(t, in); len(g) != 1 || g[0].Class != "card" {
			t.Errorf("%q: %v", in, g)
		}
	}
	// A strict window needs an issuer first digit (3-6): 1700000000004 inside
	// a longer run with no other valid window stays unmasked.
	if g := builtinScan(t, "id 91700000000004"); g != nil {
		t.Errorf("non-issuer strict window masked: %v", g)
	}
	if g := builtinScan(t, "2223000048400011"); len(g) != 1 || g[0].Text != "2223000048400011" {
		t.Errorf("2-series PAN: %v", g)
	}
	if g := builtinScan(t, "ts=1700000000004"); g != nil {
		t.Errorf("timestamp masked: %v", g)
	}
}
