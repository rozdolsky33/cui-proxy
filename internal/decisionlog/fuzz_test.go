package decisionlog

import (
	"bytes"
	"strings"
	"testing"
)

// FuzzVerify: verifying arbitrary bytes never panics, and a report that says
// OK is consistent: every line is a canonical record and each continues the
// previous one.
func FuzzVerify(f *testing.F) {
	var valid bytes.Buffer
	prev := Genesis
	for i := range 3 {
		r, err := newRecord(entry(i), goldenTS1)
		if err != nil {
			f.Fatal(err)
		}
		r.seal(prev)
		prev = r.Hash
		valid.Write(r.line())
	}
	f.Add(valid.Bytes())
	f.Add([]byte(goldenCanon1 + "\n"))
	f.Add([]byte(""))
	f.Add([]byte("\n\n{}\n"))
	f.Add(bytes.Repeat([]byte("x"), maxLine+10))
	f.Add(valid.Bytes()[:valid.Len()-7])
	f.Fuzz(func(t *testing.T, data []byte) {
		rep, err := VerifyReader("fuzz", bytes.NewReader(data))
		if err != nil {
			t.Fatalf("a bytes.Reader cannot fail to read: %v", err)
		}
		if rep == nil {
			t.Fatal("nil report")
		}
		if !rep.OK() {
			if len(rep.Breaks) == 0 {
				t.Fatalf("not OK without a break: %+v", rep)
			}
			return
		}
		prev := rep.Start
		n := 0
		for _, ln := range strings.SplitAfter(string(data), "\n") {
			if ln == "" {
				continue
			}
			r, err := parseLine([]byte(strings.TrimSuffix(ln, "\n")))
			if err != nil || !strings.HasSuffix(ln, "\n") || r.PrevHash != prev || hashOf(r.PrevHash, r.canonical()) != r.Hash {
				t.Fatalf("OK report for a bad line %q: %v", ln, err)
			}
			prev = r.Hash
			n++
		}
		if n != rep.Records || (n > 0 && rep.Head != prev) {
			t.Fatalf("records %d head %s; counted %d head %s", rep.Records, rep.Head, n, prev)
		}
	})
}
