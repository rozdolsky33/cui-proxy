package detect

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

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
	// An email-like substring inside an ocid-like token: ocid wins.
	g = builtinScan(t, "ocid1.user.oc2..bob@corp.example.com")
	for _, x := range g {
		if x.Class != "ocid" && x.Class != "email" {
			t.Errorf("unexpected %v", x)
		}
	}
}

func TestResolveTiesAndOrdering(t *testing.T) {
	in := []Span{
		{Start: 5, End: 9, Rule: "a", Class: "ipv4"},
		{Start: 5, End: 9, Rule: "b", Class: "cidr"},
		{Start: 0, End: 3, Rule: "c", Class: "x"},
		{Start: 2, End: 6, Rule: "d", Class: "y"}, // overlaps c (earlier start wins at equal... c len3 < d len4: d wins over c) and the 5..9 pair
	}
	// d (len 4, start 2) vs 5..9 (len 4, start 5): earlier start wins -> d; 5..9 dropped; c overlaps d -> dropped.
	got := resolve(in)
	want := []Span{{Start: 2, End: 6, Rule: "d", Class: "y"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
	got = resolve([]Span{{Start: 5, End: 9, Rule: "a", Class: "ipv4"}, {Start: 5, End: 9, Rule: "b", Class: "cidr"}, {Start: 20, End: 22, Rule: "z", Class: "q"}})
	if len(got) != 2 || got[0].Class != "cidr" || got[1].Start != 20 {
		t.Errorf("tie: %v", got)
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
	writeDict(t, dir, "e.txt", "# only comments\n")
	abs := filepath.Join(dir, "e.txt")
	ds, err := Build([]config.Rule{{Name: "e", Type: "dictionary", File: abs, Synth: "hostname"}}, "/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if sp := Scan(ds, "anything"); len(sp) != 0 {
		t.Errorf("empty dict spans: %v", sp)
	}
	if _, err := Build([]config.Rule{{Name: "m", Type: "dictionary", File: "nope.txt", Synth: "hostname"}}, dir); err == nil {
		t.Error("missing dictionary file must fail Build")
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
