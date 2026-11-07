package transform

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"cui-proxy/internal/config"
	"cui-proxy/internal/detect"
	"cui-proxy/internal/vault"
)

var testKey = []byte("0123456789abcdef0123456789abcdef")

func builtins(t testing.TB) []detect.Detector {
	t.Helper()
	ds, err := detect.Build([]config.Rule{{Name: "builtin", Type: "builtin"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

type recSink struct{ evs []Event }

func (r *recSink) Record(e Event) { r.evs = append(r.evs, e) }

func pseudo(t testing.TB, v *vault.Vault, class, real string) string {
	t.Helper()
	s, err := v.Pseudonym(class, real)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSanitizeReplacesAndRestores(t *testing.T) {
	v := vault.New(testKey)
	s := NewSanitizer(builtins(t), v, nil)
	in := "ssh admin@10.20.30.15 and ocid1.instance.oc2..abc"
	out, err := s.Text(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, real := range []string{"10.20.30.15", "ocid1.instance.oc2..abc"} {
		if strings.Contains(out, real) {
			t.Errorf("real value %q remains in %q", real, out)
		}
	}
	if !strings.HasPrefix(out, "ssh admin@") || !strings.Contains(out, " and ocid1.instance.") {
		t.Errorf("surrounding text changed: %q", out)
	}
	if got := NewRestorer(v).Text(out); got != in {
		t.Errorf("restore = %q, want %q", got, in)
	}
}

// [RF 1] Already-synthetic values from earlier turns are never re-pseudonymized.
func TestSanitizeLeavesExistingSyntheticsAlone(t *testing.T) {
	v := vault.New(testKey)
	s := NewSanitizer(builtins(t), v, nil)
	in := "ssh admin@10.20.30.15, mail bob@corp.example.com, net 10.0.0.0/24, card 4111 1111 1111 1111, ssn 123-45-6789"
	first, err := s.Text(in)
	if err != nil {
		t.Fatal(err)
	}
	n := v.Len()
	second, err := s.Text(first)
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Errorf("second pass changed text:\n first %q\nsecond %q", first, second)
	}
	if v.Len() != n {
		t.Errorf("vault Len %d -> %d", n, v.Len())
	}
	if got := NewRestorer(v).Text(second); got != in {
		t.Errorf("restore = %q, want %q", got, in)
	}
}

var hex8 = regexp.MustCompile(`^[0-9a-f]{8}$`)

func TestSinkNeverSeesRealValues(t *testing.T) {
	v := vault.New(testKey)
	sink := &recSink{}
	s := NewSanitizer(builtins(t), v, sink)
	reals := []string{"10.20.30.15", "ocid1.instance.oc2..abc", "bob@corp.example.com", "123-45-6789"}
	in := fmt.Sprintf("a %s b %s c %s d %s e %s", reals[0], reals[1], reals[2], reals[3], reals[0])
	if _, err := s.Text(in); err != nil {
		t.Fatal(err)
	}
	if len(sink.evs) == 0 {
		t.Fatal("no events recorded")
	}
	total := 0
	for _, e := range sink.evs {
		if !hex8.MatchString(e.Fingerprint) {
			t.Errorf("fingerprint %q is not 8 hex chars", e.Fingerprint)
		}
		if e.Rule == "" || e.Class == "" || e.Count < 1 {
			t.Errorf("incomplete event %+v", e)
		}
		total += e.Count
		dump := fmt.Sprintf("%+v %#v", e, e)
		for _, r := range reals {
			for i := 0; i+6 <= len(r); i++ {
				if strings.Contains(dump, r[i:i+6]) {
					t.Errorf("event %s contains substring %q of a real value", dump, r[i:i+6])
				}
			}
		}
	}
	// S1: fingerprints are the vault's keyed fingerprint, never a bare SHA-256 prefix.
	for _, e := range sink.evs {
		keyed := false
		for _, r := range reals {
			h := sha256.Sum256([]byte(r))
			if e.Fingerprint == hex.EncodeToString(h[:4]) {
				t.Errorf("event %+v uses unkeyed SHA-256 fingerprint", e)
			}
			keyed = keyed || e.Fingerprint == v.Fingerprint(e.Class, r)
		}
		if !keyed {
			t.Errorf("event %+v: fingerprint is not vault.Fingerprint(class, real)", e)
		}
	}
	if total != 5 {
		t.Errorf("total count %d, want 5", total)
	}
	// One aggregated event per (rule, class, fingerprint): the repeated IP is counted twice.
	for _, e := range sink.evs {
		if e.Class == "ipv4" && e.Count != 2 {
			t.Errorf("ipv4 event count %d, want 2", e.Count)
		}
	}
}

// F1/F2: numeric IDs of any length, alone or glued to letters, '_', '-' and
// dotted digits, never block, round-trip through Text and Stream, are
// sanitized idempotently, and never leak a masked Luhn window. Seeded,
// deterministic sweep.
func TestNumericIDsNeverBlock(t *testing.T) {
	ds := builtins(t)
	rng := rand.New(rand.NewSource(42))
	digits := func(n int) string {
		b := make([]byte, n)
		for i := range b {
			b[i] = byte('0' + rng.Intn(10))
		}
		return string(b)
	}
	var s *Sanitizer
	var r *Restorer
	// F2 templates: digit runs glued to letters, '_', '-', and dotted digits.
	templates := []string{
		`{"x": 0.%s}`, `{"x": 1.%s e}`, `{"x": -0.%s}`, `"trace_%s"`, `"a%s"`,
		`1.%se-5`, `%s.5`, `"%s-abcd"`, `order_%s`, `abc %s def`, `abc%sdef`,
	}
	for i := 0; i < 6000; i++ {
		if i%200 == 0 { // fresh vault: keep the per-call snapshot cost small
			v := vault.New(testKey)
			s, r = NewSanitizer(ds, v, nil), NewRestorer(v)
		}
		var in, n string
		switch i % 4 {
		case 0:
			n = digits(14 + rng.Intn(19))
			in = fmt.Sprintf(`{"id": %s}`, n)
		case 1:
			n = strconv.FormatInt(1_600_000_000_000_000_000+rng.Int63n(400_000_000_000_000_000), 10)
			in = "ts=" + n
		case 2:
			n = digits(14 + rng.Intn(19))
			in = fmt.Sprintf(`{"id": %s, "ts": %s}`, n, digits(19))
		default:
			n = digits(13 + rng.Intn(12))
			in = fmt.Sprintf(templates[rng.Intn(len(templates))], n)
		}
		out, err := s.Text(in)
		if err != nil {
			t.Fatalf("%q blocked: %v", in, err)
		}
		if got := r.Text(out); got != in {
			t.Fatalf("round trip %q -> %q -> %q", in, out, got)
		}
		if w := luhnHit(n); w != "" && strings.Contains(out, w) {
			t.Fatalf("Luhn-valid run %q survives in %q", w, out)
		}
		again, err := s.Text(out)
		if err != nil || again != out {
			t.Fatalf("not idempotent: %q -> %q (%v)", out, again, err)
		}
		st := r.NewStream()
		var sb strings.Builder
		for rest := out; rest != ""; {
			k := 1 + rng.Intn(len(rest))
			sb.WriteString(st.Write(rest[:k]))
			rest = rest[k:]
		}
		sb.WriteString(st.Flush())
		if sb.String() != in {
			t.Fatalf("stream %q -> %q, want %q", out, sb.String(), in)
		}
	}
}

// luhnHit returns a Luhn-valid 13-19 digit window of the digit run n that the
// card rules mask (the whole run if 14-19 digits, else a window starting with
// 3-6), or "".
func luhnHit(n string) string {
	if len(n) >= 14 && len(n) <= 19 && luhn(n) {
		return n
	}
	for st := 0; st+13 <= len(n); st++ {
		if n[st] < '3' || n[st] > '6' {
			continue
		}
		for l := 13; l <= 19 && st+l <= len(n); l++ {
			if luhn(n[st : st+l]) {
				return n[st : st+l]
			}
		}
	}
	return ""
}

func luhn(d string) bool {
	sum := 0
	for i := len(d) - 1; i >= 0; i-- {
		x := int(d[i] - '0')
		if (len(d)-1-i)%2 == 1 {
			if x *= 2; x > 9 {
				x -= 9
			}
		}
		sum += x
	}
	return sum%10 == 0
}

func TestSanitizeErrorReturnsNoText(t *testing.T) {
	ds, err := detect.Build([]config.Rule{{Name: "bad", Type: "regex", Pattern: `XCIDR\d+`, Synth: "cidr"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	sink := &recSink{}
	out, err := NewSanitizer(ds, vault.New(testKey), sink).Text("a XCIDR123 b")
	if err == nil {
		t.Fatal("expected error")
	}
	if out != "" {
		t.Errorf("partial text returned with error: %q", out)
	}
	if !errors.Is(err, ErrPseudonym) || !strings.Contains(err.Error(), "rule=bad class=cidr") {
		t.Errorf("err = %v, want ErrPseudonym naming rule and class", err)
	}
	if strings.Contains(err.Error(), "XCIDR123") {
		t.Errorf("error leaks real value: %v", err)
	}
	if len(sink.evs) != 0 {
		t.Errorf("events recorded on error: %+v", sink.evs)
	}
}

// A synthetic glued to an alphanumeric would not be restored, so the
// sanitizer must refuse (fail closed) rather than send text that cannot round-trip.
func TestSanitizeBlocksUnrestorableContext(t *testing.T) {
	s := NewSanitizer(builtins(t), vault.New(testKey), nil)
	// ssn keeps the boundary rule; the detector accepts it between letters.
	out, err := s.Text("id x123-45-6789y now")
	if !errors.Is(err, ErrUnrestorable) {
		t.Fatalf("err = %v, want ErrUnrestorable", err)
	}
	if msg := err.Error(); !strings.Contains(msg, "rule=builtin.ssn") || !strings.Contains(msg, "class=ssn") || strings.Contains(msg, "6789") {
		t.Errorf("error should name rule and class, never the value: %q", msg)
	}
	if out != "" {
		t.Errorf("partial text returned with error: %q", out)
	}
}

func TestSanitizeCommonContextsRoundTrip(t *testing.T) {
	v := vault.New(testKey)
	s := NewSanitizer(builtins(t), v, nil)
	r := NewRestorer(v)
	for _, in := range []string{
		`{"cmd":"ssh admin@10.20.30.15 -p 22"}`,
		"curl http://10.20.30.15:8080/x?ip=10.20.30.16&net=10.0.0.0/24",
		"'bob@corp.example.com', (123-45-6789); [fe80::1ff:fe23:4567:890a]:443",
		"range 10.20.30.15-10.20.30.16\nAuthorization: Bearer abcdefgh12345678\n",
		"ocid1.instance.oc2..abc\tocid1.tenancy.oc2..aaaa",
		"card=4111-1111-1111-1111.",
	} {
		out, err := s.Text(in)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if out == in {
			t.Errorf("%q: nothing sanitized", in)
		}
		if got := r.Text(out); got != in {
			t.Errorf("round trip:\n  in %q\n out %q\ngot %q", in, out, got)
		}
	}
}

// ipv4Pair returns two vault synthetics a, b where b = a + one digit.
func ipv4Pair(t *testing.T, v *vault.Vault) (a, b string) {
	t.Helper()
	for i := 0; i < 600; i++ {
		pseudo(t, v, "ipv4", fmt.Sprintf("10.9.%d.%d", i/250, i%250+1))
	}
	for _, s := range v.Synthetics() {
		for d := 0; d <= 9; d++ {
			if v.IsSynthetic(s + strconv.Itoa(d)) {
				return s, s + strconv.Itoa(d)
			}
		}
	}
	t.Fatal("no prefix pair")
	return "", ""
}

// The boundary rule (see restore.go): a synthetic is restored only when not
// glued to an ASCII letter/digit on either side, not continuing a dotted
// number ("1.S", "S.1"), not continuing a colon-hex run for ipv6-shaped
// synthetics, and, for letter-initial synthetics (REDACTED-, h-, user-, ocid1.),
// not continuing a word run via '_' or '-'/'.' followed by a letter/digit.
func TestRestorerBoundaries(t *testing.T) {
	v := vault.New(testKey)
	short, long := ipv4Pair(t, v)
	realShort, _ := v.Real(short)
	realLong, _ := v.Real(long)
	sec := pseudo(t, v, "secret", "tok-123")
	realSec, _ := v.Real(sec)
	v6 := pseudo(t, v, "ipv6", "fe80::1")
	em := pseudo(t, v, "email", "bob@corp.example.com")
	host := pseudo(t, v, "hostname", "db.corp.internal")
	oc := pseudo(t, v, "ocid", "ocid1.instance.oc2..abc")
	var notVault string
	for d := 0; d <= 9; d++ {
		if c := short + strconv.Itoa(d); !v.IsSynthetic(c) {
			notVault = c
			break
		}
	}
	r := NewRestorer(v)
	tests := []struct{ in, want string }{
		{short, realShort},
		{long, realLong},
		{" " + short + " ", " " + realShort + " "},
		{`"` + short + `"`, `"` + realShort + `"`},
		{"(" + short + ")", "(" + realShort + ")"},
		{short + ":8080", realShort + ":8080"},
		{"http://" + short + "/x", "http://" + realShort + "/x"},
		{"ip=" + short + "&", "ip=" + realShort + "&"},
		{"admin@" + short, "admin@" + realShort},
		{"at " + short + ".", "at " + realShort + "."},
		{short + "-" + long, realShort + "-" + realLong},
		{"x_" + short, "x_" + realShort},
		{"→" + short, "→" + realShort},
		{long + " " + short, realLong + " " + realShort},
		// not restored
		{notVault, notVault},
		{"a" + short, "a" + short},
		{short + "x", short + "x"},
		{short + ".1", short + ".1"},
		{"1." + short, "1." + short},
		// Self-delimiting classes (secret, hostname, email, ocid) restore
		// even when glued to letters, digits, '_', '-' or '.' (F2 ruling).
		{"key " + sec + ".", "key " + realSec + "."},
		{"'" + sec + "'", "'" + realSec + "'"},
		{"x" + sec + "y", "x" + realSec + "y"},
		{"9" + sec + "9", "9" + realSec + "9"},
		{sec + "-abc", realSec + "-abc"},
		{sec + "_x", realSec + "_x"},
		{"x-" + sec, "x-" + realSec},
		{"x_" + sec, "x_" + realSec},
		{sec + ".txt", realSec + ".txt"},
		{"0." + sec + ".5", "0." + realSec + ".5"},
		{em + ".com", "bob@corp.example.com.com"},
		{"x" + em + "y", "xbob@corp.example.comy"},
		{"<" + em + ">", "<bob@corp.example.com>"},
		{"a" + host + "b", "adb.corp.internalb"},
		{"opc-" + oc + "9", "opc-ocid1.instance.oc2..abc9"},
		{"x" + sec[:len(sec)-1], "x" + sec[:len(sec)-1]}, // not a vault member
		// ipv6
		{"[" + v6 + "]:443", "[fe80::1]:443"},
		{v6 + ":1", v6 + ":1"},
		{"1:" + v6, "1:" + v6},
		{v6 + "::", v6 + "::"},
	}
	for _, tc := range tests {
		if got := r.Text(tc.in); got != tc.want {
			t.Errorf("Text(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func runeSplits(s string) []int {
	var out []int
	for i := 0; i <= len(s); i++ {
		if i == len(s) || utf8.RuneStart(s[i]) {
			out = append(out, i)
		}
	}
	return out
}

// [RF 2]
func TestStreamRestoreSplitAtEveryBoundary(t *testing.T) {
	v := vault.New(testKey)
	short, long := ipv4Pair(t, v)
	a := pseudo(t, v, "ipv4", "10.20.30.15")
	b := pseudo(t, v, "ocid", "ocid1.instance.oc2..abc")
	c := pseudo(t, v, "email", "bob@corp.example.com")
	r := NewRestorer(v)
	texts := []string{
		"ssh admin@" + a + " then «" + b + "» and ü" + c + ", done",
		a + b + " " + c,
		short + " " + long + " " + short + "0x " + long + ".1",
		"h" + c[:5] + " " + a[:4],
		a,
	}
	for _, text := range texts {
		want := r.Text(text)
		for _, i := range runeSplits(text) {
			st := r.NewStream()
			got := st.Write(text[:i]) + st.Write(text[i:]) + st.Flush()
			if got != want {
				t.Errorf("split %d of %q:\n got %q\nwant %q", i, text, got, want)
			}
		}
		// one rune per write
		st := r.NewStream()
		var sb strings.Builder
		for _, ch := range text {
			sb.WriteString(st.Write(string(ch)))
		}
		sb.WriteString(st.Flush())
		if sb.String() != want {
			t.Errorf("rune-wise %q: got %q want %q", text, sb.String(), want)
		}
	}
	if got := r.Text(texts[0]); !strings.Contains(got, "10.20.30.15") || !strings.Contains(got, "ocid1.instance.oc2..abc") || !strings.Contains(got, "übob@corp.example.com") {
		t.Errorf("Text did not restore all three: %q", got)
	}
}

func TestStreamHoldsBackOnlyPossiblePrefixes(t *testing.T) {
	v := vault.New(testKey)
	a := pseudo(t, v, "ipv4", "10.20.30.15")
	r := NewRestorer(v)
	st := r.NewStream()
	if got := st.Write("hello world, nothing here "); got != "hello world, nothing here " {
		t.Errorf("plain text delayed: %q", got)
	}
	if got := st.Write("ip " + a[:3]); got != "ip " {
		t.Errorf("prefix not held back: %q", got)
	}
	if got := st.Write(a[3:]); got != "" {
		t.Errorf("complete synthetic at tail must wait for a boundary: %q", got)
	}
	if got := st.Write(" ok"); got != "10.20.30.15 ok" {
		t.Errorf("after boundary: %q", got)
	}
	if got := st.Write(" 1999"); got != " 1999" {
		t.Errorf("non-prefix digits delayed: %q", got)
	}
	if got := st.Flush(); got != "" {
		t.Errorf("flush: %q", got)
	}
	st = r.NewStream()
	if got := st.Write("end " + a); got != "end " {
		t.Errorf("got %q", got)
	}
	if got := st.Flush(); got != "10.20.30.15" {
		t.Errorf("flush: %q", got)
	}
}
