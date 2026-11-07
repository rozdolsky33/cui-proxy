package decisionlog

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var (
	goldenTS1 = time.Date(2026, 11, 7, 12, 34, 56, 789_000_000, time.UTC)
	goldenTS2 = time.Date(2026, 11, 7, 13, 34, 57, 0, time.FixedZone("x", 3600)) // recorded in UTC
	goldenID1 = ID{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef}
	goldenID2 = ID{0xfe, 0xdc, 0xba, 0x98, 0x76, 0x54, 0x32, 0x10}
)

// The vectors were computed outside Go (Python hashlib) from the canonical
// text below: hash = SHA-256(32 raw prev_hash bytes || canonical JSON).
const (
	goldenCanon1 = `{"ts":"2026-11-07T12:34:56.789Z","request_id":"0123456789abcdef","adapter":"codex","profile":"strict","decision":"sanitized","reason":"","rule_ids":["builtin.ipv4","hosts-a"],"counts":{"builtin.ipv4":2,"hosts-a":1},"approval_id":"","prev_hash":"0000000000000000000000000000000000000000000000000000000000000000"}`
	goldenHash1  = "98eb7c0acb0f5eaf757ed663098634e7c7bdd1233980380fc912f014877c5712"
	goldenHash2  = "f1312027bf12d2f2bd6373e4b96ddf20d9dfa01fca04b6b1a419373733581182"
)

func TestCanonicalGoldenVector(t *testing.T) {
	r, err := newRecord(Entry{
		RequestID: goldenID1, Adapter: "codex", Profile: "strict", Decision: Sanitized,
		Counts: map[string]int{"hosts-a": 1, "builtin.ipv4": 2}, // map order must not matter
	}, goldenTS1)
	if err != nil {
		t.Fatal(err)
	}
	r.seal(Genesis)
	if got := string(r.canonical()); got != goldenCanon1 {
		t.Fatalf("canonical:\n got %s\nwant %s", got, goldenCanon1)
	}
	if r.Hash != goldenHash1 {
		t.Fatalf("hash = %s, want %s", r.Hash, goldenHash1)
	}
	wantLine := strings.TrimSuffix(goldenCanon1, "}") + `,"hash":"` + goldenHash1 + "\"}\n"
	if got := string(r.line()); got != wantLine {
		t.Fatalf("line:\n got %s\nwant %s", got, wantLine)
	}

	// The second record chains on the first: prev_hash enters the hash.
	r2, err := newRecord(Entry{RequestID: goldenID2, Adapter: "codex", Profile: "strict", Decision: Blocked, Reason: "policy",
		Counts: map[string]int{"builtin.ipv4": 1}}, goldenTS2)
	if err != nil {
		t.Fatal(err)
	}
	r2.seal(r.Hash)
	if r2.TS != "2026-11-07T12:34:57Z" || r2.Hash != goldenHash2 {
		t.Fatalf("second record ts %s hash %s, want %s", r2.TS, r2.Hash, goldenHash2)
	}
	// Same record, different prev_hash: a different hash.
	r2.seal(Genesis)
	if r2.Hash == goldenHash2 {
		t.Fatal("prev_hash does not enter the hash")
	}
}

func TestCanonicalIsDeterministic(t *testing.T) {
	counts := map[string]int{}
	for _, k := range []string{"z", "a", "m", "b.c", "x-y", "PASS", "q_1"} {
		counts[k] = len(k)
	}
	first := ""
	for i := range 50 {
		r, err := newRecord(Entry{RequestID: goldenID1, Adapter: "codex", Profile: "sanitize", Decision: Sanitized, Counts: counts}, goldenTS1)
		if err != nil {
			t.Fatal(err)
		}
		r.seal(Genesis)
		if i == 0 {
			first = string(r.line())
		} else if string(r.line()) != first {
			t.Fatal("canonical encoding varies between runs")
		}
	}
}

func TestNewRecordKeepsTheClosedShape(t *testing.T) {
	ok := Entry{RequestID: goldenID1, Adapter: "codex", Profile: "sanitize", Decision: Forwarded}
	for _, c := range []struct {
		name string
		mut  func(*Entry)
		want string // "" = accepted
	}{
		{"forwarded", func(e *Entry) {}, ""},
		{"blocked with reason", func(e *Entry) { e.Decision, e.Reason = Blocked, "header" }, ""},
		{"audit reason", func(e *Entry) { e.Decision, e.Reason = Blocked, "audit" }, ""},
		{"passthrough field", func(e *Entry) { e.Passthrough = "sanitize" }, ""},
		{"passthrough on a sanitized request", func(e *Entry) { e.Decision, e.Passthrough = Sanitized, "sanitize" }, "passthrough"},
		{"passthrough with an open reason", func(e *Entry) { e.Passthrough = "x y" }, "passthrough"},
		{"unknown decision", func(e *Entry) { e.Decision = "allowed" }, "decision"},
		{"blocked without reason", func(e *Entry) { e.Decision = Blocked }, "reason"},
		{"reason on a forward", func(e *Entry) { e.Reason = "policy" }, "reason"},
		{"open reason", func(e *Entry) { e.Decision, e.Reason = Blocked, "policy: 10.0.0.1" }, "reason"},
		{"zero count", func(e *Entry) { e.Counts = map[string]int{"r": 0} }, "count"},
		{"negative count", func(e *Entry) { e.Counts = map[string]int{"r": -1} }, "count"},
		{"empty adapter", func(e *Entry) { e.Adapter = "" }, "adapter"},
		{"empty rule name", func(e *Entry) { e.Counts = map[string]int{"": 1} }, "counts"},
	} {
		e := ok
		c.mut(&e)
		_, err := newRecord(e, goldenTS1)
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: %v", c.name, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want) || !errors.Is(err, errInvalid)):
			t.Errorf("%s: error %v; want it to mention %q", c.name, err, c.want)
		}
	}
}

// Names from the config are cleaned, never trusted: a hostile rule name
// cannot inject JSON, a newline or a value-shaped string into a record.
func TestNewRecordCleansNames(t *testing.T) {
	r, err := newRecord(Entry{RequestID: goldenID1, Adapter: "co\"dex", Profile: "strict\n", Decision: Sanitized,
		Counts: map[string]int{"evil\"},\"x\":{\n" + strings.Repeat("a", 100): 1, "passthrough:bogus": 2}}, goldenTS1)
	if err != nil {
		t.Fatal(err)
	}
	r.seal(Genesis)
	line := string(r.line())
	if strings.Count(line, "\n") != 1 || strings.Contains(line, `\"`) || !strings.Contains(line, `"passthrough_bogus":2`) {
		t.Errorf("line = %s", line)
	}
	if _, err := parseLine([]byte(strings.TrimSuffix(line, "\n"))); err != nil {
		t.Errorf("a cleaned record does not read back: %v", err)
	}
	for _, id := range r.RuleIDs {
		if !isSafeName(id) {
			t.Errorf("rule id %q", id)
		}
	}
}

func TestParseLineRejectsNonCanonical(t *testing.T) {
	good := strings.TrimSuffix(goldenCanon1, "}") + `,"hash":"` + goldenHash1 + `"}`
	if _, err := parseLine([]byte(good)); err != nil {
		t.Fatalf("golden line: %v", err)
	}
	for name, line := range map[string]string{
		"space":         strings.Replace(good, `"ts":`, `"ts": `, 1),
		"reordered":     strings.Replace(good, `"adapter":"codex","profile":"strict"`, `"profile":"strict","adapter":"codex"`, 1),
		"extra field":   strings.Replace(good, `"approval_id":""`, `"approval_id":"","value":"x"`, 1),
		"duplicate key": strings.Replace(good, `"reason":""`, `"reason":"","reason":""`, 1),
		"approval set":  strings.Replace(good, `"approval_id":""`, `"approval_id":"A-1"`, 1),
		"upper hex":     strings.Replace(good, goldenHash1, strings.ToUpper(goldenHash1), 1),
		"local time":    strings.Replace(good, `56.789Z`, `56.789+01:00`, 1),
		"rule ids off":  strings.Replace(good, `["builtin.ipv4","hosts-a"]`, `["hosts-a","builtin.ipv4"]`, 1),
		"null counts":   strings.Replace(good, `{"builtin.ipv4":2,"hosts-a":1}`, `null`, 1),
		"trailing":      good + ` {}`,
		"not json":      "hello",
	} {
		if _, err := parseLine([]byte(line)); err == nil {
			t.Errorf("%s: accepted %s", name, line)
		}
	}
}

func TestIDs(t *testing.T) {
	a, b := NewID(), NewID()
	if a == b {
		t.Fatal("two random ids are equal")
	}
	p, err := ParseID(a.String())
	if err != nil || p != a || len(a.String()) != 16 {
		t.Fatalf("round trip %s: %v", a, err)
	}
	for _, s := range []string{"", "0123", "0123456789ABCDEF", "0123456789abcdeg", "0123456789abcdef0"} {
		if _, err := ParseID(s); err == nil {
			t.Errorf("ParseID(%q) accepted", s)
		}
	}
}

func TestSafeName(t *testing.T) {
	for in, want := range map[string]string{
		"builtin.ipv4":           "builtin.ipv4",
		"hosts-a_1":              "hosts-a_1",
		"a b\n\"c":               "a_b__c",
		strings.Repeat("x", 100): strings.Repeat("x", 64),
		"é":                      "__",
	} {
		if got := SafeName(in); got != want {
			t.Errorf("SafeName(%q) = %q, want %q", in, got, want)
		}
	}
}
