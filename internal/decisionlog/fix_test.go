package decisionlog

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Review item 1: a rule NAMED like a marker is just a (cleaned) rule name;
// only Entry.Passthrough makes a passthrough marker.
func TestRuleNamedLikeAMarkerStaysARule(t *testing.T) {
	r, err := newRecord(Entry{RequestID: goldenID1, Adapter: "codex", Profile: "sanitize", Decision: Sanitized,
		Counts: map[string]int{"passthrough:sanitize": 1, TruncatedKey: 2}}, goldenTS1)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.RuleIDs, ",") != "passthrough_sanitize,truncated_rules" || r.Counts["passthrough:sanitize"] != 0 || r.Counts[TruncatedKey] != 0 {
		t.Fatalf("rule_ids %v counts %v", r.RuleIDs, r.Counts)
	}
	p, err := newRecord(Entry{RequestID: goldenID1, Adapter: "codex", Profile: "sanitize", Decision: Forwarded, Passthrough: "sanitize"}, goldenTS1)
	if err != nil || p.Counts["passthrough:sanitize"] != 1 || len(p.RuleIDs) != 0 {
		t.Fatalf("passthrough record %+v %v", p, err)
	}
	// A marker on anything but a forward does not verify.
	p.Decision, p.Reason = Blocked, "policy"
	p.seal(Genesis)
	if _, err := parseLine(bytes.TrimSuffix(p.line(), []byte("\n"))); err == nil {
		t.Error("a passthrough marker on a blocked record was accepted")
	}
}

// Review item 4: a request matching many long rule names is capped, written
// and verifiable; strict can restart on it.
func TestManyRulesAreCappedAndStayVerifiable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.jsonl")
	l := open(t, Options{Path: path})
	counts := map[string]int{}
	for i := range 600 {
		counts[fmt.Sprintf("%060d-rule", i)] = 1 + i%3
	}
	if err := l.Record(Entry{RequestID: NewID(), Adapter: "codex", Profile: "strict", Decision: Blocked, Reason: "policy", Counts: counts}); err != nil {
		t.Fatal(err)
	}
	l.Close()
	b, _ := os.ReadFile(path)
	if len(b) > MaxLine {
		t.Fatalf("line of %d bytes", len(b))
	}
	r, err := parseLine(bytes.TrimSuffix(b, []byte("\n")))
	if err != nil || len(r.RuleIDs) != MaxRules || r.Counts[TruncatedKey] != 600-MaxRules {
		t.Fatalf("record %v rules %d truncated %d", err, len(r.RuleIDs), r.Counts[TruncatedKey])
	}
	for _, id := range r.RuleIDs { // the highest counts are kept
		if r.Counts[id] != 3 {
			t.Fatalf("kept %s with count %d", id, r.Counts[id])
		}
	}
	verifyOK(t, path, 1)
	if _, err := Open(Options{Path: path}); err != nil {
		t.Fatalf("reopen: %v", err)
	}
}

// Every entry the writer accepts produces a line the verifier accepts.
func FuzzRecordVerify(f *testing.F) {
	f.Add("builtin.ipv4", 3, 1, "", "sanitize")
	f.Add("passthrough:sanitize", 700, 64, "sanitize", "")
	f.Add(strings.Repeat("é\"\n", 40), 2, 200, "", "strict")
	f.Fuzz(func(t *testing.T, name string, n, count int, passthrough, profile string) {
		n, count = n%1000, count%1000
		e := Entry{RequestID: NewID(), Adapter: name, Profile: profile, Decision: Sanitized, Counts: map[string]int{}}
		for i := range n {
			e.Counts[fmt.Sprintf("%s%d", name, i)] = 1 + count
		}
		if passthrough != "" {
			e.Decision, e.Passthrough = Forwarded, passthrough
		}
		var buf bytes.Buffer
		prev := Genesis
		for range 2 {
			r, err := newRecord(e, goldenTS1)
			if err != nil {
				return // refused: nothing written
			}
			r.seal(prev)
			prev = r.Hash
			if len(r.line())-1 > MaxLine {
				t.Fatalf("a %d-byte line", len(r.line()))
			}
			buf.Write(r.line())
		}
		rep, err := VerifyReader("x", &buf)
		if err != nil || !rep.OK() || rep.Records != 2 {
			t.Fatalf("writer output does not verify: %+v %v", rep, err)
		}
	})
}

// Review item 2: a clock that steps back between rotations leaves an
// untampered log verifiable, and strict restarts on it.
func TestClockSteppingBackKeepsTheChainInOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.jsonl")
	clk := newClock()
	l := open(t, Options{Path: path, MaxBytes: 1200, now: clk.now})
	write(t, l, 8)
	clk.add(-2 * time.Hour)
	write(t, l, 8)
	clk.add(-30 * 24 * time.Hour)
	write(t, l, 8)
	l.Close()
	verifyOK(t, path, 24)
	l2, err := Open(Options{Path: path, MaxBytes: 1200, RetentionDays: 1, now: clk.now})
	if err != nil {
		t.Fatalf("reopen in block mode: %v", err)
	}
	write(t, l2, 2)
	l2.Close()
	verifyOK(t, path, 26)
}

// A clock wrongly in the future at startup: retention keeps the newest
// rotated file, notes what it deletes, and Verify explains the start.
func TestClockInTheFutureKeepsTheNewestAndNotesDeletions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.jsonl")
	clk := newClock()
	l := open(t, Options{Path: path, MaxBytes: 1200, RetentionDays: 90, now: clk.now})
	write(t, l, 20)
	l.Close()
	before, _ := Files(path)
	clk.add(365 * 24 * time.Hour)
	open(t, Options{Path: path, MaxBytes: 1200, RetentionDays: 90, now: clk.now}).Close()
	after, _ := Files(path)
	if len(after) != 2 || after[0] != before[len(before)-2] {
		t.Fatalf("before %v\nafter %v", before, after)
	}
	rep, err := Verify(path)
	if err != nil || !rep.OK() || rep.StartNote == nil || rep.StartNote.File != filepath.Base(before[len(before)-3]) {
		t.Fatalf("report %+v %v", rep, err)
	}
}

// A clock that reads earlier than the newest record: no retention at all.
func TestRetentionSkippedWhileTheClockIsBehind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.jsonl")
	clk := newClock()
	l := open(t, Options{Path: path, MaxBytes: 1200, now: clk.now})
	write(t, l, 8)
	clk.add(200 * 24 * time.Hour)
	write(t, l, 8)
	l.Close()
	before, _ := Files(path)
	clk.add(-150 * 24 * time.Hour) // back before the newest records; the oldest still look 50+ days old
	logs := &logLines{}
	open(t, Options{Path: path, MaxBytes: 1200, RetentionDays: 10, now: clk.now, Logf: logs.logf}).Close()
	if after, _ := Files(path); len(after) != len(before) {
		t.Errorf("retention deleted files with the clock behind: %d -> %d", len(before), len(after))
	}
	if !strings.Contains(logs.String(), "retention skipped") {
		t.Errorf("logs %q", logs.String())
	}
}

// Files from the first release (<name>.<timestamp>) are still read, first.
func TestUnnumberedRotatedNamesAreRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.jsonl")
	clk := newClock()
	l := open(t, Options{Path: path, MaxBytes: 1200, now: clk.now})
	write(t, l, 12)
	l.Close()
	files, _ := Files(path)
	old := path + "." + clk.now().Add(-time.Hour).UTC().Format(rotatedLayout)
	if err := os.Rename(files[0], old); err != nil {
		t.Fatal(err)
	}
	if f, _ := Files(path); f[0] != old {
		t.Fatalf("order %v", f)
	}
	verifyOK(t, path, 12)
}

// Review item 5: removing the first lines is a break, not "retention".
func TestHeadCutIsDetected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.jsonl")
	l := open(t, Options{Path: path})
	write(t, l, 5)
	l.Close()
	ls := lines(t, path)
	if err := os.WriteFile(path, []byte(strings.Join(ls[2:], "")), 0o600); err != nil {
		t.Fatal(err)
	}
	rep, err := Verify(path)
	if err != nil || rep.OK() || rep.Breaks[0].Kind != BreakHeadCut || rep.Breaks[0].Line != 1 {
		t.Fatalf("report %+v %v", rep, err)
	}
	if _, err := Open(Options{Path: path}); !errors.Is(err, ErrDamaged) {
		t.Fatalf("Open (block) = %v", err)
	}
	logs := &logLines{}
	l2 := open(t, Options{Path: path, NewSegmentOnDamage: true, Logf: logs.logf})
	write(t, l2, 1)
	l2.Close()
	if !strings.Contains(logs.String(), "does not start the chain") {
		t.Errorf("logs %q", logs.String())
	}
	// The same with rotated files: a whole oldest file removed without a note.
	path2 := filepath.Join(t.TempDir(), "d.jsonl")
	l3 := open(t, Options{Path: path2, MaxBytes: 1200})
	write(t, l3, 12)
	l3.Close()
	files, _ := Files(path2)
	os.Remove(files[0])
	if rep, _ := Verify(path2); rep.OK() || rep.Breaks[0].Kind != BreakHeadCut {
		t.Fatalf("a deleted oldest file without a note: %+v", rep)
	}
}

// Review item 8: no second writer can take over mid-rotation.
func TestNoSecondWriterDuringRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.jsonl")
	clk := newClock()
	l := open(t, Options{Path: path, MaxBytes: 1200, now: clk.now})
	write(t, l, 3)
	l.mu.Lock()
	rs, _ := rotatedFiles(path)
	name, _ := l.rotateName(rs)
	if err := os.Rename(path, name); err != nil { // the window between rename and create
		t.Fatal(err)
	}
	_, err := Open(Options{Path: path, now: clk.now})
	l.mu.Unlock()
	if !errors.Is(err, ErrInUse) {
		t.Fatalf("a second Open during rotation = %v, want ErrInUse", err)
	}
}

// Review item 9: the directory must be private and real.
func TestUnsafeDirectoryIsRefused(t *testing.T) {
	shared := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(shared, 0o777); err != nil {
		t.Fatal(err)
	}
	os.Chmod(shared, 0o777) // past the umask
	if _, err := Open(Options{Path: filepath.Join(shared, "d.jsonl")}); !errors.Is(err, ErrUnsafeDir) || !strings.Contains(err.Error(), "writable by group or others") {
		t.Errorf("group/other-writable dir: %v", err)
	}
	real := filepath.Join(t.TempDir(), "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(Options{Path: filepath.Join(link, "d.jsonl")}); !errors.Is(err, ErrUnsafeDir) || !strings.Contains(err.Error(), "symlink") {
		t.Errorf("symlinked dir: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(real, "d.jsonl")); err == nil {
		t.Error("a file was created through the symlinked directory")
	}
}

// Review item 7: the directory is synced after the active file is created
// and after a rotation, before the new file's first record.
func TestDirectorySyncedAfterCreateAndRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.jsonl")
	var syncs atomic.Int64
	l := open(t, Options{Path: path, MaxBytes: 1200, syncDir: func(string) { syncs.Add(1) }})
	if syncs.Load() == 0 {
		t.Fatal("no directory sync after creating the active file")
	}
	n := syncs.Load()
	write(t, l, 3)
	if syncs.Load() != n {
		t.Fatalf("a directory sync without a rotation")
	}
	for i := 0; syncs.Load() == n && i < 20; i++ {
		write(t, l, 1)
	}
	if syncs.Load() == n {
		t.Fatal("no directory sync after rotation")
	}
}

// Review item 12: verify while serve writes does not call a record in
// flight damage; without a live writer the same is a truncated line.
func TestPartialLastLineWhileWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.jsonl")
	l := open(t, Options{Path: path})
	write(t, l, 3)
	f, _ := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	f.WriteString(`{"ts":"2026`)
	f.Close()
	rep, err := Verify(path)
	if err != nil || !rep.OK() || !rep.InFlight || rep.Records != 3 {
		t.Fatalf("live writer: %+v %v", rep, err)
	}
	l.Close()
	if rep, _ := Verify(path); rep.OK() || rep.Breaks[0].Kind != BreakTruncated {
		t.Fatalf("no writer: %+v", rep)
	}
}

// Review item 10: Find says when a record does not verify.
func TestFindReportsUnverifiedRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.jsonl")
	l := open(t, Options{Path: path})
	var ids []ID
	for i := range 3 {
		e := entry(1)
		ids = append(ids, e.RequestID)
		_ = i
		if err := l.Record(e); err != nil {
			t.Fatal(err)
		}
	}
	l.Close()
	ls := lines(t, path)
	ls[1] = strings.Replace(ls[1], `"hosts-a":1`, `"hosts-a":2`, 1)
	os.WriteFile(path, []byte(strings.Join(ls, "")), 0o600)
	for i, want := range []string{"", BreakHash, BreakPrev} {
		got, err := Find(path, ids[i])
		if err != nil || len(got) != 1 || got[0].Problem != want {
			t.Errorf("record %d: %+v %v, want problem %q", i, got, err, want)
		}
	}
}

// Retention stops at the first rotated file that does not qualify, so a
// younger-named file earlier in the chain (the clock was ahead when it was
// rotated) is never skipped over to delete a later one.
func TestRetentionNeverMakesAHole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.jsonl")
	clk := newClock()
	clk.add(100 * 24 * time.Hour) // ahead
	l := open(t, Options{Path: path, MaxBytes: 1200, now: clk.now})
	write(t, l, 8)
	clk.add(-100 * 24 * time.Hour) // corrected
	write(t, l, 8)
	l.Close()
	before, _ := Files(path)
	clk.add(120 * 24 * time.Hour)
	open(t, Options{Path: path, MaxBytes: 1200, RetentionDays: 90, now: clk.now}).Close()
	after, _ := Files(path)
	if len(after) != len(before) {
		t.Errorf("deleted %d files past a file that does not qualify", len(before)-len(after))
	}
	verifyOK(t, path, 16)
}
