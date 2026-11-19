package detecteval

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"cui-proxy/internal/detect"
)

const baselinePath = "testdata/baseline.json"

// TestBaseline fails when the built-in detectors' results on the published
// corpus change. A detection change that moves a count is expected to update
// the baseline and the figures in docs/detection-quality.md in the same
// change: run with CUI_DETECTEVAL_UPDATE=1, then
// `go run ./scripts/detecteval` for the tables.
func TestBaseline(t *testing.T) {
	r, err := Run()
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if os.Getenv("CUI_DETECTEVAL_UPDATE") == "1" {
		if err := os.WriteFile(baselinePath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(got, want) {
		return
	}
	var old Report
	if err := json.Unmarshal(want, &old); err != nil {
		t.Fatalf("baseline is not a report: %v", err)
	}
	// Name the groups that moved, by counts only.
	was := map[string]Positive{}
	for _, p := range old.Variants {
		was[p.Family+"/"+p.Group] = p
	}
	for _, p := range r.Variants {
		if o, ok := was[p.Family+"/"+p.Group]; !ok || o != p {
			t.Errorf("positive %s/%s: full %d -> %d, partial %d -> %d, missed %d -> %d",
				p.Family, p.Group, o.Full, p.Full, o.Partial, p.Partial, o.Missed, p.Missed)
		}
	}
	wasNeg := map[string]Negative{}
	for _, n := range old.Negatives {
		wasNeg[n.Family+"/"+n.Group] = n
	}
	for _, n := range r.Negatives {
		if o, ok := wasNeg[n.Family+"/"+n.Group]; !ok || o != n {
			t.Errorf("negative %s/%s: flagged %d -> %d", n.Family, n.Group, o.Flagged, n.Flagged)
		}
	}
	t.Fatal("detection results differ from the baseline; see the comment on TestBaseline")
}

func TestCorpusIsDeterministicAndWellFormed(t *testing.T) {
	a, b := Corpus(Seed, 5), Corpus(Seed, 5)
	if len(a) == 0 || len(a) != len(b) {
		t.Fatalf("corpus sizes %d and %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("sample %d differs between two runs with the same seed", i)
		}
		s := a[i]
		if s.Start < 0 || s.End > len(s.Text) || s.Start >= s.End {
			t.Fatalf("%s/%s/%s: bad value offsets", s.Family, s.Variant, s.Context)
		}
	}
	if other := Corpus(Seed+1, 5); other[0].Text == a[0].Text {
		t.Error("a different seed produced the same first sample")
	}
}

// stub reports one fixed span for every text.
type stub struct{ start, end int }

func (stub) Name() string  { return "stub" }
func (stub) Class() string { return "ipv4" }
func (d stub) Find(string) []detect.Span {
	return []detect.Span{{Start: d.start, End: d.end, Rule: "stub", Class: "ipv4"}}
}

func TestEvaluateClassifiesCoverage(t *testing.T) {
	text := "aa VALUE bb" // the value is bytes 3..8
	pos := Sample{Family: "ipv4", Variant: "v", Context: "c", Positive: true, Claimed: true, Text: text, Start: 3, End: 8}
	neg := Sample{Family: "ipv4", Variant: "n", Context: "c", Text: text, Start: 3, End: 8}
	for _, tc := range []struct {
		name                  string
		span                  stub
		full, partial, missed int
		flagged               int
	}{
		{"exact", stub{3, 8}, 1, 0, 0, 1},
		{"wider", stub{0, 11}, 1, 0, 0, 1},
		{"left part", stub{0, 5}, 0, 1, 0, 1},
		{"outside", stub{9, 11}, 0, 0, 1, 0},
	} {
		r := Evaluate([]detect.Detector{tc.span}, []Sample{pos, neg})
		p, n := r.Variants[0], r.Negatives[0]
		if p.Full != tc.full || p.Partial != tc.partial || p.Missed != tc.missed || n.Flagged != tc.flagged {
			t.Errorf("%s: full=%d partial=%d missed=%d flagged=%d", tc.name, p.Full, p.Partial, p.Missed, n.Flagged)
		}
		if p.Full == 1 && p.AsFamily != 1 {
			t.Errorf("%s: a covering span of the value's family was not counted", tc.name)
		}
	}
}
