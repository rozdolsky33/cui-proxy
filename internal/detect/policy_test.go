package detect

import (
	"os"
	"path/filepath"
	"testing"

	"cui-proxy/internal/config"
)

func rulesFor(t *testing.T, builtinAction, dictAction string) ([]config.Rule, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hosts.txt"), []byte("host1.corp\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return []config.Rule{
		{Name: "builtin", Type: "builtin", Action: builtinAction},
		{Name: "hosts", Type: "dictionary", File: "hosts.txt", Synth: "hostname", Action: dictAction},
	}, dir
}

func spansOf(t *testing.T, ds []Detector, s string) []Span { return Scan(ds, s) }

func TestBuildForProfileTagsBlockingRules(t *testing.T) {
	const text = "ssh 10.20.30.15 and host1.corp"
	rules, dir := rulesFor(t, "", "block")

	ds, err := BuildForProfile(rules, dir, "sanitize")
	if err != nil {
		t.Fatal(err)
	}
	byClass := map[string]Span{}
	for _, sp := range spansOf(t, ds, text) {
		byClass[sp.Class] = sp
	}
	if sp := byClass["ipv4"]; sp.Block {
		t.Errorf("ipv4 from a sanitize rule is tagged Block: %+v", sp)
	}
	if sp := byClass["hostname"]; !sp.Block || sp.BlockRule != "hosts" || sp.BlockClass != "hostname" {
		t.Errorf("dictionary rule with action block: %+v", sp)
	}

	// strict: every rule blocks, builtin expansions included.
	ds, err = BuildForProfile(rules, dir, "strict")
	if err != nil {
		t.Fatal(err)
	}
	for _, sp := range spansOf(t, ds, text) {
		if !sp.Block {
			t.Errorf("strict: span not tagged Block: %+v", sp)
		}
	}
}

func TestBuildDefaultNeverBlocks(t *testing.T) {
	rules, dir := rulesFor(t, "", "")
	ds, err := Build(rules, dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, sp := range Scan(ds, "10.20.30.15 host1.corp admin@example.com") {
		if sp.Block || sp.BlockRule != "" {
			t.Errorf("default build tagged a span Block: %+v", sp)
		}
	}
}

// A block rule must still block when its match sits inside, or overlaps, a
// longer match from a rule that only sanitizes: the merged span keeps Block.
func TestBlockSurvivesOverlapWithLongerSanitizeMatch(t *testing.T) {
	rules, dir := rulesFor(t, "", "block")
	ds, err := BuildForProfile(rules, dir, "sanitize")
	if err != nil {
		t.Fatal(err)
	}
	// builtin email "admin@host1.corp" is longer than, and contains, the
	// blocked dictionary host "host1.corp".
	text := "mail admin@host1.corp now"
	spans := Scan(ds, text)
	if len(spans) != 1 {
		t.Fatalf("want one merged span, got %+v", spans)
	}
	sp := spans[0]
	if text[sp.Start:sp.End] != "admin@host1.corp" || sp.Class != "email" {
		t.Errorf("merged span %q class %s; want the email", text[sp.Start:sp.End], sp.Class)
	}
	if !sp.Block || sp.BlockRule != "hosts" || sp.BlockClass != "hostname" {
		t.Errorf("block flag lost in the merge: %+v", sp)
	}
}

func TestResolveBlockMergeCases(t *testing.T) {
	sanitize := func(a, b int) Span { return Span{Start: a, End: b, Rule: "s", Class: "secret"} }
	block := func(a, b int, rule string) Span {
		return Span{Start: a, End: b, Rule: rule, Class: "hostname", Block: true, BlockRule: rule, BlockClass: "hostname"}
	}
	for name, tc := range map[string]struct {
		in        []Span
		wantN     int
		wantBlock []bool // per output span
		wantRule  []string
	}{
		"block contained in longer sanitize": {[]Span{sanitize(0, 20), block(5, 10, "b")}, 1, []bool{true}, []string{"b"}},
		"sanitize contained in longer block": {[]Span{block(0, 20, "b"), sanitize(5, 10)}, 1, []bool{true}, []string{"b"}},
		"partial overlap":                    {[]Span{sanitize(0, 10), block(8, 15, "b")}, 1, []bool{true}, []string{"b"}},
		"touching spans stay separate":       {[]Span{sanitize(0, 5), block(5, 10, "b")}, 2, []bool{false, true}, []string{"", "b"}},
		"two blockers: the better one names": {[]Span{block(0, 4, "short"), block(2, 12, "long")}, 1, []bool{true}, []string{"long"}},
		"no blockers":                        {[]Span{sanitize(0, 5), sanitize(3, 9)}, 1, []bool{false}, []string{""}},
		// A blocking group must not leak its flag into the next group.
		"blocking group then plain group": {[]Span{block(0, 5, "b"), sanitize(6, 10)}, 2, []bool{true, false}, []string{"b", ""}},
		"plain, blocking, plain":          {[]Span{sanitize(0, 3), block(4, 8, "b"), sanitize(9, 12)}, 3, []bool{false, true, false}, []string{"", "b", ""}},
	} {
		got := resolve(tc.in)
		if len(got) != tc.wantN {
			t.Errorf("%s: %d spans %+v; want %d", name, len(got), got, tc.wantN)
			continue
		}
		for i := range got {
			if got[i].Block != tc.wantBlock[i] || got[i].BlockRule != tc.wantRule[i] {
				t.Errorf("%s: span %d = %+v; want Block=%v BlockRule=%q", name, i, got[i], tc.wantBlock[i], tc.wantRule[i])
			}
		}
	}
}

func TestIsBlocking(t *testing.T) {
	rules, dir := rulesFor(t, "block", "")
	ds, err := BuildForProfile(rules, dir, "sanitize")
	if err != nil {
		t.Fatal(err)
	}
	var blockers, others int
	for _, d := range ds {
		if IsBlocking(d) {
			blockers++
		} else {
			others++
		}
	}
	if blockers == 0 || others == 0 {
		t.Errorf("blockers=%d others=%d; want builtin detectors blocking and the dictionary not", blockers, others)
	}
	plain, _ := Build(rules[:0], dir)
	if len(plain) != 0 {
		t.Fatal("setup")
	}
}
